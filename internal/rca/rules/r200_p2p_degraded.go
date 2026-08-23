package rules

import (
	"fmt"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// metricCLPeerCount mirrors internal/source/promscrape.MetricCLPeerCount's
// string value. rules cannot import internal/source (outside the
// rca-purity allow-list, correctly — this package must not know where a
// metric came from, I-11), so the normalised name is duplicated here as
// the one detail this rule needs to agree with that package on.
const metricCLPeerCount domain.MetricName = "cl_peer_count"

// P2PDegraded is R-200: propagation to this node was slow because peering
// was insufficient.
//
// A timely network baseline is mandatory: without it, a late block shared by
// the whole network is observationally indistinguishable from slow local
// propagation, regardless of local peer count. R-110 turns that gap into
// unknown.insufficient_data before this rule runs.
//
// docs/causes.md's rule text requires a peer-count signal to corroborate.
// peerCountValue (helpers.go) reads it from either form a Timeline can
// carry: the corpus form (a peer_count_sampled Observation,
// tools/faultinjector's shape) or the live-collection form (a MetricSample,
// internal/source/promscrape's shape). Local-vs-network timing establishes
// that the delay was local; only the peer signal establishes that insufficient
// peering, rather than another local cause, explains it.
//
// Two ways to establish propagation was slow:
//
//  1. Validation is also known (attestation_published was captured): use
//     Stages.Dominant's share comparison, same as every other stage-based
//     rule.
//  2. Validation is unknown (no attestation_published — this build's most
//     common real shape, see Stages.Dominant's doc comment on why a share
//     isn't meaningful with only one stage known): fall back to an
//     absolute bar instead — propagation alone consumed the whole
//     attestation budget. Verified against the real corpus: p2p-degraded-*
//     has block_seen well past the ~4s deadline with no
//     attestation_published captured; cl-slow-cpu has block_seen at 1.4s
//     (comfortably inside the budget) with the same "no published" shape
//     — only the absolute duration tells these apart, since neither has a
//     Validation figure to compare against.
//
// R-200 deliberately precedes R-400. When the measured block itself arrived
// after the deadline, propagation is the more specific explanation for a
// missing publication; R-400 later requires a timely block and head before it
// can blame validator-client connectivity.
type P2PDegraded struct{}

// ID returns R-200.
func (P2PDegraded) ID() string { return "R-200" }

// Evaluate implements rca.Rule.
func (P2PDegraded) Evaluate(tl domain.Timeline, cfg Config) (*domain.Verdict, bool) {
	stages, propagationSlow := propagationOverspent(tl, cfg)
	if !propagationSlow || tl.Network == nil {
		return nil, false
	}
	deadline := tl.AttestationDeadline().Sub(tl.SlotStart)
	if tl.Network.BlockArrivalP50 > deadline {
		return nil, false
	}

	peerCount, peerAt, peerSource, hasPeerCount := peerCountFact(tl)
	if !hasPeerCount {
		return nil, false
	}

	if peerCount >= cfg.PeerCountMin {
		return nil, false
	}
	confidence := domain.ConfidenceHigh
	if tl.Network.SampleCount < 10 {
		confidence = domain.ConfidenceMedium
	}

	return &domain.Verdict{
		Cause:      domain.CauseP2PDegraded,
		Confidence: confidence,
		Evidence: []domain.Evidence{
			{
				At:        tl.SlotStart.Add(stages.Propagation),
				Statement: fmt.Sprintf("local block propagation consumed %s, exceeding the %s attestation budget", stages.Propagation, deadline),
				Source:    domain.SourcePromScrape,
				Comparison: &domain.Comparison{
					Label:    "local block arrival vs attestation deadline",
					Observed: stages.Propagation.Seconds() * 1000,
					Expected: deadline.Seconds() * 1000,
					Unit:     domain.UnitMilliseconds,
				},
			},
			{
				At:        tl.SlotStart,
				Statement: fmt.Sprintf("the independent network p50 was timely at +%s (p90 +%s)", tl.Network.BlockArrivalP50, tl.Network.BlockArrivalP90),
				Source:    tl.Network.Source,
				Comparison: &domain.Comparison{
					Label:    "network p50 vs attestation deadline",
					Observed: tl.Network.BlockArrivalP50.Seconds() * 1000,
					Expected: deadline.Seconds() * 1000,
					Unit:     domain.UnitMilliseconds,
				},
			},
			{
				At:        peerAt,
				Statement: fmt.Sprintf("connected peer count %.0f was below the configured minimum %.0f", peerCount, cfg.PeerCountMin),
				Source:    peerSource,
				Comparison: &domain.Comparison{
					Label:    "connected peers",
					Observed: peerCount,
					Expected: cfg.PeerCountMin,
					Unit:     domain.UnitCount,
				},
			},
		},
		Remediation: []string{
			"confirm inbound TCP/UDP ports are open and forwarded (typically 30303 EL / 9000 CL — verify against your own config)",
			"raise the target peer count in your consensus client configuration",
		},
	}, true
}
