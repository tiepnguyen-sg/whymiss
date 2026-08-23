package rules

import (
	"fmt"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// InclusionFailure is R-500: the attestation was published before the
// deadline and never appeared on chain.
type InclusionFailure struct{}

// ID returns R-500.
func (InclusionFailure) ID() string { return "R-500" }

// Evaluate implements rca.Rule.
func (InclusionFailure) Evaluate(tl domain.Timeline, _ Config) (*domain.Verdict, bool) {
	published, hasPublished := tl.First(domain.ObsAttestationPublished)
	if !hasPublished || tl.Has(domain.ObsAttestationIncluded) {
		return nil, false
	}
	if !published.At.Before(tl.AttestationDeadline()) {
		return nil, false
	}
	completed, haveCompletion := tl.First(domain.ObsCollectionCompleted)
	if !haveCompletion {
		return nil, false
	}
	publishedRoot, havePublishedRoot := published.Attr(domain.AttrBlockRoot)
	head, haveHead := tl.First(domain.ObsHeadUpdated)
	headRoot, haveHeadRoot := head.Attr(domain.AttrBlockRoot)
	if !havePublishedRoot || !haveHead || !haveHeadRoot {
		return &domain.Verdict{
			Cause:      domain.CauseInsufficientData,
			Confidence: domain.ConfidenceLow,
			Evidence: []domain.Evidence{{
				At: published.At, Statement: "the attestation was published on time but its voted head could not be compared with a canonical head observation", Source: domain.SourceDerived,
			}},
			Remediation: []string{"inspect collector errors and recollect with canonical head-root evidence before attributing non-inclusion"},
		}, true
	}
	if publishedRoot != headRoot {
		return nil, false
	}

	evidence := []domain.Evidence{
		{
			At: published.At, Statement: fmt.Sprintf("attestation_published exists at +%s, before the attestation deadline", published.At.Sub(tl.SlotStart)), Source: domain.SourceBeaconAPI,
		},
		{
			At: completed.At, Statement: "the complete inclusion window closed with no attestation_included observation", Source: domain.SourceDerived,
		},
		{
			At: head.At, Statement: fmt.Sprintf("the attestation voted for canonical head root %s", publishedRoot), Source: domain.SourceBeaconAPI,
		},
	}
	for _, obs := range tl.Observations {
		if obs.Kind == domain.ObsReorg {
			evidence = append(evidence, reorgContextEvidence(obs))
		}
	}
	for _, reorg := range tl.Reorgs {
		evidence = append(evidence, reorgContextEvidence(reorg))
	}
	return &domain.Verdict{
		Cause:      domain.CauseInclusionFailure,
		Confidence: domain.ConfidenceMedium,
		Evidence:   evidence,
		Remediation: []string{
			"verify inbound P2P ports are reachable, since poor connectivity reduces the chance an aggregator sees your attestation",
			"if recurring, correlate with local.p2p_degraded frequency",
		},
	}, true
}

func reorgContextEvidence(obs domain.Observation) domain.Evidence {
	return domain.Evidence{
		At: obs.At, Statement: fmt.Sprintf("a chain reorganisation was observed at slot %d within the inclusion window; it is context, not proof that this attestation was removed", obs.Slot), Source: obs.Source,
	}
}
