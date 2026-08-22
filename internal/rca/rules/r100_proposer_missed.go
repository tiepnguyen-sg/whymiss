package rules

import (
	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/rca"
)

// ProposerMissed is R-100: no block_seen observation exists for this slot.
// Exonerates the operator immediately — nobody proposed, so nothing this
// operator's stack did or didn't do mattered.
//
// Also requires attestation_published absent: a validator client that
// still managed to publish an attestation had *some* head to attest to
// (normal behaviour when its own slot's block hasn't arrived yet — it
// attests the latest known head instead, per beaconapi.PollAttestationPublished's
// own doc comment), which means the chain is not actually empty — this
// slot's own block just wasn't seen locally in time, a local/host problem
// (see r600_host_fallback.go), not "nobody proposed." Confirmed against a
// real scenario (host-memory-pressure): the block for that slot existed
// and was canonical, confirmed by direct query minutes after the fault
// cleared — this node's own collector just never saw it within the window.
type ProposerMissed struct{}

// ID returns R-100.
func (ProposerMissed) ID() string { return "R-100" }

// Evaluate implements rca.Rule.
func (ProposerMissed) Evaluate(tl domain.Timeline, _ rca.Config) (*domain.Verdict, bool) {
	if tl.Has(domain.ObsBlockSeen) || tl.Has(domain.ObsAttestationPublished) {
		return nil, false
	}

	return &domain.Verdict{
		Cause:      domain.CauseProposerMissed,
		Confidence: domain.ConfidenceHigh,
		Evidence: []domain.Evidence{{
			At:        tl.SlotStart,
			Statement: "no block was observed for this slot within the collection window — nobody proposed",
			Source:    domain.SourceDerived,
		}},
	}, true
}
