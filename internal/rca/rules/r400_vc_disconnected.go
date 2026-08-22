package rules

import (
	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/rca"
)

// VCDisconnected is R-400: the validator client could not reach the beacon
// node, so no attestation was produced.
//
// Requires both attestation_published and attestation_included absent —
// not published absent alone, which cl-slow-cpu's real corpus data shows
// can happen even when the attestation genuinely was published and
// included (a documented polling gap: the pool endpoint didn't reflect it
// before the collection window closed, but inclusion on chain is
// independent proof publishing did happen).
//
// Also requires block_seen, if it exists at all, to have arrived before
// the attestation deadline. A validator client that had a head ready in
// time and still never attested is genuine, direct evidence of
// disconnection. One that never attested after the block arrived well
// past the deadline has a more specific explanation already available —
// propagation — so this rule defers to R-200 in that case instead. A
// real corpus scenario surfaced this the hard way: a severely
// propagation-degraded slot (block_seen minutes past the deadline) whose
// validator client gave up on the duty entirely, rather than publishing
// late, produced exactly the "no published, no included" shape this rule
// used to treat as unconditional — which would have reported
// local.vc_disconnected at ConfidenceHigh for a propagation problem, the
// exact false-confident-verdict failure mode I-8 exists to prevent.
type VCDisconnected struct{}

// ID returns R-400.
func (VCDisconnected) ID() string { return "R-400" }

// Evaluate implements rca.Rule.
func (VCDisconnected) Evaluate(tl domain.Timeline, _ rca.Config) (*domain.Verdict, bool) {
	if tl.Has(domain.ObsAttestationPublished) || tl.Has(domain.ObsAttestationIncluded) {
		return nil, false
	}
	if blockSeen, ok := tl.First(domain.ObsBlockSeen); ok && !blockSeen.At.Before(tl.AttestationDeadline()) {
		return nil, false
	}

	return &domain.Verdict{
		Cause:      domain.CauseVCDisconnected,
		Confidence: domain.ConfidenceHigh,
		Evidence: []domain.Evidence{{
			At:        tl.SlotStart,
			Statement: "no attestation_published or attestation_included observation exists for this slot despite a block being seen — the validator client never produced an attestation",
			Source:    domain.SourceDerived,
		}},
		Remediation: []string{
			"check the validator client process is running and its beacon-node endpoint is correct",
			"if using multiple beacon nodes, verify fallback ordering behaved as intended",
		},
	}, true
}
