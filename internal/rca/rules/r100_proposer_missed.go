package rules

import "github.com/tiepnguyen-sg/whymiss/internal/domain"

// ProposerMissed is R-100: a fully synced node confirmed the canonical slot
// was skipped. Attesters may still publish for the preceding head normally.
type ProposerMissed struct{}

// ID returns R-100.
func (ProposerMissed) ID() string { return "R-100" }

// Evaluate implements rca.Rule.
func (ProposerMissed) Evaluate(tl domain.Timeline, _ Config) (*domain.Verdict, bool) {
	// For the operator's own proposer duty, absence is not an exonerating
	// network event: it may be a local proposal failure, which this taxonomy
	// cannot yet distinguish. R-100 only explains an attester whose upstream
	// proposer failed.
	if tl.Duty == nil || tl.Duty.Kind != domain.DutyAttester {
		return nil, false
	}
	skipped, ok := tl.First(domain.ObsBlockSkipped)
	if !ok || tl.Has(domain.ObsBlockSeen) || tl.Has(domain.ObsHeadUpdated) || tl.Has(domain.ObsBlockProposed) {
		return nil, false
	}

	return &domain.Verdict{
		Cause:      domain.CauseProposerMissed,
		Confidence: domain.ConfidenceHigh,
		Evidence: []domain.Evidence{{
			At:        skipped.At,
			Statement: "a fully synced beacon node confirmed that the canonical chain skipped this slot",
			Source:    domain.SourceBeaconAPI,
		}},
	}, true
}
