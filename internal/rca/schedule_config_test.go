package rca_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/rca"
	"github.com/tiepnguyen-sg/whymiss/internal/timeline"
)

// TestAnalyze_TimingFollowsTheScheduleNotTheCode is the evidence behind Phase
// 5's DoD item "switching between pre-ePBS and post-ePBS timing requires only a
// config change — proven by test".
//
// `SlotSchedule` is data and every rule is supposed to read its deadlines off
// the timeline it was handed rather than reaching for a constant. Nothing tested
// that: every other test in the repository analyses with mainnet timing, so a
// rule that hard-coded 4 seconds would have passed all of them.
//
// The demonstration uses a real corpus record rather than a synthetic timeline.
// `p2p-degraded-prysm-r06` measured block arrival at roughly +5.6s, which
// overspends mainnet's 4s attestation budget and earns `local.p2p_degraded`.
// Widen the budget past that arrival — a fork changing INTERVALS_PER_SLOT would
// — and the same observations must stop being a propagation fault, with no code
// change anywhere. If this test ever fails, some rule has started deciding
// timing from a literal instead of from the schedule.
func TestAnalyze_TimingFollowsTheScheduleNotTheCode(t *testing.T) {
	t.Parallel()

	dir := filepath.Join("..", "..", "test", "corpus", "p2p-degraded-prysm-r06")
	obs, err := timeline.LoadObservations(filepath.Join(dir, "observations.jsonl"))
	if err != nil {
		t.Fatalf("LoadObservations: %v", err)
	}
	samples, err := timeline.LoadSamples(filepath.Join(dir, "samples.jsonl"))
	if err != nil {
		t.Fatalf("LoadSamples: %v", err)
	}

	// Only the attestation budget moves. Slot duration stays at mainnet's 12s on
	// purpose: the inclusion window is derived from it, and widening the slot
	// would push that window past the record's own collection_completed
	// timestamp, so Replay would reject the fixture before any rule ran. Holding
	// the slot fixed isolates the one variable this test is about.
	widened := domain.SlotSchedule{
		SecondsPerSlot:      12 * time.Second,
		AttestationDeadline: 8 * time.Second,
		AggregationDeadline: 10 * time.Second,
	}
	if err := widened.Validate(); err != nil {
		t.Fatalf("the hypothetical schedule is not a valid one: %v", err)
	}

	mainnetTL, err := timeline.ReplayWithSamples(obs, samples, domain.MainnetPreEPBS())
	if err != nil {
		t.Fatalf("replay under mainnet timing: %v", err)
	}
	widenedTL, err := timeline.ReplayWithSamples(obs, samples, widened)
	if err != nil {
		t.Fatalf("replay under the widened schedule: %v", err)
	}

	underMainnet := rca.Analyze(mainnetTL, rca.DefaultConfig())
	underWidened := rca.Analyze(widenedTL, rca.DefaultConfig())

	if underMainnet.Cause != domain.CauseP2PDegraded {
		t.Fatalf("under mainnet timing this record should be %q, got %q — the fixture changed and this test's premise no longer holds",
			domain.CauseP2PDegraded, underMainnet.Cause)
	}
	if underWidened.Cause == underMainnet.Cause {
		t.Errorf("widening the attestation budget to %s left the verdict at %q; a rule is deciding timing from a literal rather than from SlotSchedule",
			widened.AttestationDeadline, underWidened.Cause)
	}
	t.Logf("same observations: %s deadline -> %q; %s deadline -> %q",
		domain.MainnetPreEPBS().AttestationDeadline, underMainnet.Cause,
		widened.AttestationDeadline, underWidened.Cause)
}
