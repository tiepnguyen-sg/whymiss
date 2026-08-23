package promscrape

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// BlockTiming is one slot-qualified local block-propagation offset. Slot and
// Propagation come from the same Prometheus response so callers cannot assign a
// stale latest-value gauge to a newer head.
type BlockTiming struct {
	Slot        domain.Slot
	Propagation time.Duration
	SampledAt   time.Time
}

// SampleLighthouseBlockTiming reads Lighthouse's latest observed-slot gauge.
func (s *Scraper) SampleLighthouseBlockTiming(ctx context.Context, metricsURL string) (BlockTiming, error) {
	return s.sampleBlockTiming(ctx, metricsURL, "beacon_block_delay_observed_slot_start ")
}

// SamplePrysmBlockTiming reads Prysm's latest block-arrival gauge.
func (s *Scraper) SamplePrysmBlockTiming(ctx context.Context, metricsURL string) (BlockTiming, error) {
	return s.sampleBlockTiming(ctx, metricsURL, "block_arrival_latency_milliseconds_gauge ")
}

func (s *Scraper) sampleBlockTiming(ctx context.Context, metricsURL, prefix string) (BlockTiming, error) {
	lines, err := s.fetchMetricsLines(ctx, metricsURL)
	if err != nil {
		return BlockTiming{}, err
	}
	var timing BlockTiming
	var foundPropagation, foundSlot bool
	for _, line := range lines {
		if value, ok := strings.CutPrefix(line, prefix); ok {
			milliseconds, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil {
				return BlockTiming{}, fmt.Errorf("parse block timing from line %q: %w", line, err)
			}
			if milliseconds < 0 || math.IsNaN(milliseconds) || math.IsInf(milliseconds, 0) || milliseconds > float64(math.MaxInt64)/float64(time.Millisecond) {
				return BlockTiming{}, fmt.Errorf("block timing is not a finite non-negative duration: %q", strings.TrimSpace(value))
			}
			timing.Propagation = time.Duration(milliseconds * float64(time.Millisecond))
			foundPropagation = true
		}
		if value, ok := strings.CutPrefix(line, "beacon_head_slot "); ok {
			slot, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
			if err != nil {
				return BlockTiming{}, fmt.Errorf("parse beacon head slot from line %q: %w", line, err)
			}
			timing.Slot = domain.Slot(slot)
			foundSlot = true
		}
	}
	if !foundPropagation {
		return BlockTiming{}, fmt.Errorf("block timing metric %q not found in metrics from %s", strings.TrimSpace(prefix), metricsURL)
	}
	if !foundSlot {
		return BlockTiming{}, fmt.Errorf("beacon_head_slot not found in metrics from %s", metricsURL)
	}
	timing.SampledAt = time.Now().UTC()
	return timing, nil
}
