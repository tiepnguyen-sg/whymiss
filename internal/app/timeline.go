package app

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/CHANGEME/whymiss/internal/domain"
	"github.com/CHANGEME/whymiss/internal/store"
	"github.com/CHANGEME/whymiss/internal/timeline"
)

// GetTimeline opens dbPath read-only-in-spirit (store.Open still runs
// migrations, which is a no-op on an already-current database) and
// assembles the domain.Timeline for slot from whatever whymiss watch has
// persisted about it.
//
// sampleWindow bounds how far back MetricSample values are pulled from
// (internal/timeline.Assembler.AddSample has no slot scoping of its own —
// see its doc comment) — a day is generous enough for any rolling baseline
// this build computes, without loading the store's entire sample history
// for one slot's report.
func GetTimeline(ctx context.Context, dbPath string, slot domain.Slot, schedule domain.SlotSchedule) (domain.Timeline, error) {
	const sampleWindow = 24 * time.Hour

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

	samples, err := st.SamplesSince(ctx, slotStart.Add(-sampleWindow))
	if err != nil {
		return domain.Timeline{}, fmt.Errorf("load samples for slot %d: %w", slot, err)
	}

	a := timeline.NewAssembler(schedule)
	for _, obs := range observations {
		a.AddObservation(obs)
		if obs.Kind == domain.ObsDutyAssigned {
			if vi, ok := obs.Attr(domain.AttrValidatorIndex); ok {
				parsed, err := strconv.ParseUint(vi, 10, 64)
				if err != nil {
					return domain.Timeline{}, fmt.Errorf("parse validator_index %q: %w", vi, err)
				}
				a.SetDuty(domain.Duty{Kind: domain.DutyAttester, Slot: slot, ValidatorIndex: domain.ValidatorIndex(parsed)})
			}
		}
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
