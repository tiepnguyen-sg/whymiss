package rules

import (
	"fmt"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/rca"
)

// VCSlow is R-410: the validator client received a valid head in time but
// published the attestation after the deadline.
//
// docs/causes.md compares T_head (head_updated) against the deadline; with
// head_updated never populated by this codebase's collectors (see
// rca.ComputeStages's doc comment), block_seen is used as the proxy for
// "the block this duty needed was available" — a validator client cannot
// build on a head it hasn't seen, so block_seen strictly precedes the real
// T_head, making this rule's timing check conservative (it can only under-
// count vc_slow cases, never manufacture one where the head genuinely
// wasn't available in time).
type VCSlow struct{}

// ID returns R-410.
func (VCSlow) ID() string { return "R-410" }

// Evaluate implements rca.Rule.
func (VCSlow) Evaluate(tl domain.Timeline, _ rca.Config) (*domain.Verdict, bool) {
	blockSeen, hasBlockSeen := tl.First(domain.ObsBlockSeen)
	published, hasPublished := tl.First(domain.ObsAttestationPublished)
	if !hasBlockSeen || !hasPublished {
		return nil, false
	}

	deadline := tl.AttestationDeadline()
	if !blockSeen.At.Before(deadline) || !published.At.After(deadline) {
		return nil, false
	}

	headOffset := blockSeen.At.Sub(tl.SlotStart)
	publishOffset := published.At.Sub(tl.SlotStart)
	return &domain.Verdict{
		Cause:      domain.CauseVCSlow,
		Confidence: domain.ConfidenceMedium, // high requires a remote signer's own latency corroborating it — not collected by this build.
		Evidence: []domain.Evidence{{
			At:        published.At,
			Statement: fmt.Sprintf("the block was available at +%s, before the attestation deadline (+%s), but the attestation was not published until +%s, after it", headOffset, tl.Schedule.AttestationDeadline, publishOffset),
			Source:    domain.SourceBeaconAPI,
		}},
		Remediation: []string{
			"if using a remote signer (Web3Signer or similar), measure its latency — it is frequently the culprit and rarely monitored",
			"check for CPU contention on the validator client host",
			"confirm validator client and beacon node clocks agree",
		},
	}, true
}
