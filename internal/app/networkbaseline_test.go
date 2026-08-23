package app

import (
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/source"
)

func TestNetworkBaselineFromTimingRequiresMatchingSlot(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	head, err := domain.NewObservation(domain.Observation{
		Slot: 100, Kind: domain.ObsHeadUpdated, At: at, Source: domain.SourceBeaconAPI,
	})
	if err != nil {
		t.Fatal(err)
	}

	timing := source.BlockTiming{Slot: 100, Propagation: 450 * time.Millisecond, SampledAt: at.Add(time.Second)}
	got, err := networkBaselineFromTiming(head, timing)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := domain.NetworkBaselineFromObservation(got)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Slot != 100 || baseline.BlockArrivalP50 != 450*time.Millisecond || baseline.BlockArrivalP90 != 450*time.Millisecond || baseline.SampleCount != 1 {
		t.Fatalf("baseline = %+v", baseline)
	}

	timing.Slot = 99
	if _, err := networkBaselineFromTiming(head, timing); err == nil {
		t.Fatal("mismatched baseline slot was accepted")
	}
}
