package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "faultinjector:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: faultinjector run --scenario <id> --out <dir>")
	}

	switch args[0] {
	case "run":
		return runScenarioCmd(ctx, args[1:])
	default:
		return fmt.Errorf("unknown command %q — only \"run\" is supported", args[0])
	}
}

func runScenarioCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	scenarioID := fs.String("scenario", "", "scenario id (matches a file under tools/faultinjector/scenarios/)")
	outDir := fs.String("out", "", "output directory for the corpus scenario")
	enclave := fs.String("enclave", "whymiss-devnet", "Kurtosis enclave name")
	beaconAPI := fs.String("beacon-api", "", "beacon API base URL for the watched validator's client (e.g. http://127.0.0.1:60466)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *scenarioID == "" || *outDir == "" || *beaconAPI == "" {
		return fmt.Errorf("--scenario, --out, and --beacon-api are all required")
	}

	scenarioPath := filepath.Join("tools", "faultinjector", "scenarios", *scenarioID+".yaml")
	scenario, err := LoadScenario(scenarioPath)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(*outDir, 0o750); err != nil {
		return fmt.Errorf("create output dir %s: %w", *outDir, err)
	}

	return RunScenario(ctx, scenario, *enclave, *beaconAPI, *outDir)
}

// RunScenario executes scenario against a live devnet end to end: it records the
// duty and slot start, applies the fault, holds it for the declared duration,
// reverts it, watches for the outcome, and writes the corpus scenario to outDir.
//
// Every value that ends up in observations.jsonl is something this function
// actually measured against beaconAPI during this run — never synthesized to
// match Expect (docs/BUILD_PROMPT.md §8). Expect is the prediction the scenario
// file carries in; what actually happened is recorded regardless of whether it
// matches, because a scenario whose recorded facts were adjusted to match its
// own label would be worthless as a corpus fixture.
func RunScenario(ctx context.Context, s Scenario, enclave, beaconAPI, outDir string) error {
	obs, err := NewObserver(ctx, beaconAPI)
	if err != nil {
		return fmt.Errorf("connect to beacon API %s: %w", beaconAPI, err)
	}

	fault, err := NewFault(s.Fault)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	sinceGenesis := now.Sub(obs.GenesisTime)
	if sinceGenesis < 0 {
		return fmt.Errorf("chain has not reached genesis yet (%s remaining) — nothing to watch", -sinceGenesis)
	}
	currentSlot := uint64(sinceGenesis / obs.SecondsPerSlot) //nolint:gosec // G115: sinceGenesis just checked non-negative
	startEpoch := currentSlot/domain.SlotsPerEpoch + 1       // next epoch: enough lead time to act before the duty slot

	dutySlot, watched, d, dutyAt, err := findCleanDuty(ctx, obs, startEpoch, watchedValidators(s), s.AvoidProposerValidators, s.RequireProposerValidators)
	if err != nil {
		return err
	}
	// Pin the scenario to whichever candidate was actually chosen, so the
	// manifest, the observations, and the log line below all record the one
	// real validator this run watched (s is a value parameter — this does
	// not leak back to the caller's copy).
	s.ValidatorIndex = watched
	slotStart := obs.SlotStart(dutySlot)

	fmt.Printf("faultinjector: watching validator %d at slot %d (starts %s), fault=%s target=%s duration=%s\n",
		s.ValidatorIndex, dutySlot, slotStart.Format(time.RFC3339), s.Fault.Kind, s.Target, s.Duration)

	waitUntil(ctx, slotStart.Add(-2*time.Second))

	fmt.Println("faultinjector: applying fault")
	revert, err := fault.Apply(ctx, enclave, s.Target)
	if err != nil {
		return fmt.Errorf("apply fault: %w", err)
	}

	// revertOnce guards against ever leaving a fault active on the devnet
	// past this function's return, regardless of how it returns. Without
	// this, an error from polling (a single non-404 HTTP response, a
	// timeout — anything that isn't the happy path) would return early and
	// skip reverting entirely: a real run left a 90%-loss netem qdisc
	// permanently attached to a container's veth this way, silently
	// corrupting every subsequent scenario run against the same devnet
	// until it was found and cleared by hand. The deferred call below runs
	// on every exit path; revertOnce just keeps it from double-reverting
	// when the happy path already reverted on its own schedule below.
	var revertOnce sync.Once
	var revertErr error
	doRevert := func(ctx context.Context) error {
		revertOnce.Do(func() { revertErr = revert(ctx) })
		return revertErr
	}
	defer func() {
		if rerr := doRevert(context.WithoutCancel(ctx)); rerr != nil {
			fmt.Fprintln(os.Stderr, "faultinjector: revert on cleanup:", rerr)
		}
	}()

	// The fault stays active on its own clock (revertAt) while observation
	// starts immediately, in parallel — not after revert returns. An earlier
	// version waited for revert before polling at all, which meant no
	// observation could ever be timestamped earlier than roughly
	// slotStart+duration regardless of when the block or attestation actually
	// appeared: every recorded "delay" was bounded below by how long the fault
	// was held, not by anything the fault caused. Watching while the fault is
	// still in effect is what makes "did this appear before or after revert"
	// a real question the observations can answer.
	revertAt := time.Now().Add(s.Duration)
	revertDone := make(chan error, 1)
	go func() {
		waitUntil(ctx, revertAt)
		fmt.Println("faultinjector: reverting fault")
		revertDone <- doRevert(ctx)
	}()

	watchDeadline := slotStart.Add(3 * obs.SecondsPerSlot)

	var (
		blockRoot     string
		proposerIndex uint64
		seenAt        time.Time
		blockFound    bool
		blockErr      error
		publishedAt   time.Time
		published     bool
		publishErr    error
	)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		blockRoot, proposerIndex, seenAt, blockFound, blockErr = obs.PollBlockSeen(ctx, dutySlot, watchDeadline)
	}()
	go func() {
		defer wg.Done()
		publishedAt, published, publishErr = obs.PollAttestationPublished(ctx, dutySlot, d, watchDeadline)
	}()
	wg.Wait()

	if blockErr != nil {
		return fmt.Errorf("poll block: %w", blockErr)
	}
	if publishErr != nil {
		return fmt.Errorf("poll attestation publish: %w", publishErr)
	}

	outcome := dutyOutcome{
		BlockFound: blockFound, BlockRoot: blockRoot, ProposerIndex: proposerIndex, BlockSeenAt: seenAt,
		Published: published, PublishedAt: publishedAt,
	}

	// Waited for here, before sampling below, on purpose: PollBlockSeen and
	// PollAttestationPublished (fixed above to run concurrently with the fault)
	// now typically return within a few seconds, but io.pressure's avg10 is a
	// 10-second decaying average — sampling that early caught the fault only a
	// second or two into being held, before enough real I/O had happened to
	// register any pressure at all (verified: an early version sampled
	// immediately after the poll and read a flat 0.00% every time). Waiting for
	// the fault's full declared duration first gives the average something to
	// have actually averaged. PSI decays smoothly rather than resetting the
	// instant the throttle lifts, so sampling right after revert still reflects
	// the preceding ~duration seconds of real pressure.
	if err := <-revertDone; err != nil {
		return fmt.Errorf("revert fault: %w", err)
	}

	if s.SamplePressure != "" {
		containerID, err := dockerContainerID(ctx, s.Target)
		if err != nil {
			return fmt.Errorf("sample_pressure: %w", err)
		}
		var avg10 float64
		var psiFile, metric string
		switch s.SamplePressure {
		case "memory":
			avg10, err = SampleMemoryPressure(ctx, containerID)
			psiFile, metric = "memory.pressure", "mem_pressure_pct"
		default:
			avg10, err = SampleIOPressure(ctx, containerID)
			psiFile, metric = "io.pressure", "iowait_pct"
		}
		if err != nil {
			return fmt.Errorf("sample_pressure: %w", err)
		}
		outcome.HostPressure, outcome.HostPressureMetric, outcome.HostSampledAt = &avg10, metric, time.Now().UTC()
		fmt.Printf("faultinjector: sampled %s some avg10=%.2f%% for %s\n", psiFile, avg10, s.Target)
	}
	if s.MetricsTarget != "" {
		metricsURL, err := resolveMetricsURL(ctx, enclave, s.MetricsTarget)
		if err != nil {
			return fmt.Errorf("metrics_target: %w", err)
		}
		samples, err := SampleEngineCallDurations(ctx, metricsURL)
		if err != nil {
			return fmt.Errorf("metrics_target: %w", err)
		}
		outcome.EngineSamples, outcome.EngineSampledAt = samples, time.Now().UTC()
		for _, sample := range samples {
			fmt.Printf("faultinjector: sampled engine_call %s=%.2fms\n", sample.Method, sample.DurationMS)
		}
	}
	if s.PeerCountTarget != "" {
		peerCount, err := SamplePeerCount(ctx, enclave, s.PeerCountTarget)
		if err != nil {
			return fmt.Errorf("peer_count_target: %w", err)
		}
		outcome.PeerCount, outcome.PeerCountSampledAt = &peerCount, time.Now().UTC()
		fmt.Printf("faultinjector: sampled peer_count=%.0f for %s\n", peerCount, s.PeerCountTarget)
	}

	if outcome.BlockFound {
		outcome.IncludedInSlot, outcome.IncludedAt, outcome.Included, err = obs.CheckInclusion(ctx, dutySlot, d, dutySlot+2, watchDeadline.Add(2*obs.SecondsPerSlot))
		if err != nil {
			return fmt.Errorf("check inclusion: %w", err)
		}
	}

	observations, err := buildObservations(s, dutySlot, slotStart, dutyAt, outcome)
	if err != nil {
		return err
	}

	manifest := Manifest{
		ID: s.ID, Description: s.Description, Expect: s.Expect,
		Slot: dutySlot, ValidatorIndex: s.ValidatorIndex,
		FaultKind: s.Fault.Kind, FaultTarget: s.Target, Duration: s.Duration,
		GeneratedAt: time.Now().UTC(),
	}
	readme := renderReadme(s, dutySlot, outcome)

	if err := WriteCorpusScenario(outDir, manifest, observations, readme); err != nil {
		return err
	}

	fmt.Printf("faultinjector: wrote %s (block_found=%v included=%v)\n", outDir, outcome.BlockFound, outcome.Included)
	return nil
}

// minDutyLead is how far in the future a candidate duty slot must be to be
// usable: the fault has to be applied before the slot starts, and
// RunScenario itself waits until slotStart-2s before doing so. A few
// seconds of headroom past that keeps a slot from being picked that is
// already effectively upon us.
const minDutyLead = 8 * time.Second

// findCleanDuty picks which validator's attester duty to watch in
// startEpoch: the first candidate (in ascending index order, for
// determinism) whose assigned slot is far enough ahead to still act on and
// whose proposer satisfies the scenario's constraint — outside avoid (see
// Scenario.AvoidProposerValidators), or inside require (see
// Scenario.RequireProposerValidators).
//
// Attester duty is one slot per epoch per validator, so a single candidate
// gives a single slot to accept or reject. Supplying a whole node's
// validator set instead (Scenario.ValidatorCandidates) gives one candidate
// slot per validator, which is what makes a constrained scenario reliably
// satisfiable within one epoch rather than a coin flip — both duty lookups
// cost exactly one request regardless of how many candidates are asked
// about.
func findCleanDuty(ctx context.Context, obs *Observer, startEpoch uint64, candidates []uint64, avoid, require *[2]uint64) (slot, validatorIndex uint64, d duty, at time.Time, err error) {
	// The standard beacon API only guarantees duties are computable one epoch
	// ahead — querying further reliably 400s (verified against both Lighthouse
	// and Prysm here). So there is exactly one epoch worth trying per
	// invocation.
	duties, at, err := obs.FetchDuties(ctx, startEpoch, candidates)
	if err != nil {
		return 0, 0, duty{}, time.Time{}, fmt.Errorf("epoch %d: %w", startEpoch, err)
	}

	var proposers map[uint64]uint64
	if avoid != nil || require != nil {
		proposers, err = obs.FetchProposers(ctx, startEpoch)
		if err != nil {
			return 0, 0, duty{}, time.Time{}, fmt.Errorf("epoch %d: %w", startEpoch, err)
		}
	}

	earliestUsable := time.Now().UTC().Add(minDutyLead)
	var tooSoon, wrongProposer int
	for _, vi := range candidates {
		assignment, ok := duties[vi]
		if !ok {
			continue // no duty for this validator this epoch
		}
		if obs.SlotStart(assignment.Slot).Before(earliestUsable) {
			tooSoon++
			continue
		}
		if proposers != nil {
			proposer, ok := proposers[assignment.Slot]
			if !ok {
				return 0, 0, duty{}, time.Time{}, fmt.Errorf("no proposer duty found for slot %d", assignment.Slot)
			}
			if avoid != nil && proposer >= avoid[0] && proposer <= avoid[1] {
				wrongProposer++
				continue
			}
			if require != nil && (proposer < require[0] || proposer > require[1]) {
				wrongProposer++
				continue
			}
		}
		return assignment.Slot, vi, assignment.Duty, at, nil
	}

	return 0, 0, duty{}, time.Time{}, fmt.Errorf(
		"no usable duty in epoch %d across %d candidate validator(s): %d assigned a slot too soon to act on, %d assigned a slot whose proposer fails the scenario's constraint — widen validator_candidates, or run the command again for a freshly-shuffled epoch",
		startEpoch, len(candidates), tooSoon, wrongProposer)
}

// watchedValidators is the candidate list findCleanDuty chooses from: the
// whole ValidatorCandidates range when the scenario declares one, otherwise
// just its single ValidatorIndex.
func watchedValidators(s Scenario) []uint64 {
	if s.ValidatorCandidates == nil {
		return []uint64{s.ValidatorIndex}
	}
	out := make([]uint64, 0, s.ValidatorCandidates[1]-s.ValidatorCandidates[0]+1)
	for vi := s.ValidatorCandidates[0]; vi <= s.ValidatorCandidates[1]; vi++ {
		out = append(out, vi)
	}
	return out
}

// waitUntil blocks until t or ctx is done, whichever comes first. Negative
// durations (t already passed) return immediately, matching time.Sleep's
// treatment of a non-positive duration.
func waitUntil(ctx context.Context, t time.Time) {
	d := time.Until(t)
	if d <= 0 {
		return
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func renderReadme(s Scenario, slot uint64, o dutyOutcome) string {
	var extra strings.Builder
	if o.HostPressure != nil {
		fmt.Fprintf(&extra, "- Host %s pressure (some avg10): %.2f%%\n", o.HostPressureMetric, *o.HostPressure)
	}
	for _, sample := range o.EngineSamples {
		fmt.Fprintf(&extra, "- Engine API %s (rolling median): %.2fms\n", sample.Method, sample.DurationMS)
	}
	if o.PeerCount != nil {
		fmt.Fprintf(&extra, "- Connected peers: %.0f\n", *o.PeerCount)
	}

	return fmt.Sprintf(`# %s

%s

## What was broken

Fault: %s applied to %s for %s, around slot %d.

## Recorded outcome

- Block observed for the duty slot: %v
- Attestation published (seen in the pool): %v
- Attestation included: %v
%s
## Expected taxonomy label

- cause: %s
- sub_cause: %s
- confidence: %s

Generated by tools/faultinjector against a live Kurtosis devnet
(test/e2e/kurtosis). See manifest.yaml for full provenance and
observations.jsonl for the raw recorded facts.
`, s.ID, s.Description, s.Fault.Kind, s.Target, s.Duration, slot, o.BlockFound, o.Published, o.Included, extra.String(),
		s.Expect.Cause, s.Expect.SubCause, s.Expect.Confidence)
}
