package app

import (
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/source"
	"github.com/tiepnguyen-sg/whymiss/internal/source/beaconapi"
)

// baselineFixture returns a genesis whose slot 100 starts at slotStart, plus a
// head observation for that slot, so a test only has to state the timing it
// wants judged.
func baselineFixture(t *testing.T, slotStart time.Time, headAt time.Time) (beaconapi.GenesisInfo, domain.Observation) {
	t.Helper()
	const slotDuration = 12 * time.Second
	genesis := beaconapi.GenesisInfo{
		GenesisTime:    slotStart.Add(-100 * slotDuration),
		SecondsPerSlot: slotDuration,
	}
	head, err := domain.NewObservation(domain.Observation{
		Slot: 100, Kind: domain.ObsHeadUpdated, At: headAt, Source: domain.SourceBeaconAPI,
	})
	if err != nil {
		t.Fatal(err)
	}
	return genesis, head
}

func TestNetworkBaselineFromTimingRequiresMatchingSlot(t *testing.T) {
	t.Parallel()
	slotStart := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	genesis, head := baselineFixture(t, slotStart, slotStart)

	timing := source.BlockTiming{Slot: 100, Propagation: 450 * time.Millisecond, SampledAt: slotStart.Add(time.Second)}
	got, err := networkBaselineFromTiming(head, timing, genesis)
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
	if _, err := networkBaselineFromTiming(head, timing, genesis); err == nil {
		t.Fatal("mismatched baseline slot was accepted")
	}
}

// TestNetworkBaselineFromTimingRejectsStaleGauge is the regression guard for
// ADR-0022. Both clients publish block arrival as a latest-value gauge with no
// slot label, and the sample's slot is proven from beacon_head_slot — a
// different series — so a node advancing its head without recording an arrival
// keeps returning an older slot's value. A live run recorded 21 consecutive
// baseline observations carrying the identical stale propagation before this
// bound existed on the baseline path.
func TestNetworkBaselineFromTimingRejectsStaleGauge(t *testing.T) {
	t.Parallel()
	slotStart := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	genesis, head := baselineFixture(t, slotStart, slotStart.Add(90*time.Millisecond))

	// The exact shape observed live: a 2233 ms gauge read 702 ms into the slot.
	// The block cannot have arrived after the instant the gauge reporting it was
	// read, so the value belongs to some earlier slot.
	stale := source.BlockTiming{
		Slot:        100,
		Propagation: 2233 * time.Millisecond,
		SampledAt:   slotStart.Add(702 * time.Millisecond),
	}
	if _, err := networkBaselineFromTiming(head, stale, genesis); err == nil {
		t.Fatal("a gauge placing arrival after its own sample time was accepted as this slot's measurement")
	}

	// A genuinely late block stays acceptable: the node imported it, then we
	// read the gauge afterwards, so arrival still precedes the sample.
	late := source.BlockTiming{
		Slot:        100,
		Propagation: 3 * time.Second,
		SampledAt:   slotStart.Add(3100 * time.Millisecond),
	}
	if _, err := networkBaselineFromTiming(head, late, genesis); err != nil {
		t.Errorf("a genuinely late but freshly recorded arrival was rejected: %v", err)
	}

	// The boundary is inclusive: arrival exactly at the sample instant is the
	// fastest possible honest reading, not a stale one.
	exact := source.BlockTiming{
		Slot:        100,
		Propagation: time.Second,
		SampledAt:   slotStart.Add(time.Second),
	}
	if _, err := networkBaselineFromTiming(head, exact, genesis); err != nil {
		t.Errorf("arrival at exactly the sample instant was rejected: %v", err)
	}
}

// TestBaselineFromArrival pins the measurement the slot-driven path produces,
// and the reason that path exists at all.
//
// The first version of this collector was triggered by the watched node's
// head_updated, and that is a false-attribution trap: BlockSeen returns as soon
// as the baseline node has the block, so starting the poll only once *our* node
// reported a head makes the reading an upper bound shaped by our own latency. A
// node seeing the block at +6s would have produced a ~6.1s baseline for a peer
// that actually had it at +0.1s, and R-110 would then see local and network
// agreeing above the deadline and report network.late_block — exonerating a
// local fault. Driving from the slot boundary is what makes the number an
// arrival time.
func TestBaselineFromArrival(t *testing.T) {
	t.Parallel()
	slotStart := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)

	obs, err := baselineFromArrival(100, slotStart, slotStart.Add(115*time.Millisecond))
	if err != nil {
		t.Fatalf("baselineFromArrival: %v", err)
	}
	if obs.Kind != domain.ObsNetworkBaselineSampled {
		t.Errorf("Kind = %q, want %q", obs.Kind, domain.ObsNetworkBaselineSampled)
	}
	if obs.Source != domain.SourceBeaconAPI {
		t.Errorf("Source = %q, want %q", obs.Source, domain.SourceBeaconAPI)
	}
	baseline, err := domain.NetworkBaselineFromObservation(obs)
	if err != nil {
		t.Fatalf("NetworkBaselineFromObservation: %v", err)
	}
	if baseline.BlockArrivalP50 != 115*time.Millisecond {
		t.Errorf("p50 = %s, want 115ms", baseline.BlockArrivalP50)
	}
	if baseline.SampleCount != 1 {
		t.Errorf("SampleCount = %d, want 1 — one node is one sample, however it was measured", baseline.SampleCount)
	}

	// An arrival before its own slot start is not a measurement.
	if _, err := baselineFromArrival(100, slotStart, slotStart.Add(-time.Millisecond)); err == nil {
		t.Error("an arrival preceding its slot start was accepted")
	}
}
