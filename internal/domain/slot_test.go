package domain_test

import (
	"math"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func TestSlotEpoch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		slot domain.Slot
		want domain.Epoch
	}{
		{0, 0},
		{31, 0},
		{32, 1},
		{100, 3},
	}

	for _, tc := range tests {
		if got := tc.slot.Epoch(); got != tc.want {
			t.Errorf("Slot(%d).Epoch() = %d, want %d", tc.slot, got, tc.want)
		}
	}
}

func TestLastAttestationInclusionSlotSaturates(t *testing.T) {
	t.Parallel()
	if got := domain.Slot(math.MaxUint64).LastAttestationInclusionSlot(); got != domain.Slot(math.MaxUint64) {
		t.Fatalf("LastAttestationInclusionSlot() = %d, want MaxUint64", got)
	}
}

func TestEpochFirstSlot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		epoch domain.Epoch
		want  domain.Slot
	}{
		{0, 0},
		{1, 32},
		{3, 96},
	}

	for _, tc := range tests {
		if got := tc.epoch.FirstSlot(); got != tc.want {
			t.Errorf("Epoch(%d).FirstSlot() = %d, want %d", tc.epoch, got, tc.want)
		}
	}
}

func TestLastAttestationInclusionSlot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		slot domain.Slot
		want domain.Slot
	}{
		{0, 63},
		{31, 63},
		{32, 95},
		{100, 159},
	}
	for _, tc := range tests {
		if got := tc.slot.LastAttestationInclusionSlot(); got != tc.want {
			t.Errorf("Slot(%d).LastAttestationInclusionSlot() = %d, want %d", tc.slot, got, tc.want)
		}
	}
}

// TestCollectionWindowEnd pins the exact value Timeline.Validate compares
// ObsCollectionCompleted's timestamp against — this is the single source of
// truth every caller (tools/faultinjector, internal/app/duty_tracking.go)
// must agree with, so a wrong value here is a wrong value everywhere.
func TestCollectionWindowEnd(t *testing.T) {
	t.Parallel()

	slotStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	secondsPerSlot := 12 * time.Second

	slot := domain.Slot(0)
	windowSlots := slot.LastAttestationInclusionSlot() - slot // 63, per TestLastAttestationInclusionSlot

	got := slot.CollectionWindowEnd(slotStart, secondsPerSlot)
	want := slotStart.Add(time.Duration(windowSlots+1) * secondsPerSlot)
	if !got.Equal(want) {
		t.Errorf("CollectionWindowEnd() = %s, want %s (windowSlots=%d)", got, want, windowSlots)
	}
}

func TestCollectionWindowEndSaturates(t *testing.T) {
	t.Parallel()

	slotStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// A slot near MaxUint64 makes LastAttestationInclusionSlot saturate to s
	// itself (TestLastAttestationInclusionSlotSaturates), so windowSlots is 0
	// here, not an overflowing value — this exercises that CollectionWindowEnd
	// stays well-defined (one slot-duration past slotStart) rather than
	// overflowing, without itself needing to saturate.
	got := domain.Slot(math.MaxUint64).CollectionWindowEnd(slotStart, 12*time.Second)
	if want := slotStart.Add(12 * time.Second); !got.Equal(want) {
		t.Errorf("CollectionWindowEnd() = %s, want %s (windowSlots collapses to 0 once the slot itself has saturated)", got, want)
	}

	// A non-positive slot duration is the one input that actually reaches
	// CollectionWindowEnd's own saturation branch (windowSlots is always in
	// [0, SlotsPerEpoch*2-1] on its own, by construction of
	// LastAttestationInclusionSlot).
	got = domain.Slot(0).CollectionWindowEnd(slotStart, 0)
	if !got.After(slotStart.Add(100 * 365 * 24 * time.Hour)) {
		t.Errorf("CollectionWindowEnd() with zero secondsPerSlot = %s, want saturation, not a zero-duration window", got)
	}
}

// TestEpochRoundTrip proves the two conversions agree at their boundary, which is
// the property a report relies on when it labels a slot with its epoch.
func TestEpochRoundTrip(t *testing.T) {
	t.Parallel()

	for _, epoch := range []domain.Epoch{0, 1, 5, 1000} {
		first := epoch.FirstSlot()
		if got := first.Epoch(); got != epoch {
			t.Errorf("Epoch(%d).FirstSlot().Epoch() = %d, want %d", epoch, got, epoch)
		}
	}
}

func TestSourceIDValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		source domain.SourceID
		want   bool
	}{
		{domain.SourceBeaconAPI, true},
		{domain.SourcePromScrape, true},
		{domain.SourceHostMetrics, true},
		{domain.SourceClock, true},
		{domain.SourceXatu, true},
		{domain.SourceDerived, true},
		{"unregistered_adapter", false},
		{"", false},
	}

	for _, tc := range tests {
		if got := tc.source.Valid(); got != tc.want {
			t.Errorf("SourceID(%q).Valid() = %v, want %v", tc.source, got, tc.want)
		}
	}
}

func TestStages(t *testing.T) {
	t.Parallel()

	want := []domain.Stage{domain.StagePropagation, domain.StageValidation, domain.StageSigning}
	got := domain.Stages()
	if len(got) != len(want) {
		t.Fatalf("Stages() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Stages()[%d] = %q, want %q", i, got[i], want[i])
		}
		if !got[i].Valid() {
			t.Errorf("Stage %q reported invalid", got[i])
		}
	}
	if domain.Stage("aggregation").Valid() {
		t.Error(`Stage("aggregation").Valid() = true, want false — not part of the timing model`)
	}
}
