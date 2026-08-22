package domain_test

import (
	"testing"

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
