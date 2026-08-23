package app

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/clock"
	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/source"
	"github.com/tiepnguyen-sg/whymiss/internal/store"
)

// runNetworkBaseline records what an independent beacon node saw for the same
// slot, so R-110 and R-200 can separate "the block was late for everyone" from
// "the block was late here". Without it both rules correctly refuse to
// attribute, which leaves half the product's question unanswerable.
//
// One reference node yields a one-sample baseline (p50 == p90, sample_count 1).
// That is deliberately weak evidence and the rules treat it as such — R-200
// caps a one-sample baseline at medium confidence. A real percentile
// distribution needs the public Xatu dataset, which is a separate opt-in
// source; this keeps the same observation shape so that adapter can replace
// the value without touching a rule.
func runNetworkBaseline(ctx context.Context, st *store.Store, sampler *source.MetricsSampler, jobs <-chan domain.Observation, client source.ConsensusClient, metricsURL string, clk *clock.Tracker, clockMaxAge time.Duration, logger *slog.Logger) {
	seen := make(map[domain.Slot]struct{})
	for {
		select {
		case <-ctx.Done():
			return
		case head, ok := <-jobs:
			if !ok {
				return
			}
			if _, duplicate := seen[head.Slot]; duplicate {
				continue
			}
			seen[head.Slot] = struct{}{}
			if len(seen) > 64 {
				oldest := head.Slot
				for slot := range seen {
					if slot < oldest {
						oldest = slot
					}
				}
				delete(seen, oldest)
			}

			timing, err := sampler.SampleBlockTimingForSlot(ctx, client, metricsURL, head.Slot, time.Now().Add(blockTimingCatchupWindow))
			if err != nil {
				logger.Warn("sample network baseline timing", "error", err, "slot", head.Slot)
				continue
			}
			obs, err := networkBaselineFromTiming(head, timing)
			if err != nil {
				logger.Warn("reject network baseline timing", "error", err, "slot", head.Slot)
				continue
			}
			if err := st.WriteObservation(ctx, stampClockProvenance(clk, clockMaxAge, obs, false)); err != nil {
				logger.Error("write network baseline observation", "error", err, "slot", head.Slot)
			}
		}
	}
}

func networkBaselineFromTiming(head domain.Observation, timing source.BlockTiming) (domain.Observation, error) {
	if head.Kind != domain.ObsHeadUpdated {
		return domain.Observation{}, fmt.Errorf("expected head_updated, got %s", head.Kind)
	}
	if timing.Slot != head.Slot {
		return domain.Observation{}, fmt.Errorf("baseline timing is for slot %d, watched head is for slot %d", timing.Slot, head.Slot)
	}
	if timing.Propagation < 0 {
		return domain.Observation{}, fmt.Errorf("baseline propagation is negative: %s", timing.Propagation)
	}
	propagationMS := strconv.FormatFloat(timing.Propagation.Seconds()*1000, 'f', -1, 64)
	return domain.NewObservation(domain.Observation{
		Slot: head.Slot, Kind: domain.ObsNetworkBaselineSampled, At: timing.SampledAt,
		Source: domain.SourcePromScrape,
		Attrs: map[domain.AttrKey]string{
			domain.AttrBlockArrivalP50MS: propagationMS,
			domain.AttrBlockArrivalP90MS: propagationMS,
			domain.AttrSampleCount:       "1",
		},
	})
}
