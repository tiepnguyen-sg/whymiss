package main

import (
	"context"
	"fmt"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/source"
)

// collectEngineBaseline samples the timing node's Engine API counters once per
// slot until it holds source.EngineBaselineMinSamples per-slot totals, then
// returns the p99 as a MetricSample for the corpus record to carry.
//
// R-300 (local.el_slow) compares a slot's Engine cost against this baseline, and
// it reads the baseline as a domain.MetricSample. Only the watch daemon used to
// write one, so no corpus record could ever satisfy R-300 — the cause was
// unreproducible at any fault severity and under any load, which several cycles
// misread as the devnet being too quiet.
//
// It has to run before the duty is chosen, not after. The Beacon API only
// guarantees duties one epoch ahead, and a chosen duty can be as little as
// minDutyLead away, so there is no reliable window between "duty known" and
// "duty slot" long enough to gather 32 slots of history. Sampling first and
// choosing the duty afterwards costs an epoch of wall clock and is the only
// ordering that always has the samples in hand.
// engineCounterSampler is the one method this collector needs from
// source.MetricsSampler. Declared here, by the consumer, so the bounded-retry
// behaviour below can be tested against a sampler that never succeeds
// (BUILD_PROMPT §5).
type engineCounterSampler interface {
	SampleEngineCounters(ctx context.Context, client source.ConsensusClient, metricsURL string) (source.EngineCounters, error)
}

func collectEngineBaseline(ctx context.Context, sampler engineCounterSampler, client source.ConsensusClient, metricsURL string, obs *Observer) (*domain.MetricSample, error) {
	var baseline source.EngineBaseline
	var previous *source.EngineCounters

	fmt.Printf("faultinjector: sampling Engine baseline for %d slots (~%s) before choosing a duty\n",
		source.EngineBaselineMinSamples, time.Duration(source.EngineBaselineMinSamples)*obs.SecondsPerSlot)

	// Bounded, because every other polling loop in this project is. A node whose
	// Engine counters are absent, reset every slot, or served by a client this
	// build has no adapter for would otherwise spin one slot at a time until the
	// context died, and a corpus run would hang rather than fail with something
	// an operator could act on. Three times the ideal slot count leaves room for
	// transient scrape failures without letting a broken endpoint run forever.
	maxAttempts := source.EngineBaselineMinSamples * 3
	failures := 0
	for attempt := 0; baseline.Len() < source.EngineBaselineMinSamples; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if attempt >= maxAttempts {
			return nil, fmt.Errorf(
				"engine baseline collected %d of %d per-slot samples in %d slots (%d scrapes unusable) from %s; the node is not reporting usable Engine counters",
				baseline.Len(), source.EngineBaselineMinSamples, attempt, failures, metricsURL)
		}
		sampleCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		counters, err := sampler.SampleEngineCounters(sampleCtx, client, metricsURL)
		cancel()
		if err != nil {
			// One failed scrape must not discard the window: the node is under
			// no fault yet, so a transient timeout is noise, not signal.
			fmt.Printf("faultinjector: Engine baseline sample unavailable: %v\n", err)
			failures++
			previous = nil
			waitUntil(ctx, time.Now().Add(obs.SecondsPerSlot))
			continue
		}
		if previous != nil {
			calls, err := source.EngineCallsBetween(*previous, counters)
			if err != nil {
				// An ambiguous window (a counter reset, or a method missing)
				// cannot be attributed to one slot; drop it and restart the pair.
				failures++
				previous = nil
				waitUntil(ctx, time.Now().Add(obs.SecondsPerSlot))
				continue
			}
			var totalMS float64
			for _, call := range calls {
				totalMS += call.DurationMS
			}
			baseline.Add(totalMS)
		}
		snapshot := counters
		previous = &snapshot
		waitUntil(ctx, time.Now().Add(obs.SecondsPerSlot))
	}

	p99, ok := baseline.P99()
	if !ok {
		return nil, fmt.Errorf("engine baseline holds %d samples, want %d", baseline.Len(), source.EngineBaselineMinSamples)
	}
	sample := domain.MetricSample{
		At:        time.Now().UTC(),
		Component: domain.ComponentEL,
		Name:      source.MetricELEngineCallsP99MS,
		Value:     p99,
		Source:    domain.SourceDerived,
	}
	if err := sample.Validate(); err != nil {
		return nil, fmt.Errorf("build engine baseline sample: %w", err)
	}
	fmt.Printf("faultinjector: Engine baseline p99=%.2fms over %d slots\n", p99, baseline.Len())
	return &sample, nil
}
