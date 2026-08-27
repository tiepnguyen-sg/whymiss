package app

import (
	"strings"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/source"
	"github.com/tiepnguyen-sg/whymiss/internal/source/beaconapi"
)

func TestBlockSeenFromTiming(t *testing.T) {
	t.Parallel()
	genesis := beaconapi.GenesisInfo{GenesisTime: time.Unix(1_700_000_000, 0).UTC(), SecondsPerSlot: 12 * time.Second}
	slot := domain.Slot(10)
	slotStart := genesis.SlotStart(uint64(slot))
	root := "0x" + strings.Repeat("a", 64)
	head, err := domain.NewObservation(domain.Observation{
		Slot: slot, Kind: domain.ObsHeadUpdated, At: slotStart.Add(900 * time.Millisecond),
		Source: domain.SourceBeaconAPI, Attrs: map[domain.AttrKey]string{domain.AttrBlockRoot: root},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := blockSeenFromTiming(head, source.BlockTiming{Slot: slot, Propagation: 350 * time.Millisecond}, genesis)
	if err != nil {
		t.Fatal(err)
	}
	if got.At != slotStart.Add(350*time.Millisecond) || got.Source != domain.SourcePromScrape {
		t.Fatalf("observation = %+v", got)
	}
	if gotRoot, ok := got.Attr(domain.AttrBlockRoot); !ok || gotRoot != root {
		t.Fatalf("block root = %q, %v", gotRoot, ok)
	}
}

func TestBlockSeenFromTimingRejectsStaleGauge(t *testing.T) {
	t.Parallel()
	genesis := beaconapi.GenesisInfo{GenesisTime: time.Unix(1_700_000_000, 0).UTC(), SecondsPerSlot: 12 * time.Second}
	slotStart := genesis.SlotStart(10)
	head, err := domain.NewObservation(domain.Observation{
		Slot: 10, Kind: domain.ObsHeadUpdated, At: slotStart.Add(time.Second), Source: domain.SourceBeaconAPI,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := blockSeenFromTiming(head, source.BlockTiming{Slot: 10, Propagation: 2 * time.Second}, genesis); err == nil {
		t.Fatal("want stale-gauge error")
	}
}

func TestBlockSeenFromTimingRejectsDifferentMetricSlot(t *testing.T) {
	t.Parallel()
	genesis := beaconapi.GenesisInfo{GenesisTime: time.Unix(1_700_000_000, 0).UTC(), SecondsPerSlot: 12 * time.Second}
	head, err := domain.NewObservation(domain.Observation{
		Slot: 10, Kind: domain.ObsHeadUpdated, At: genesis.SlotStart(10).Add(time.Second), Source: domain.SourceBeaconAPI,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := blockSeenFromTiming(head, source.BlockTiming{Slot: 9, Propagation: 350 * time.Millisecond}, genesis); err == nil {
		t.Fatal("want mismatched-slot error")
	}
}

func TestEngineWindowTrackerRequiresConsecutiveCanonicalHeads(t *testing.T) {
	t.Parallel()
	snapshot := func(at time.Time, count uint64, sum float64) source.EngineCounters {
		value := source.EngineCounters{SampledAt: at}
		value.NewPayload.Count, value.NewPayload.SumMS = count, sum
		value.ForkchoiceUpdated.Count, value.ForkchoiceUpdated.SumMS = count, sum
		return value
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var tracker engineWindowTracker
	if calls, complete, err := tracker.advance(100, snapshot(base, 10, 100)); err != nil || complete || calls != nil {
		t.Fatalf("warm-up = %+v, %t, %v", calls, complete, err)
	}
	calls, complete, err := tracker.advance(101, snapshot(base.Add(time.Second), 11, 110))
	if err != nil || !complete || len(calls) != 2 {
		t.Fatalf("consecutive window = %+v, %t, %v", calls, complete, err)
	}
	if calls, complete, err := tracker.advance(103, snapshot(base.Add(2*time.Second), 13, 130)); err != nil || complete || calls != nil {
		t.Fatalf("gapped window = %+v, %t, %v", calls, complete, err)
	}
	calls, complete, err = tracker.advance(104, snapshot(base.Add(3*time.Second), 14, 140))
	if err != nil || !complete || len(calls) != 2 {
		t.Fatalf("window after reset = %+v, %t, %v", calls, complete, err)
	}
}

func TestEngineWindowTrackerRejectsCounterReset(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	before := source.EngineCounters{SampledAt: base}
	before.NewPayload.Count, before.NewPayload.SumMS = 10, 100
	before.ForkchoiceUpdated.Count, before.ForkchoiceUpdated.SumMS = 10, 100
	after := source.EngineCounters{SampledAt: base.Add(time.Second)}
	after.NewPayload.Count, after.NewPayload.SumMS = 1, 1
	after.ForkchoiceUpdated.Count, after.ForkchoiceUpdated.SumMS = 1, 1
	var tracker engineWindowTracker
	_, _, _ = tracker.advance(100, before)
	if _, complete, err := tracker.advance(101, after); err == nil || complete {
		t.Fatalf("counter reset = complete %t, error %v; want rejection", complete, err)
	}
}
