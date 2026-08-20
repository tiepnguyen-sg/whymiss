package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/CHANGEME/whymiss/internal/domain"
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

	dutySlot, d, dutyAt, err := findCleanDuty(ctx, obs, startEpoch, s.ValidatorIndex, s.AvoidProposerValidators)
	if err != nil {
		return err
	}
	slotStart := obs.SlotStart(dutySlot)

	fmt.Printf("faultinjector: watching validator %d at slot %d (starts %s), fault=%s target=%s duration=%s\n",
		s.ValidatorIndex, dutySlot, slotStart.Format(time.RFC3339), s.Fault.Kind, s.Target, s.Duration)

	waitUntil(ctx, slotStart.Add(-2*time.Second))

	fmt.Println("faultinjector: applying fault")
	revert, err := fault.Apply(ctx, enclave, s.Target)
	if err != nil {
		return fmt.Errorf("apply fault: %w", err)
	}

	waitUntil(ctx, time.Now().Add(s.Duration))

	fmt.Println("faultinjector: reverting fault")
	if err := revert(ctx); err != nil {
		return fmt.Errorf("revert fault: %w", err)
	}

	watchDeadline := slotStart.Add(3 * obs.SecondsPerSlot)
	blockRoot, proposerIndex, seenAt, blockFound, err := obs.PollBlockSeen(ctx, dutySlot, watchDeadline)
	if err != nil {
		return fmt.Errorf("poll block: %w", err)
	}

	publishedAt, published, err := obs.PollAttestationPublished(ctx, dutySlot, d, watchDeadline)
	if err != nil {
		return fmt.Errorf("poll attestation publish: %w", err)
	}

	var (
		includedInSlot uint64
		includedAt     time.Time
		included       bool
	)
	if blockFound {
		includedInSlot, includedAt, included, err = obs.CheckInclusion(ctx, dutySlot, d, dutySlot+2, watchDeadline.Add(2*obs.SecondsPerSlot))
		if err != nil {
			return fmt.Errorf("check inclusion: %w", err)
		}
	}

	observations, err := buildObservations(s, dutySlot, slotStart, dutyAt,
		blockFound, blockRoot, proposerIndex, seenAt,
		published, publishedAt,
		included, includedInSlot, includedAt)
	if err != nil {
		return err
	}

	manifest := Manifest{
		ID: s.ID, Description: s.Description, Expect: s.Expect,
		Slot: dutySlot, ValidatorIndex: s.ValidatorIndex,
		FaultKind: s.Fault.Kind, FaultTarget: s.Target, Duration: s.Duration,
		GeneratedAt: time.Now().UTC(),
	}
	readme := renderReadme(s, dutySlot, blockFound, published, included)

	if err := WriteCorpusScenario(outDir, manifest, observations, readme); err != nil {
		return err
	}

	fmt.Printf("faultinjector: wrote %s (block_found=%v included=%v)\n", outDir, blockFound, included)
	return nil
}

// findCleanDuty finds validatorIndex's attester duty starting from startEpoch,
// skipping an epoch whose assigned slot's proposer falls inside avoid (see
// Scenario.AvoidProposerValidators) — attester duty is fixed to one slot per
// epoch, so avoiding a confounding slot means trying a later epoch, not a later
// slot within the same one.
func findCleanDuty(ctx context.Context, obs *Observer, startEpoch, validatorIndex uint64, avoid *[2]uint64) (slot uint64, d duty, at time.Time, err error) {
	// The standard beacon API only guarantees duties are computable one epoch
	// ahead — querying further reliably 400s (verified against both Lighthouse
	// and Prysm here). So there is exactly one epoch worth trying per
	// invocation; a proposer confound in it means "run the command again", not
	// "look further ahead" — retrying naturally lands on a freshly-shuffled
	// epoch next time.
	slot, d, at, err = obs.FetchDuty(ctx, startEpoch, validatorIndex)
	if err != nil {
		return 0, duty{}, time.Time{}, fmt.Errorf("epoch %d: %w", startEpoch, err)
	}
	if avoid == nil {
		return slot, d, at, nil
	}
	proposer, err := obs.FetchProposer(ctx, slot)
	if err != nil {
		return 0, duty{}, time.Time{}, fmt.Errorf("check proposer for slot %d: %w", slot, err)
	}
	if proposer < avoid[0] || proposer > avoid[1] {
		return slot, d, at, nil
	}
	return 0, duty{}, time.Time{}, fmt.Errorf(
		"epoch %d's duty slot %d has proposer %d, inside the fault's own range %v — this would confound the scenario; run the command again for a freshly-shuffled epoch",
		startEpoch, slot, proposer, *avoid)
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

func renderReadme(s Scenario, slot uint64, blockFound, published, included bool) string {
	return fmt.Sprintf(`# %s

%s

## What was broken

Fault: %s applied to %s for %s, around slot %d.

## Recorded outcome

- Block observed for the duty slot: %v
- Attestation published (seen in the pool): %v
- Attestation included: %v

## Expected taxonomy label

- cause: %s
- sub_cause: %s
- confidence: %s

Generated by tools/faultinjector against a live Kurtosis devnet
(test/e2e/kurtosis). See manifest.yaml for full provenance and
observations.jsonl for the raw recorded facts.
`, s.ID, s.Description, s.Fault.Kind, s.Target, s.Duration, slot, blockFound, published, included,
		s.Expect.Cause, s.Expect.SubCause, s.Expect.Confidence)
}
