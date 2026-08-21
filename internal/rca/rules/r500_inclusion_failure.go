package rules

import (
	"fmt"

	"github.com/CHANGEME/whymiss/internal/domain"
	"github.com/CHANGEME/whymiss/internal/rca"
)

// InclusionFailure is R-500: the attestation was published before the
// deadline and never appeared on chain.
type InclusionFailure struct{}

// ID returns R-500.
func (InclusionFailure) ID() string { return "R-500" }

// Evaluate implements rca.Rule.
func (InclusionFailure) Evaluate(tl domain.Timeline, _ rca.Config) (*domain.Verdict, bool) {
	published, hasPublished := tl.First(domain.ObsAttestationPublished)
	if !hasPublished || tl.Has(domain.ObsAttestationIncluded) {
		return nil, false
	}
	if !published.At.Before(tl.AttestationDeadline()) {
		return nil, false
	}

	reorged := tl.Has(domain.ObsReorg)
	confidence := domain.ConfidenceMedium
	if reorged {
		confidence = domain.ConfidenceHigh
	}

	return &domain.Verdict{
		Cause:      domain.CauseInclusionFailure,
		Confidence: confidence,
		Evidence: []domain.Evidence{{
			At:        published.At,
			Statement: fmt.Sprintf("attestation_published exists at +%s, before the attestation deadline, but no attestation_included observation exists within the collection window", published.At.Sub(tl.SlotStart)),
			Source:    domain.SourceBeaconAPI,
		}},
		Remediation: []string{
			"verify inbound P2P ports are reachable, since poor connectivity reduces the chance an aggregator sees your attestation",
			"if recurring, correlate with local.p2p_degraded frequency",
		},
	}, true
}
