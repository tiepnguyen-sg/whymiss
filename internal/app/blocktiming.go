package app

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/clock"
	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/source"
	"github.com/tiepnguyen-sg/whymiss/internal/source/beaconapi"
	"github.com/tiepnguyen-sg/whymiss/internal/store"
)

const blockTimingCatchupWindow = 3 * time.Second

func runBlockTiming(ctx context.Context, st *store.Store, sampler *source.MetricsSampler, jobs <-chan domain.Observation, client source.ConsensusClient, metricsURL string, genesis beaconapi.GenesisInfo, clk *clock.Tracker, clockMaxAge time.Duration, logger *slog.Logger) {
	processed := make(map[domain.Slot]struct{})
	var windows engineWindowTracker
	var baseline engineBaseline
	for {
		select {
		case <-ctx.Done():
			return
		case head, ok := <-jobs:
			if !ok {
				return
			}
			if _, duplicate := processed[head.Slot]; duplicate {
				continue
			}
			processed[head.Slot] = struct{}{}
			if len(processed) > 64 {
				oldest := head.Slot
				for slot := range processed {
					if slot < oldest {
						oldest = slot
					}
				}
				delete(processed, oldest)
			}
			trustedHead := stampClock(clk, clockMaxAge, head)
			timing, err := sampler.SampleBlockTimingForSlot(ctx, client, metricsURL, head.Slot, time.Now().Add(blockTimingCatchupWindow))
			if err != nil {
				logger.Warn("sample block timing", "error", err, "slot", head.Slot)
			} else if obs, buildErr := blockSeenFromTiming(trustedHead, timing, genesis); buildErr != nil {
				logger.Warn("reject block timing", "error", buildErr, "slot", head.Slot)
			} else if writeErr := st.WriteObservation(ctx, stampClockProvenance(clk, clockMaxAge, obs, false)); writeErr != nil {
				logger.Error("write measured block_seen", "error", writeErr, "slot", head.Slot)
			}

			after, err := sampler.SampleEngineCounters(ctx, client, metricsURL)
			if err != nil {
				logger.Warn("sample Engine counters", "error", err, "slot", head.Slot)
				windows = engineWindowTracker{}
				continue
			}
			calls, complete, err := windows.advance(head.Slot, after)
			if err != nil {
				logger.Debug("skip ambiguous Engine counter window", "error", err, "slot", head.Slot)
				continue
			}
			if !complete {
				logger.Debug("warm or reset Engine counter window", "slot", head.Slot)
				continue
			}
			if p99, ok := baseline.p99(); ok {
				sample := domain.MetricSample{
					At: genesis.SlotStart(uint64(head.Slot)), Component: domain.ComponentEL,
					Name: source.MetricELEngineCallsP99MS, Value: p99, Source: domain.SourceDerived,
				}
				if err := st.WriteSample(ctx, sample); err != nil {
					logger.Error("write Engine p99 baseline", "error", err, "slot", head.Slot)
				}
			}
			var totalMS float64
			for _, call := range calls {
				totalMS += call.DurationMS
				obs, err := domain.NewObservation(domain.Observation{
					Slot: head.Slot, Kind: domain.ObsEngineCall, At: call.At,
					Source: domain.SourcePromScrape,
					Attrs: map[domain.AttrKey]string{
						domain.AttrEngineMethod: call.Method,
						domain.AttrDurationMS:   strconv.FormatFloat(call.DurationMS, 'f', -1, 64),
						domain.AttrSampleCount:  strconv.FormatUint(call.Count, 10),
					},
				})
				if err != nil {
					logger.Error("build Engine call observation", "error", err, "slot", head.Slot)
					continue
				}
				if err := st.WriteObservation(ctx, stampClock(clk, clockMaxAge, obs)); err != nil {
					logger.Error("write Engine call observation", "error", err, "slot", head.Slot)
				}
			}
			baseline.add(totalMS)
		}
	}
}

// engineWindowTracker accepts only counter snapshots taken after consecutive
// canonical heads. Startup and slot gaps are warm-up points, not evidence: a
// wider counter delta could include work from several blocks and must never be
// attributed to one duty slot.
type engineWindowTracker struct {
	ready    bool
	slot     domain.Slot
	counters source.EngineCounters
}

func (t *engineWindowTracker) advance(slot domain.Slot, after source.EngineCounters) ([]source.EngineCallWindow, bool, error) {
	if !t.ready {
		t.ready, t.slot, t.counters = true, slot, after
		return nil, false, nil
	}
	previousSlot, before := t.slot, t.counters
	t.slot, t.counters = slot, after
	if slot <= previousSlot || slot-previousSlot != 1 {
		return nil, false, nil
	}
	calls, err := source.EngineCallsBetween(before, after)
	if err != nil {
		return nil, false, err
	}
	return calls, true, nil
}

const (
	engineBaselineMinSamples = 32
	engineBaselineMaxSamples = 256
)

type engineBaseline struct{ totals []float64 }

func (b *engineBaseline) add(totalMS float64) {
	if totalMS < 0 || math.IsNaN(totalMS) || math.IsInf(totalMS, 0) {
		return
	}
	if len(b.totals) == engineBaselineMaxSamples {
		copy(b.totals, b.totals[1:])
		b.totals[len(b.totals)-1] = totalMS
		return
	}
	b.totals = append(b.totals, totalMS)
}

func (b *engineBaseline) p99() (float64, bool) {
	if len(b.totals) < engineBaselineMinSamples {
		return 0, false
	}
	values := append([]float64(nil), b.totals...)
	sort.Float64s(values)
	index := int(math.Ceil(0.99*float64(len(values)))) - 1
	return values[index], true
}

func blockSeenFromTiming(head domain.Observation, timing source.BlockTiming, genesis beaconapi.GenesisInfo) (domain.Observation, error) {
	if head.Kind != domain.ObsHeadUpdated {
		return domain.Observation{}, fmt.Errorf("expected head_updated, got %s", head.Kind)
	}
	if timing.Slot != head.Slot {
		return domain.Observation{}, fmt.Errorf("block timing is for slot %d, head update is for slot %d", timing.Slot, head.Slot)
	}
	blockAt := genesis.SlotStart(uint64(head.Slot)).Add(timing.Propagation)
	if blockAt.After(head.At) {
		return domain.Observation{}, fmt.Errorf("block arrival %s is after head update %s", blockAt, head.At)
	}
	attrs := map[domain.AttrKey]string{}
	if root, ok := head.Attr(domain.AttrBlockRoot); ok {
		attrs[domain.AttrBlockRoot] = root
	}
	return domain.NewObservation(domain.Observation{
		Slot: head.Slot, Kind: domain.ObsBlockSeen, At: blockAt,
		Source: domain.SourcePromScrape, Attrs: attrs,
	})
}
