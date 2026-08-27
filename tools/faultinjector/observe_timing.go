package main

import (
	"context"
	"fmt"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/source"
	"github.com/tiepnguyen-sg/whymiss/internal/source/beaconapi"
)

type headTimingResult struct {
	Head        domain.Observation
	HeadErr     error
	Timing      source.BlockTiming
	TimingErr   error
	EngineAfter *source.EngineCounters
	EngineErr   error
}

func waitHeadTiming(ctx context.Context, result <-chan headTimingResult, done <-chan struct{}) (headTimingResult, error) {
	select {
	case measured := <-result:
		return measured, nil
	case <-done:
		select {
		case measured := <-result:
			return measured, nil
		default:
			return headTimingResult{}, fmt.Errorf("head timing observer exited without a result")
		}
	case <-ctx.Done():
		return headTimingResult{}, ctx.Err()
	}
}

// observeHeadForSlot resolves this slot's validated head from the event stream
// and the header poll at once, taking whichever answers first.
//
// The poll alone loses the measurement outright on exactly the runs this harness
// exists to produce. headUpdatedUncached samples /eth/v1/beacon/headers/head
// every 200ms and gives up the moment it reads a slot past the one asked for, so
// a head that advances from slot-1 to slot+1 between two samples is never
// observed. A node under a cl_slow or p2p fault is precisely a node whose head
// advances in bursts while its HTTP server answers slowly: two recipes
// (cl-slow-cpu-lighthouse, p2p-ambiguous-no-baseline-prysm) lost every run this
// way, because the fault degrades the very API the measurement reads. No stage
// got timed and the duty came out healthy with no cause — the fault was real and
// the record said nothing.
//
// The stream cannot lose a transition. /eth/v1/events pushes one head event per
// head change, so a throttled node delays the event rather than dropping it, and
// a slot the stream never announces was genuinely skipped rather than merely
// sampled past — a distinction the poll cannot make at all. The poll is kept
// beside it for a node that does not serve the events endpoint.
//
// Neither channel separates "the node processed this head late" from "the node
// answered us late"; that limitation predates this function and is unchanged by
// it. What changes is that a delayed head is measured instead of lost.
func observeHeadForSlot(ctx context.Context, client *beaconapi.Client, slot domain.Slot, deadline time.Time) (domain.Observation, error) {
	// Cancelling on every return path stops both the stream goroutine and the
	// poll, so neither outlives the slot it was opened for.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type pollOutcome struct {
		obs   domain.Observation
		found bool
		err   error
	}
	polled := make(chan pollOutcome, 1)
	go func() {
		obs, found, err := client.HeadUpdated(ctx, slot, deadline)
		polled <- pollOutcome{obs: obs, found: found, err: err}
	}()

	events := client.Stream(ctx, nil)
	// The deadline is what ends this call negatively, never the poll. A
	// not-found from headUpdatedUncached carries no information on its own: it
	// returns that the instant it samples a slot past this one, which on a
	// bursting node is immediately, and letting it end the wait would report
	// "not observed" while the stream still had its entire window to announce
	// the head. The poll can therefore only ever resolve this call positively.
	timeout := time.NewTimer(time.Until(deadline))
	defer timeout.Stop()

	var pollErr error
	for {
		select {
		case obs, open := <-events:
			if !open {
				// Stream closes only once ctx is done, which the case below
				// already answers for. Stop selecting on a closed channel.
				events = nil
				continue
			}
			if obs.Kind != domain.ObsHeadUpdated {
				continue
			}
			switch {
			case obs.Slot == slot:
				return obs, nil
			case obs.Slot > slot:
				return domain.Observation{}, fmt.Errorf(
					"the event stream announced slot %d as head without ever announcing one for slot %d, so the chain skipped it",
					obs.Slot, slot)
			}
		case result := <-polled:
			if result.found {
				return result.obs, nil
			}
			// Resolved as far as this channel can resolve it. Keep the reason,
			// if any, for the message below and wait the stream out.
			pollErr = result.err
			polled = nil
		case <-timeout.C:
			// select is free to pick the deadline over a head the stream is
			// delivering at the same instant, so take anything already
			// committed to the channel before reporting none.
			if head, ok := queuedHead(events, slot); ok {
				return head, nil
			}
			return domain.Observation{}, headNotObserved(slot, pollErr)
		case <-ctx.Done():
			return domain.Observation{}, ctx.Err()
		}
	}
}

// headNotObserved reports that both channels came up empty. It claims only
// that: unlike the stream announcing a later slot, silence does not distinguish
// a skipped slot from one whose head neither channel delivered in time.
func headNotObserved(slot domain.Slot, pollErr error) error {
	if pollErr != nil {
		return fmt.Errorf(
			"neither the event stream nor the header poll observed a validated head for slot %d before the deadline elapsed; the poll failed with: %w",
			slot, pollErr)
	}
	return fmt.Errorf(
		"neither the event stream nor the header poll observed a validated head for slot %d before the deadline elapsed",
		slot)
}

// queuedHead takes a head observation for slot from the stream if a sender is
// already committed to delivering one, and never blocks waiting for it.
func queuedHead(events <-chan domain.Observation, slot domain.Slot) (domain.Observation, bool) {
	for {
		select {
		case obs, open := <-events:
			if !open {
				return domain.Observation{}, false
			}
			if obs.Kind == domain.ObsHeadUpdated && obs.Slot == slot {
				return obs, true
			}
		default:
			return domain.Observation{}, false
		}
	}
}

// blockTimingPollInterval mirrors the unexported interval
// internal/source.SampleBlockTimingForSlot polls its gauge at. The harness
// cannot import that constant, and 250ms is the value the watcher below is
// tuned against; it only ever costs extra scrapes if the two drift apart.
const blockTimingPollInterval = 250 * time.Millisecond

// watchBlockTimingForSlot waits for a client's latest-value arrival gauge to
// reach slot, tolerating a failed scrape the way beaconapi's head poll already
// does.
//
// source.SampleBlockTimingForSlot abandons the whole watch on a single scrape
// error, and that trade is wrong here for the same reason it was wrong in
// headUpdatedUncached: the faults this harness injects are precisely what makes
// a consensus client miss one metrics deadline. cl-slow-cpu-lighthouse at 5% of
// a core has already lost a run that way. Watching from before the slot rather
// than after the head means many more scrapes across the window, so keeping the
// single-failure abort would have made that recipe more fragile, not less — the
// wider window has to come with the tolerance or it is a regression.
//
// A scrape error is therefore remembered and retried, and only reported if the
// deadline passes with nothing measured. An error from ctx is not retried, since
// nothing later can succeed.
func watchBlockTimingForSlot(ctx context.Context, sampler *source.MetricsSampler, client source.ConsensusClient, metricsURL string, slot domain.Slot, deadline time.Time) (source.BlockTiming, error) {
	pollCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	var lastSlot domain.Slot
	var lastErr error
	sawSlot := false
	for {
		timing, err := sampler.SampleBlockTiming(pollCtx, client, metricsURL)
		switch {
		case err != nil:
			if ctx.Err() != nil {
				return source.BlockTiming{}, err
			}
			lastErr = err
		case timing.Slot == slot:
			return timing, nil
		case timing.Slot > slot:
			return source.BlockTiming{}, fmt.Errorf(
				"block timing gauge advanced to slot %d before slot %d was sampled", timing.Slot, slot)
		default:
			lastSlot, sawSlot = timing.Slot, true
		}

		timer := time.NewTimer(blockTimingPollInterval)
		select {
		case <-pollCtx.Done():
			timer.Stop()
			return source.BlockTiming{}, blockTimingNotSampled(slot, lastSlot, sawSlot, lastErr, pollCtx.Err())
		case <-timer.C:
		}
	}
}

// blockTimingNotSampled says which of the two ways the watch can come up empty
// actually happened, because they call for different fixes: a gauge that never
// moved means the fault never produced a block for this slot, while a gauge that
// could not be read means the fault broke the measurement channel instead.
func blockTimingNotSampled(slot, lastSlot domain.Slot, sawSlot bool, lastErr, ctxErr error) error {
	if lastErr != nil && !sawSlot {
		return fmt.Errorf("block timing gauge for slot %d was never readable; last scrape failed with: %w", slot, lastErr)
	}
	if lastErr != nil {
		return fmt.Errorf(
			"block timing gauge remained at slot %d while waiting for slot %d, and the last scrape failed with: %w",
			lastSlot, slot, lastErr)
	}
	return fmt.Errorf("block timing gauge remained at slot %d while waiting for slot %d: %w", lastSlot, slot, ctxErr)
}

func observeHeadTiming(ctx context.Context, sampler *source.MetricsSampler, client *beaconapi.Client, consensusClient source.ConsensusClient, metricsURL string, slot domain.Slot, deadline time.Time, sampleEngine bool, result chan<- headTimingResult) {
	// The arrival gauge is watched from here — main.go starts this observer at
	// slotStart-8s — rather than scraped once the head has arrived.
	//
	// SampleBlockTimingForSlot waits for a latest-value gauge to climb to the
	// slot asked for and fails only when it finds the gauge already past it. Read
	// after the head, that failure is the normal outcome under the very faults
	// this harness injects: with propagation at 6-8s the next slot's block lands
	// before the scrape, the gauge moves on, and the measurement is gone for
	// good. It cost p2p-degraded-prysm-r04 its arrival — verdict
	// unknown.insufficient_data against a local.p2p_degraded label — while r05,
	// the same recipe against the same devnet, kept it. The recipe was never the
	// variable; which side won the race was.
	//
	// Starting the watch before the slot begins removes the race rather than
	// widening a window: the gauge is observed through its transition into this
	// slot instead of sampled after it. A genuinely skipped slot still yields the
	// same "advanced to slot N+1" error, because the gauge still steps over it.
	type timingOutcome struct {
		timing source.BlockTiming
		err    error
	}
	timingCtx, stopTiming := context.WithCancel(ctx)
	defer stopTiming()
	timingDone := make(chan timingOutcome, 1)
	go func() {
		timing, err := watchBlockTimingForSlot(timingCtx, sampler, consensusClient, metricsURL, slot, deadline)
		timingDone <- timingOutcome{timing: timing, err: err}
	}()

	head, err := observeHeadForSlot(ctx, client, slot, deadline)
	if err != nil {
		// An arrival offset without the head that validated it is not a
		// measurement this harness records, so stop the watch and collect it
		// rather than letting the goroutine outlive this call.
		stopTiming()
		<-timingDone
		select {
		case result <- headTimingResult{HeadErr: err}:
		case <-ctx.Done():
		}
		return
	}
	measuredTiming := <-timingDone
	timing, timingErr := measuredTiming.timing, measuredTiming.err
	var engineAfter *source.EngineCounters
	var engineErr error
	if sampleEngine {
		counters, err := sampler.SampleEngineCounters(ctx, consensusClient, metricsURL)
		engineErr = err
		if err == nil {
			engineAfter = &counters
		}
	}
	select {
	case result <- headTimingResult{Head: head, Timing: timing, TimingErr: timingErr, EngineAfter: engineAfter, EngineErr: engineErr}:
	case <-ctx.Done():
	}
}

func prepareHeadTiming(ctx context.Context, beaconAPI, enclave, timingTarget string) (*beaconapi.Client, source.ConsensusClient, string, error) {
	client := beaconapi.NewClient(beaconAPI, 200*time.Millisecond)
	version, err := client.FetchNodeVersion(ctx)
	if err != nil {
		return nil, source.ConsensusUnknown, "", fmt.Errorf("fetch node version: %w", err)
	}
	consensusClient := source.DetectConsensusClient(version)
	if consensusClient == source.ConsensusUnknown {
		return nil, consensusClient, "", fmt.Errorf("unsupported consensus client version %q", version)
	}
	metricsURL, err := resolveCLMetricsURL(ctx, enclave, timingTarget)
	if err != nil {
		return nil, consensusClient, "", err
	}
	return client, consensusClient, metricsURL, nil
}
