package source

import (
	"context"
	"fmt"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/source/promscrape"
)

// BlockTiming is a client-normalized local block-arrival measurement.
type BlockTiming = promscrape.BlockTiming

// EngineCounters is a client-normalized cumulative Engine metric snapshot.
type EngineCounters = promscrape.EngineCounters

// EngineCallWindow is one method's exact aggregate between head snapshots.
type EngineCallWindow = promscrape.EngineCallWindow

// MetricELEngineCallsP99MS is the rolling per-slot Engine total baseline.
const MetricELEngineCallsP99MS = promscrape.MetricELEngineCallsP99MS

// MetricsSampler owns one bounded Prometheus scraper for a collector process.
// It is constructed explicitly by internal/app (or the devnet-only injector)
// and safely reuses connections without package-global mutable state.
type MetricsSampler struct {
	scraper *promscrape.Scraper
}

// NewMetricsSampler constructs a client-normalizing metrics sampler.
func NewMetricsSampler() *MetricsSampler {
	return &MetricsSampler{scraper: promscrape.New()}
}

// SampleBlockTiming reads the latest block's client-normalized arrival offset.
func (s *MetricsSampler) SampleBlockTiming(ctx context.Context, client ConsensusClient, metricsURL string) (BlockTiming, error) {
	switch client {
	case ConsensusLighthouse:
		return s.scraper.SampleLighthouseBlockTiming(ctx, metricsURL)
	case ConsensusPrysm:
		return s.scraper.SamplePrysmBlockTiming(ctx, metricsURL)
	default:
		return BlockTiming{}, fmt.Errorf("block timing sampling not implemented for consensus client %q", client)
	}
}

const blockTimingPollInterval = 250 * time.Millisecond

// SampleBlockTimingForSlot waits for a latest-value timing gauge to catch up to
// expectedSlot, without ever assigning a stale or future slot's value to it.
// deadline bounds both each HTTP request and the complete polling window.
func (s *MetricsSampler) SampleBlockTimingForSlot(ctx context.Context, client ConsensusClient, metricsURL string, expectedSlot domain.Slot, deadline time.Time) (BlockTiming, error) {
	pollCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	var lastSlot domain.Slot
	for {
		timing, err := s.SampleBlockTiming(pollCtx, client, metricsURL)
		if err != nil {
			return BlockTiming{}, err
		}
		lastSlot = timing.Slot
		switch {
		case timing.Slot == expectedSlot:
			return timing, nil
		case timing.Slot > expectedSlot:
			return BlockTiming{}, fmt.Errorf("block timing gauge advanced to slot %d before slot %d was sampled", timing.Slot, expectedSlot)
		}

		timer := time.NewTimer(blockTimingPollInterval)
		select {
		case <-pollCtx.Done():
			timer.Stop()
			return BlockTiming{}, fmt.Errorf("block timing gauge remained at slot %d while waiting for slot %d: %w", lastSlot, expectedSlot, pollCtx.Err())
		case <-timer.C:
		}
	}
}

// SampleEngineCounters reads client-normalized cumulative Engine metrics.
func (s *MetricsSampler) SampleEngineCounters(ctx context.Context, client ConsensusClient, metricsURL string) (EngineCounters, error) {
	switch client {
	case ConsensusLighthouse:
		return s.scraper.SampleLighthouseEngineCounters(ctx, metricsURL)
	case ConsensusPrysm:
		return s.scraper.SamplePrysmEngineCounters(ctx, metricsURL)
	default:
		return EngineCounters{}, fmt.Errorf("engine counter sampling not implemented for consensus client %q", client)
	}
}

// EngineCallsBetween isolates each method's calls between head snapshots.
func EngineCallsBetween(before, after EngineCounters) ([]EngineCallWindow, error) {
	return promscrape.EngineCallsBetween(before, after)
}
