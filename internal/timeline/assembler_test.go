package timeline

import (
	"testing"
	"time"

	"github.com/CHANGEME/whymiss/internal/domain"
)

func mustObs(t *testing.T, slot domain.Slot, kind domain.ObservationKind, at time.Time, attrs map[domain.AttrKey]string) domain.Observation {
	t.Helper()
	obs, err := domain.NewObservation(domain.Observation{
		Slot: slot, Kind: kind, At: at, Source: domain.SourceBeaconAPI, Attrs: attrs,
	})
	if err != nil {
		t.Fatalf("NewObservation: %v", err)
	}
	return obs
}

func TestAssembler_Build(t *testing.T) {
	schedule := domain.MainnetPreEPBS()
	slotStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	a := NewAssembler(schedule)
	a.SetDuty(domain.Duty{Kind: domain.DutyAttester, Slot: 100, ValidatorIndex: 24})
	a.AddObservation(mustObs(t, 100, domain.ObsSlotStart, slotStart, nil))
	a.AddObservation(mustObs(t, 100, domain.ObsBlockSeen, slotStart.Add(600*time.Millisecond),
		map[domain.AttrKey]string{domain.AttrProposerIndex: "3"}))

	tl, err := a.Build(100, slotStart)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if tl.Slot != 100 {
		t.Errorf("Slot = %d, want 100", tl.Slot)
	}
	if tl.Duty == nil || tl.Duty.ValidatorIndex != 24 {
		t.Errorf("Duty = %+v, want validator 24", tl.Duty)
	}
	if len(tl.Observations) != 2 {
		t.Fatalf("got %d observations, want 2", len(tl.Observations))
	}
	if tl.Observations[0].Kind != domain.ObsSlotStart || tl.Observations[1].Kind != domain.ObsBlockSeen {
		t.Errorf("observations not in chronological order: %+v", tl.Observations)
	}
}

func TestAssembler_Build_OnlyReturnsTheRequestedSlot(t *testing.T) {
	schedule := domain.MainnetPreEPBS()
	slotStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	a := NewAssembler(schedule)
	a.AddObservation(mustObs(t, 100, domain.ObsSlotStart, slotStart, nil))
	a.AddObservation(mustObs(t, 101, domain.ObsSlotStart, slotStart.Add(12*time.Second), nil))

	tl, err := a.Build(100, slotStart)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(tl.Observations) != 1 {
		t.Fatalf("got %d observations, want 1 (slot 101's should not appear on slot 100's timeline): %+v", len(tl.Observations), tl.Observations)
	}
}

// TestAssembler_Build_DeterministicAcrossArrivalOrder is the property
// BUILD_PROMPT.md §10.3 requires of replay: the same set of observations,
// added in a different order (as real adapters running on separate
// goroutines could produce), must assemble into the identical Timeline.
func TestAssembler_Build_DeterministicAcrossArrivalOrder(t *testing.T) {
	schedule := domain.MainnetPreEPBS()
	slotStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Two observations sharing the exact same timestamp, so the tie-break
	// logic (not just At-ordering) is what has to agree across both builds.
	same := slotStart.Add(4 * time.Second)
	obsA := mustObs(t, 100, domain.ObsAttestationPublished, same, map[domain.AttrKey]string{domain.AttrValidatorIndex: "24"})
	obsB := mustObs(t, 100, domain.ObsBlockSeen, same, map[domain.AttrKey]string{domain.AttrProposerIndex: "3"})

	a1 := NewAssembler(schedule)
	a1.AddObservation(obsA)
	a1.AddObservation(obsB)
	tl1, err := a1.Build(100, slotStart)
	if err != nil {
		t.Fatalf("Build (order 1): %v", err)
	}

	a2 := NewAssembler(schedule)
	a2.AddObservation(obsB)
	a2.AddObservation(obsA)
	tl2, err := a2.Build(100, slotStart)
	if err != nil {
		t.Fatalf("Build (order 2): %v", err)
	}

	if len(tl1.Observations) != len(tl2.Observations) {
		t.Fatalf("observation counts differ: %d vs %d", len(tl1.Observations), len(tl2.Observations))
	}
	for i := range tl1.Observations {
		if tl1.Observations[i].Kind != tl2.Observations[i].Kind {
			t.Errorf("observation %d: Kind = %q vs %q, want identical order regardless of arrival order",
				i, tl1.Observations[i].Kind, tl2.Observations[i].Kind)
		}
	}
}

func TestAssembler_Build_NoDutyIsNilNotZeroValue(t *testing.T) {
	schedule := domain.MainnetPreEPBS()
	slotStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	a := NewAssembler(schedule)
	tl, err := a.Build(100, slotStart)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if tl.Duty != nil {
		t.Errorf("Duty = %+v, want nil for a slot nothing was set for", tl.Duty)
	}
}
