package rules

import (
	"fmt"

	"github.com/CHANGEME/whymiss/internal/domain"
	"github.com/CHANGEME/whymiss/internal/rca"
)

// NetworkLateBlock is R-110, "the single most valuable rule in the
// taxonomy" per docs/causes.md — but it only ever fires once a network
// baseline exists (Phase 5; domain.Timeline.Network is always nil until
// then, which every scenario in this build's corpus confirms today).
//
// When no baseline is available, this rule does not match at all — it
// falls through to R-200, which is what actually decides between
// local.p2p_degraded and unknown.insufficient_data for a propagation-dominant
// slot with no baseline. An earlier version of this rule matched on
// propagation-dominance alone and reported unknown.insufficient_data
// whenever no baseline existed; that reading of docs/causes.md's fallback
// text ("falls through to unknown.insufficient_data") turned out to mean R-110
// itself falling through past R-200 to R-999's eventual unknown — not R-110
// matching and reporting unknown directly — confirmed against this
// project's corpus: with the direct-match reading, local.p2p_degraded's
// four real scenarios (p2p-degraded-*, peer-isolated-*) were unreachable,
// since R-110 always ran first and always "matched" whenever propagation
// was dominant. See R-200's doc comment for the confidence handling that
// replaces it.
//
// When a baseline does exist: matches with network.late_block if local and
// network agree (within cfg.NetworkDeviation); otherwise doesn't match,
// falling to R-200, which independently re-checks propagation dominance.
type NetworkLateBlock struct{}

// ID returns R-110.
func (NetworkLateBlock) ID() string { return "R-110" }

// Evaluate implements rca.Rule.
func (NetworkLateBlock) Evaluate(tl domain.Timeline, cfg rca.Config) (*domain.Verdict, bool) {
	if tl.Network == nil {
		return nil, false
	}

	stages := rca.ComputeStages(tl)
	dominant, ok := stages.Dominant(cfg)
	if !ok || dominant != domain.StagePropagation {
		return nil, false
	}

	local := stages.Propagation
	networkP50 := tl.Network.BlockArrivalP50
	deviation := local - networkP50
	if deviation < 0 {
		deviation = -deviation
	}
	if deviation >= cfg.NetworkDeviation {
		return nil, false
	}

	confidence := domain.ConfidenceHigh
	if tl.Network.SampleCount < 10 {
		confidence = domain.ConfidenceMedium
	}
	return &domain.Verdict{
		Cause:      domain.CauseLateBlock,
		Confidence: confidence,
		Evidence: []domain.Evidence{{
			At:        tl.SlotStart,
			Statement: fmt.Sprintf("local block-arrival offset (%s) agrees with the network-wide p50 (%s) within %s — the block was late for everyone", local, networkP50, cfg.NetworkDeviation),
			Source:    domain.SourceXatu,
			Comparison: &domain.Comparison{
				Label:    "block arrival vs. network p50",
				Observed: local.Seconds() * 1000,
				Expected: networkP50.Seconds() * 1000,
				Unit:     domain.UnitMilliseconds,
			},
		}},
	}, true
}
