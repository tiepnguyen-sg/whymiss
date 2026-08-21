package rules

import (
	"fmt"

	"github.com/CHANGEME/whymiss/internal/domain"
	"github.com/CHANGEME/whymiss/internal/rca"
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
// docs/causes.md's rule text requires a peer-count signal to corroborate.
// peerCountValue (helpers.go) reads it from either form a Timeline can
// carry: the corpus form (a peer_count_sampled Observation,
// tools/faultinjector's shape) or the live-collection form (a MetricSample,
// internal/source/promscrape's shape). Rather than let every
// propagation-dominant slot fall to unknown for want of a metric that
// wasn't sampled, this rule still matches on dominance alone when no
// peer-count sample exists, at ConfidenceMedium (dominance without
// corroboration — exactly docs/causes.md §4's D && !C && K formula) rather
// than refusing to attribute at all: by the time execution reaches here,
// R-110 has already ruled out "network-wide" (no baseline, or deviation
// too large), so a still-dominant propagation stage has no other
// candidate explanation among the rules ordered before this one.
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
// Reaching this rule at all with neither attestation_published nor
// attestation_included means R-400 already looked at the same timeline
// and declined: R-400 only defers here when block_seen exists and
// arrived at or after the deadline, i.e. exactly the shape that makes
// propagation, not VC connectivity, the better explanation (see R-400's
// own doc comment for the real corpus scenario — a validator client
// giving up on a duty entirely once the head was too stale to be worth
// attesting to — that made this distinction necessary). This rule used
// to short-circuit that shape straight to R-400 itself; that duplicated,
// and pre-empted, R-400's own (more specific) reasoning, and for a while
// did so incorrectly — see git history around the R-400 fix.
type P2PDegraded struct{}

// ID returns R-200.
func (P2PDegraded) ID() string { return "R-200" }

// Evaluate implements rca.Rule.
func (P2PDegraded) Evaluate(tl domain.Timeline, cfg rca.Config) (*domain.Verdict, bool) {
	stages := rca.ComputeStages(tl)
	var propagationSlow bool
	switch {
	case stages.HasValidation:
		dominant, ok := stages.Dominant(cfg)
		propagationSlow = ok && dominant == domain.StagePropagation
	case stages.HasPropagation:
		budget := tl.AttestationDeadline().Sub(tl.SlotStart)
		propagationSlow = stages.Propagation > budget
	}
	if !propagationSlow {
		return nil, false
	}

	peerCount, hasPeerCount := peerCountValue(tl)

	if !hasPeerCount {
		return &domain.Verdict{
			Cause:      domain.CauseP2PDegraded,
			Confidence: domain.ConfidenceMedium,
			Evidence: []domain.Evidence{{
				At:        tl.SlotStart,
				Statement: fmt.Sprintf("propagation dominated this duty's overspend (%s of the slot's stage total), with no other candidate explanation and no peer-count sample to independently corroborate it", stages.Propagation),
				Source:    domain.SourceDerived,
			}},
			Remediation: []string{
				"confirm inbound TCP/UDP ports are open and forwarded (typically 30303 EL / 9000 CL — verify against your own config)",
				"raise the target peer count in your consensus client configuration",
			},
		}, true
	}

	if peerCount >= cfg.PeerCountMin {
		return nil, false
	}

	return &domain.Verdict{
		Cause:      domain.CauseP2PDegraded,
		Confidence: domain.ConfidenceHigh,
		Evidence: []domain.Evidence{{
			At:        tl.SlotStart,
			Statement: fmt.Sprintf("propagation dominated this duty's overspend (%s), corroborated by a peer count of %.0f, below the %.0f minimum", stages.Propagation, peerCount, cfg.PeerCountMin),
			Source:    domain.SourcePromScrape,
			Comparison: &domain.Comparison{
				Label:    "connected peers",
				Observed: peerCount,
				Expected: cfg.PeerCountMin,
				Unit:     domain.UnitCount,
			},
		}},
		Remediation: []string{
			"confirm inbound TCP/UDP ports are open and forwarded (typically 30303 EL / 9000 CL — verify against your own config)",
			"raise the target peer count in your consensus client configuration",
		},
	}, true
}
