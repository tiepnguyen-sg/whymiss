package app

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/store"
	"github.com/tiepnguyen-sg/whymiss/internal/timeline"
)

// GetTimeline opens dbPath read-only-in-spirit (store.Open still runs
// migrations, which is a no-op on an already-current database) and
// assembles the domain.Timeline for slot from whatever whymiss watch has
// persisted about it.
//
// Samples are restricted to the slot's evidence window. This prevents a report
// for an old slot from using measurements collected afterward.
func GetTimeline(ctx context.Context, dbPath string, slot domain.Slot, schedule domain.SlotSchedule) (domain.Timeline, error) {
	return GetTimelineForValidatorSelection(ctx, dbPath, slot, nil, schedule)
}

// GetTimelineForValidator returns one validator's duty-scoped facts plus the
// common slot facts. It is required when several tracked validators share a slot.
func GetTimelineForValidator(ctx context.Context, dbPath string, slot domain.Slot, validator domain.ValidatorIndex, schedule domain.SlotSchedule) (domain.Timeline, error) {
	return GetTimelineForValidatorSelection(ctx, dbPath, slot, &validator, schedule)
}

// GetTimelineForValidatorSelection auto-selects the only duty when validator is
// nil, and rejects an ambiguous multi-validator slot instead of mixing evidence.
func GetTimelineForValidatorSelection(ctx context.Context, dbPath string, slot domain.Slot, validator *domain.ValidatorIndex, schedule domain.SlotSchedule) (domain.Timeline, error) {
	const sampleLookback = time.Minute

	st, err := store.Open(ctx, dbPath)
	if err != nil {
		return domain.Timeline{}, fmt.Errorf("open store: %w", err)
	}
	defer st.Close() //nolint:errcheck // read path; nothing to act on if Close fails

	observations, err := st.ObservationsForSlot(ctx, slot)
	if err != nil {
		return domain.Timeline{}, fmt.Errorf("load observations for slot %d: %w", slot, err)
	}
	if len(observations) == 0 {
		return domain.Timeline{}, fmt.Errorf("no observations recorded for slot %d", slot)
	}
	observations, duty, err := selectDutyObservations(slot, observations, validator)
	if err != nil {
		return domain.Timeline{}, err
	}
	var windowReorgs []domain.Observation
	if duty != nil && duty.Kind == domain.DutyAttester {
		windowReorgs, err = st.ReorgsBetweenSlots(ctx, slot, slot.LastAttestationInclusionSlot())
		if err != nil {
			return domain.Timeline{}, fmt.Errorf("load inclusion-window reorgs for slot %d: %w", slot, err)
		}
	}

	var slotStart time.Time
	found := false
	for _, obs := range observations {
		if obs.Kind == domain.ObsSlotStart {
			slotStart, found = obs.At, true
			break
		}
	}
	if !found {
		return domain.Timeline{}, fmt.Errorf("no slot_start observation recorded for slot %d", slot)
	}

	samples, err := st.SamplesBetween(ctx, slotStart.Add(-sampleLookback), schedule.SlotEndAt(slotStart))
	if err != nil {
		return domain.Timeline{}, fmt.Errorf("load samples for slot %d: %w", slot, err)
	}

	a := timeline.NewAssembler(schedule)
	for _, obs := range observations {
		a.AddObservation(obs)
		if obs.Kind == domain.ObsNetworkBaselineSampled {
			baseline, err := domain.NetworkBaselineFromObservation(obs)
			if err != nil {
				return domain.Timeline{}, fmt.Errorf("decode network baseline for slot %d: %w", slot, err)
			}
			a.SetNetwork(baseline)
		}
	}
	for _, reorg := range windowReorgs {
		a.AddObservation(reorg)
	}
	if duty != nil {
		a.SetDuty(*duty)
	}
	for _, sample := range samples {
		a.AddSample(sample)
	}

	tl, err := a.Build(slot, slotStart)
	if err != nil {
		return domain.Timeline{}, fmt.Errorf("build timeline for slot %d: %w", slot, err)
	}
	return tl, nil
}

func selectDutyObservations(slot domain.Slot, observations []domain.Observation, requested *domain.ValidatorIndex) ([]domain.Observation, *domain.Duty, error) {
	assigned := make(map[domain.ValidatorIndex]struct{})
	for _, obs := range observations {
		if obs.Kind != domain.ObsDutyAssigned {
			continue
		}
		validator, err := observationValidator(obs)
		if err != nil {
			return nil, nil, fmt.Errorf("slot %d duty assignment: %w", slot, err)
		}
		assigned[validator] = struct{}{}
	}

	var selected *domain.ValidatorIndex
	switch {
	case requested != nil:
		if _, ok := assigned[*requested]; !ok {
			return nil, nil, fmt.Errorf("slot %d has no recorded duty for validator %d", slot, *requested)
		}
		value := *requested
		selected = &value
	case len(assigned) == 1:
		for validator := range assigned {
			value := validator
			selected = &value
		}
	case len(assigned) > 1:
		validators := make([]domain.ValidatorIndex, 0, len(assigned))
		for validator := range assigned {
			validators = append(validators, validator)
		}
		slices.Sort(validators)
		return nil, nil, fmt.Errorf("slot %d has duties for validators %v; select exactly one validator_index", slot, validators)
	}

	filtered := make([]domain.Observation, 0, len(observations))
	for _, obs := range observations {
		if !dutyScopedObservation(obs.Kind) {
			filtered = append(filtered, obs)
			continue
		}
		validator, err := observationValidator(obs)
		if err != nil {
			// Pre-release completion markers had no validator attribute. They are
			// unambiguous only when the slot has exactly one assigned duty.
			if obs.Kind == domain.ObsCollectionCompleted && selected != nil && len(assigned) == 1 {
				filtered = append(filtered, obs)
				continue
			}
			return nil, nil, fmt.Errorf("slot %d %s: %w", slot, obs.Kind, err)
		}
		if selected != nil && validator == *selected {
			filtered = append(filtered, obs)
		}
	}
	if selected == nil {
		return filtered, nil, nil
	}
	return filtered, &domain.Duty{Kind: domain.DutyAttester, Slot: slot, ValidatorIndex: *selected}, nil
}

func dutyScopedObservation(kind domain.ObservationKind) bool {
	switch kind {
	case domain.ObsDutyAssigned, domain.ObsAttestationPublished, domain.ObsAttestationIncluded,
		domain.ObsBlockProposed, domain.ObsCollectionCompleted:
		return true
	default:
		return false
	}
}

func observationValidator(obs domain.Observation) (domain.ValidatorIndex, error) {
	value, ok := obs.Attr(domain.AttrValidatorIndex)
	if !ok {
		return 0, fmt.Errorf("missing validator_index")
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse validator_index %q: %w", value, err)
	}
	return domain.ValidatorIndex(parsed), nil
}
