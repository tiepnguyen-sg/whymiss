package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/clock"
	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/rca"
	"github.com/tiepnguyen-sg/whymiss/internal/source"
	"github.com/tiepnguyen-sg/whymiss/internal/source/beaconapi"
	"github.com/tiepnguyen-sg/whymiss/internal/timeline"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
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
	recordID := fs.String("record-id", "", "unique corpus record id; defaults to the scenario recipe id")
	outDir := fs.String("out", "", "output directory for the corpus scenario")
	enclave := fs.String("enclave", "whymiss-devnet", "Kurtosis enclave name")
	beaconAPI := fs.String("beacon-api", "", "beacon API base URL for the watched validator's client (e.g. http://127.0.0.1:60466)")
	ntpServer := fs.String("ntp-server", "pool.ntp.org", "NTP server used to stamp observations with a real clock offset")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *scenarioID == "" || *outDir == "" || *beaconAPI == "" {
		return fmt.Errorf("--scenario, --out, and --beacon-api are all required")
	}
	if !validScenarioID(*scenarioID) {
		return fmt.Errorf("--scenario %q is not a valid scenario id", *scenarioID)
	}

	scenarioPath := filepath.Join("tools", "faultinjector", "scenarios", *scenarioID+".yaml")
	scenario, err := LoadScenario(scenarioPath)
	if err != nil {
		return err
	}
	if *recordID != "" {
		if !validScenarioID(*recordID) {
			return fmt.Errorf("--record-id %q is not a valid scenario id", *recordID)
		}
		scenario.ID = *recordID
	}
	if filepath.Base(filepath.Clean(*outDir)) != scenario.ID {
		return fmt.Errorf("output directory basename %q must match record id %q", filepath.Base(filepath.Clean(*outDir)), scenario.ID)
	}

	if err := os.MkdirAll(*outDir, 0o750); err != nil {
		return fmt.Errorf("create output dir %s: %w", *outDir, err)
	}

	return RunScenario(ctx, scenario, *enclave, *beaconAPI, *outDir, *ntpServer)
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
func RunScenario(ctx context.Context, s Scenario, enclave, beaconAPI, outDir, ntpServer string) error {
	if faultRequiresRoot(s.Fault.Kind) && os.Geteuid() != 0 && !dockerDesktopFaultFallback(s.Fault.Kind) {
		return fmt.Errorf("fault %q requires host root privileges; rerun faultinjector with sudo", s.Fault.Kind)
	}
	sampler, err := clock.New(clock.Config{Servers: []string{ntpServer}, Timeout: 5 * time.Second, MaxAttempts: 3})
	if err != nil {
		return fmt.Errorf("configure clock sampler: %w", err)
	}

	obs, err := NewObserver(ctx, beaconAPI)
	if err != nil {
		return fmt.Errorf("connect to beacon API %s: %w", beaconAPI, err)
	}
	metricsSampler := source.NewMetricsSampler()
	timingClient, consensusClient, timingURL, err := prepareHeadTiming(ctx, beaconAPI, enclave, s.TimingTarget)
	if err != nil {
		return fmt.Errorf("prepare block timing: %w", err)
	}
	var (
		baselineTimingClient *beaconapi.Client
		baselineConsensus    source.ConsensusClient
		baselineTimingURL    string
	)
	if s.BaselineTarget != "" {
		baselineBeaconAPI, err := resolveKurtosisPort(ctx, enclave, s.BaselineTarget, "http")
		if err != nil {
			return fmt.Errorf("resolve baseline beacon API: %w", err)
		}
		baselineTimingClient, baselineConsensus, baselineTimingURL, err = prepareHeadTiming(ctx, baselineBeaconAPI, enclave, s.BaselineTarget)
		if err != nil {
			return fmt.Errorf("prepare network baseline: %w", err)
		}
	}

	// Before anything is recorded: prove the devnet is still a network. See
	// preflightPeering's doc comment for the fourteen records that made this
	// necessary.
	if err := preflightPeering(ctx, enclave); err != nil {
		return err
	}

	fault, err := NewFault(s.Fault)
	if err != nil {
		return err
	}

	// Before the duty is chosen — see collectEngineBaseline's doc comment for
	// why the ordering is forced.
	var engineBaselineSample *domain.MetricSample
	if s.SampleEngineBaseline {
		engineBaselineSample, err = collectEngineBaseline(ctx, metricsSampler, consensusClient, timingURL, obs)
		if err != nil {
			return fmt.Errorf("collect Engine baseline: %w", err)
		}
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
	watchDeadline := slotStart.Add(3 * obs.SecondsPerSlot)

	fmt.Printf("faultinjector: watching validator %d at slot %d (starts %s), fault=%s target=%s duration=%s\n",
		s.ValidatorIndex, dutySlot, slotStart.Format(time.RFC3339), s.Fault.Kind, s.Target, s.Duration)

	// Sample close to the duty, not before duty discovery: finding a suitable
	// slot can take most of an epoch, which made the previous "up front"
	// reading stale before the fault even began. Twenty seconds leaves enough
	// room for the sampler's three bounded attempts and still applies the fault
	// eight seconds before the slot.
	waitUntil(ctx, slotStart.Add(-20*time.Second))
	if err := ctx.Err(); err != nil {
		return err
	}
	reading, err := sampler.Sample(ctx)
	if err != nil {
		return fmt.Errorf("sample clock offset from %s: %w", ntpServer, err)
	}
	if s.Expect.Cause != string(domain.CauseHostClockDrift) && absoluteDuration(reading.Offset) > rca.DefaultConfig().ClockOffsetMax {
		return fmt.Errorf("clock offset %s from %s exceeds the %s corpus trust limit; choose a healthy fixed NTP source before injecting the fault", reading.Offset, ntpServer, rca.DefaultConfig().ClockOffsetMax)
	}
	var engineBefore *source.EngineCounters
	if s.SampleEngineCalls {
		waitUntil(ctx, slotStart.Add(-12*time.Second))
		if err := ctx.Err(); err != nil {
			return err
		}
		sampleCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		counters, sampleErr := metricsSampler.SampleEngineCounters(sampleCtx, consensusClient, timingURL)
		cancel()
		if sampleErr != nil {
			return fmt.Errorf("sample Engine counters before slot: %w", sampleErr)
		}
		engineBefore = &counters
	}
	waitUntil(ctx, slotStart.Add(-8*time.Second))
	if err := ctx.Err(); err != nil {
		return err
	}
	headCtx, stopHead := context.WithCancel(ctx)
	headResult := make(chan headTimingResult, 1)
	headDone := make(chan struct{})
	go func() {
		defer close(headDone)
		observeHeadTiming(headCtx, metricsSampler, timingClient, consensusClient, timingURL, domain.Slot(dutySlot), watchDeadline, s.SampleEngineCalls, headResult)
	}()
	defer func() {
		stopHead()
		<-headDone
	}()
	var baselineResult chan headTimingResult
	var baselineDone chan struct{}
	if baselineTimingClient != nil {
		baselineCtx, stopBaseline := context.WithCancel(ctx)
		baselineResult = make(chan headTimingResult, 1)
		baselineDone = make(chan struct{})
		go func() {
			defer close(baselineDone)
			observeHeadTiming(baselineCtx, metricsSampler, baselineTimingClient, baselineConsensus, baselineTimingURL, domain.Slot(dutySlot), watchDeadline, false, baselineResult)
		}()
		defer func() {
			stopBaseline()
			<-baselineDone
		}()
	}

	fmt.Println("faultinjector: applying fault")
	revert, err := fault.Apply(ctx, enclave, s.Target)
	if err != nil {
		return fmt.Errorf("apply fault: %w", err)
	}

	// doRevert guards against ever leaving a fault active on the devnet
	// past this function's return, regardless of how it returns. Without
	// this, an error from polling (a single non-404 HTTP response, a
	// timeout — anything that isn't the happy path) would return early and
	// skip reverting entirely: a real run left a 90%-loss netem qdisc
	// permanently attached to a container's veth this way, silently
	// corrupting every subsequent scenario run against the same devnet
	// until it was found and cleared by hand. The deferred call below runs
	// on every exit path. A failed or cancelled revert is not marked complete,
	// so cleanup can retry it with a fresh bounded context.
	var revertMu sync.Mutex
	reverted := false
	doRevert := func(ctx context.Context) error {
		revertMu.Lock()
		defer revertMu.Unlock()
		if reverted {
			return nil
		}
		if err := revert(ctx); err != nil {
			return err
		}
		reverted = true
		return nil
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if rerr := doRevert(cleanupCtx); rerr != nil {
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
		if err := ctx.Err(); err != nil {
			revertDone <- err
			return
		}
		fmt.Println("faultinjector: reverting fault")
		revertDone <- doRevert(ctx)
	}()

	type pressureResult struct {
		avg10        float64
		metric, file string
		sampledAt    time.Time
		err          error
	}
	var pressureDone chan pressureResult
	if s.SamplePressure != "" {
		containerID, err := dockerContainerID(ctx, enclave, s.Target)
		if err != nil {
			return fmt.Errorf("sample_pressure: %w", err)
		}
		pressureCtx, cancelPressure := context.WithCancel(ctx)
		defer cancelPressure()
		pressureDone = make(chan pressureResult, 1)
		go func() {
			// Sample while the fault is active and before slot end, matching the
			// live collector's evidence window. Two-thirds of a slot gives PSI's
			// avg10 enough fault exposure and leaves Docker Desktop helper latency.
			waitUntil(pressureCtx, slotStart.Add(obs.SecondsPerSlot*2/3))
			result := pressureResult{}
			if err := pressureCtx.Err(); err != nil {
				result.err = err
				pressureDone <- result
				return
			}
			switch s.SamplePressure {
			case "memory":
				result.avg10, result.err = SampleMemoryPressure(pressureCtx, containerID)
				result.file, result.metric = "memory.pressure", "host_mem_pressure_pct"
			default:
				result.avg10, result.err = SampleIOPressure(pressureCtx, containerID)
				result.file, result.metric = "io.pressure", "host_iowait_pct"
			}
			result.sampledAt = time.Now().UTC()
			pressureDone <- result
		}()
	}

	type peerResult struct {
		count     float64
		sampledAt time.Time
		source    domain.SourceID
		err       error
	}
	var peerDone chan peerResult
	if s.PeerCountTarget != "" {
		peerDone = make(chan peerResult, 1)
		go func() {
			waitUntil(ctx, slotStart)
			result := peerResult{}
			if err := ctx.Err(); err != nil {
				result.err = err
				peerDone <- result
				return
			}
			// Retried, because this sample is taken from a node the scenario is
			// actively degrading. A p2p-degrading fault makes the target slower
			// to answer everything, its own Beacon API included, so a single
			// timeout here is the fault working rather than evidence about
			// peering — and abandoning the record for it throws away a run that
			// otherwise measured exactly what it set out to. Three attempts
			// spread across the duty window; the record is still abandoned if
			// none succeeds, since a p2p scenario without a peer count is
			// missing evidence R-200 requires.
			var sample domain.MetricSample
			var err error
			for attempt := range peerCountAttempts {
				if attempt > 0 {
					waitUntil(ctx, time.Now().Add(peerCountRetryDelay))
					if ctxErr := ctx.Err(); ctxErr != nil {
						err = ctxErr
						break
					}
				}
				sample, err = SamplePeerCount(ctx, enclave, s.PeerCountTarget)
				if err == nil {
					break
				}
				fmt.Printf("faultinjector: peer count attempt %d/%d failed: %v\n", attempt+1, peerCountAttempts, err)
			}
			result.count, result.err = sample.Value, err
			result.sampledAt, result.source = sample.At, sample.Source
			peerDone <- result
		}()
	}

	var (
		blockResult   blockStatus
		blockErr      error
		publishedAt   time.Time
		publishedRoot string
		published     bool
		publishErr    error
	)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		blockResult, blockErr = obs.PollBlockSeen(ctx, dutySlot, watchDeadline)
	}()
	go func() {
		defer wg.Done()
		publishedAt, publishedRoot, published, publishErr = obs.PollAttestationPublished(ctx, dutySlot, d, watchDeadline)
	}()
	wg.Wait()

	if blockErr != nil {
		return fmt.Errorf("poll block: %w", blockErr)
	}
	if publishErr != nil {
		return fmt.Errorf("poll attestation publish: %w", publishErr)
	}

	outcome := dutyOutcome{
		BlockFound: blockResult.Found, BlockSkipped: blockResult.Skipped, BlockRoot: blockResult.Root,
		ProposerIndex: blockResult.ProposerIndex, ProposerKnown: blockResult.Found, BlockSeenAt: blockResult.At,
		Published: published, PublishedAt: publishedAt, PublishedRoot: publishedRoot,
	}

	if err := <-revertDone; err != nil {
		return fmt.Errorf("revert fault: %w", err)
	}

	measured, err := waitHeadTiming(ctx, headResult, headDone)
	if err != nil {
		return fmt.Errorf("wait for watched head timing: %w", err)
	}
	if measured.HeadErr != nil {
		fmt.Printf("faultinjector: head observation unavailable: %v\n", measured.HeadErr)
	} else {
		outcome.HeadFound = true
		outcome.BlockSkipped = false
		outcome.HeadUpdatedAt = measured.Head.At
		outcome.HeadRoot, _ = measured.Head.Attr(domain.AttrBlockRoot)
		if measured.TimingErr != nil {
			fmt.Printf("faultinjector: block timing unavailable: %v\n", measured.TimingErr)
		} else if measured.Timing.Slot != domain.Slot(dutySlot) {
			fmt.Printf("faultinjector: rejected cross-slot block timing: metric slot %d, duty slot %d\n", measured.Timing.Slot, dutySlot)
		} else {
			blockAt := slotStart.Add(measured.Timing.Propagation)
			if blockAt.After(measured.Head.At.Add(reading.Offset)) {
				fmt.Printf("faultinjector: rejected stale block timing: arrival %s after head %s\n", blockAt, measured.Head.At.Add(reading.Offset))
			} else {
				outcome.BlockTimingMeasured = true
				outcome.BlockFound = true
				outcome.BlockSkipped = false
				outcome.BlockSeenAt = blockAt
				if outcome.BlockRoot == "" {
					outcome.BlockRoot = outcome.HeadRoot
				}
				fmt.Printf("faultinjector: sampled block propagation=%s\n", measured.Timing.Propagation)
			}
		}
		if s.SampleEngineCalls {
			switch {
			case measured.EngineErr != nil:
				fmt.Printf("faultinjector: Engine counters after slot unavailable: %v\n", measured.EngineErr)
			case engineBefore == nil || measured.EngineAfter == nil:
				fmt.Println("faultinjector: Engine counter window incomplete")
			default:
				calls, err := source.EngineCallsBetween(*engineBefore, *measured.EngineAfter)
				if err != nil {
					fmt.Printf("faultinjector: Engine counter window ambiguous: %v\n", err)
				} else {
					outcome.EngineCalls = calls
					var callCount uint64
					for _, call := range calls {
						callCount += call.Count
					}
					fmt.Printf("faultinjector: isolated %d Engine calls across %d exact method windows\n", callCount, len(calls))
				}
			}
		}
	}
	if baselineResult != nil {
		baselineMeasured, err := waitHeadTiming(ctx, baselineResult, baselineDone)
		if err != nil {
			return fmt.Errorf("wait for network baseline head: %w", err)
		}
		if baselineMeasured.HeadErr != nil {
			return fmt.Errorf("observe network baseline head: %w", baselineMeasured.HeadErr)
		}
		if baselineMeasured.TimingErr != nil {
			return fmt.Errorf("sample network baseline timing: %w", baselineMeasured.TimingErr)
		}
		if baselineMeasured.Timing.Slot != domain.Slot(dutySlot) {
			return fmt.Errorf("network baseline timing is for slot %d, duty slot is %d", baselineMeasured.Timing.Slot, dutySlot)
		}
		outcome.Network = &domain.NetworkBaseline{
			Slot: domain.Slot(dutySlot), BlockArrivalP50: baselineMeasured.Timing.Propagation,
			BlockArrivalP90: baselineMeasured.Timing.Propagation, SampleCount: 1, Source: domain.SourcePromScrape,
		}
		outcome.NetworkSampledAt = baselineMeasured.Timing.SampledAt
		fmt.Printf("faultinjector: sampled independent network baseline=%s from %s\n", baselineMeasured.Timing.Propagation, s.BaselineTarget)
	}

	if pressureDone != nil {
		pressure := <-pressureDone
		if pressure.err != nil {
			return fmt.Errorf("sample_pressure: %w", pressure.err)
		}
		outcome.HostPressure, outcome.HostPressureMetric, outcome.HostSampledAt = &pressure.avg10, pressure.metric, pressure.sampledAt
		fmt.Printf("faultinjector: sampled %s some avg10=%.2f%% for %s\n", pressure.file, pressure.avg10, s.Target)
	}
	if peerDone != nil {
		peer := <-peerDone
		if peer.err != nil {
			return fmt.Errorf("peer_count_target: %w", peer.err)
		}
		outcome.PeerCount, outcome.PeerCountSampledAt = &peer.count, peer.sampledAt
		outcome.PeerCountSource = peer.source
		fmt.Printf("faultinjector: sampled peer_count=%.0f for %s at duty start\n", peer.count, s.PeerCountTarget)
	}

	inclusionEndSlot := domain.Slot(dutySlot).LastAttestationInclusionSlot()
	inclusionSlots := inclusionEndSlot - domain.Slot(dutySlot)
	if inclusionSlots > domain.SlotsPerEpoch*2-1 {
		return fmt.Errorf("invalid inclusion window of %d slots", inclusionSlots)
	}
	// windowEnd is the wall-clock floor domain.Timeline.Validate requires
	// CollectionCompletedAt to be at or after (ADR-0016/0018) — the block for
	// the window's final slot can be observed near that slot's *start*, well
	// before the slot itself has elapsed, so CheckInclusion returning is not
	// by itself proof the window is closed. collectionDeadline stays a looser
	// upper bound: CheckInclusion's own poll budget, same margin
	// internal/app/duty_tracking.go's collectionWindowEnd uses.
	windowEnd := domain.Slot(dutySlot).CollectionWindowEnd(slotStart, obs.SecondsPerSlot)
	collectionDeadline := windowEnd.Add(inclusionPollSlack * obs.SecondsPerSlot)
	outcome.IncludedInSlot, outcome.IncludedAt, outcome.IncludedRoot, outcome.HeadCorrect, outcome.TargetCorrect, outcome.Included, err = obs.CheckInclusion(ctx, dutySlot, d, uint64(inclusionEndSlot), collectionDeadline)
	if err != nil {
		return fmt.Errorf("check inclusion: %w", err)
	}
	waitUntil(ctx, windowEnd)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	completionReading, err := sampler.Sample(ctx)
	if err != nil {
		return fmt.Errorf("sample completion clock offset from %s: %w", ntpServer, err)
	}
	if s.Expect.Cause != string(domain.CauseHostClockDrift) && absoluteDuration(completionReading.Offset) > rca.DefaultConfig().ClockOffsetMax {
		return fmt.Errorf("completion clock offset %s from %s exceeds the %s corpus trust limit", completionReading.Offset, ntpServer, rca.DefaultConfig().ClockOffsetMax)
	}
	outcome.CollectionCompletedAt = time.Now().UTC()

	readings := []clock.Reading{reading, completionReading}
	observations, err := buildObservations(s, dutySlot, slotStart, dutyAt, outcome, readings)
	if err != nil {
		return err
	}

	// The samples this run collected have to go into the replay, not just into
	// the record. Checking the verdict without them evaluates a timeline the
	// engine will never see again: R-300 reads its baseline from tl.Samples, so a
	// perfectly good local.el_slow record would be judged against a rule that
	// declined for want of an input this run had already measured, and reported
	// as a failed scenario.
	var checkSamples []domain.MetricSample
	if engineBaselineSample != nil {
		checkSamples = append(checkSamples, *engineBaselineSample)
	}
	replayed, err := timeline.ReplayWithSamples(observations, checkSamples, domain.MainnetPreEPBS())
	if err != nil {
		return fmt.Errorf("replay generated observations before writing: %w", err)
	}
	verdict := rca.Analyze(replayed, rca.DefaultConfig())
	wantCause := domain.CauseID(s.Expect.Cause)
	if s.Expect.SubCause != "" {
		wantCause = domain.CauseID(s.Expect.SubCause)
	}

	recipeID := s.RecipeID
	if recipeID == "" {
		recipeID = s.ID
	}
	manifest := Manifest{
		CorpusFormatVersion:    2,
		GeneratorEngineVersion: rca.EngineVersion,
		ID:                     s.ID, RecipeID: recipeID, Description: s.Description, Expect: s.Expect,
		Slot: dutySlot, ValidatorIndex: s.ValidatorIndex,
		FaultKind: s.Fault.Kind, FaultTarget: s.Target, Duration: s.Duration,
		GeneratedAt:  time.Now().UTC(),
		ClockSamples: clockProvenance(readings),
	}
	readme := renderReadme(s, dutySlot, outcome)

	if err := WriteCorpusScenario(outDir, manifest, observations, checkSamples, readme); err != nil {
		return err
	}

	fmt.Printf("faultinjector: wrote %s (block_found=%v block_skipped=%v included=%v verdict=%s confidence=%s)\n",
		outDir, outcome.BlockFound, outcome.BlockSkipped, outcome.Included, verdict.ReportedCause(), verdict.Confidence)
	if verdict.ReportedCause() != wantCause {
		return fmt.Errorf("recorded evidence yielded verdict %q (%s), expected %q; fixture was preserved for diagnosis but does not pass the labelled scenario",
			verdict.ReportedCause(), verdict.Confidence, wantCause)
	}
	return nil
}

func faultRequiresRoot(kind string) bool {
	switch kind {
	case "netem", "cgroup_io", "cgroup_cpu", "cgroup_mem":
		return true
	default:
		return false
	}
}

func absoluteDuration(value time.Duration) time.Duration {
	if value == time.Duration(math.MinInt64) {
		return time.Duration(math.MaxInt64)
	}
	if value < 0 {
		return -value
	}
	return value
}

// minDutyLead is how far in the future a candidate duty slot must be to be
// usable: the fault has to be applied before the slot starts, and
// RunScenario itself waits until slotStart-8s before doing so. A few
// seconds of headroom past that keeps a slot from being picked that is
// already effectively upon us.
const minDutyLead = 25 * time.Second

// peerCountAttempts and peerCountRetryDelay bound the retry above. Two seconds
// apart keeps all three inside the duty window a fault is live for.
const (
	peerCountAttempts   = 3
	peerCountRetryDelay = 2 * time.Second
)

// inclusionPollSlack is extra time beyond domain's own required minimum
// (domain.Slot.CollectionWindowEnd) given to CheckInclusion's poll loop, so
// a slow-but-healthy poll isn't cut off right at the instant validation
// would first accept a completion marker. Same margin
// internal/app/duty_tracking.go's collectionWindowEnd uses; a separate local
// constant because that one lives in a different package.
const inclusionPollSlack = 2

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
	for vi := s.ValidatorCandidates[0]; ; vi++ {
		out = append(out, vi)
		if vi == s.ValidatorCandidates[1] {
			break
		}
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
	if s.RecipeID != "" && s.RecipeID != s.ID {
		fmt.Fprintf(&extra, "- Source recipe: %s\n", s.RecipeID)
	}
	if o.HostPressure != nil {
		fmt.Fprintf(&extra, "- Host %s pressure (some avg10): %.2f%%\n", o.HostPressureMetric, *o.HostPressure)
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
