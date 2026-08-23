package rules

import "github.com/tiepnguyen-sg/whymiss/internal/domain"

// VCDisconnected infers VC failure only when the beacon node had a timely head.
type VCDisconnected struct{}

// ID returns R-400.
func (VCDisconnected) ID() string { return "R-400" }

// Evaluate implements rca.Rule.
func (VCDisconnected) Evaluate(tl domain.Timeline, _ Config) (*domain.Verdict, bool) {
	if tl.Has(domain.ObsAttestationPublished) || tl.Has(domain.ObsAttestationIncluded) {
		return nil, false
	}
	blockSeen, ok := timedBlockSeen(tl)
	if !ok || !blockSeen.At.Before(tl.AttestationDeadline()) {
		return nil, false
	}
	head, ok := tl.First(domain.ObsHeadUpdated)
	if !ok || !head.At.Before(tl.AttestationDeadline()) {
		return nil, false
	}
	completed, ok := tl.First(domain.ObsCollectionCompleted)
	if !ok {
		return nil, false
	}

	return &domain.Verdict{
		Cause:      domain.CauseVCDisconnected,
		Confidence: domain.ConfidenceMedium,
		Evidence: []domain.Evidence{{
			At:        completed.At,
			Statement: "the beacon node updated head before the deadline, but no attestation_published or attestation_included observation exists — the validator client never produced an attestation",
			Source:    domain.SourceDerived,
		}},
		Remediation: []string{
			"check the validator client process is running and its beacon-node endpoint is correct",
			"if using multiple beacon nodes, verify fallback ordering behaved as intended",
		},
	}, true
}
