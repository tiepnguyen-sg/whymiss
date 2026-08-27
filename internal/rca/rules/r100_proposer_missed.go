package rules

import (
	"fmt"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// ProposerMissed is R-100: a fully synced node confirmed the canonical slot
// was skipped.
//
// A confirmed skip explains an attester's lost timely_head and nothing else:
// per ADR-0015, an attester still publishes and is still included normally on
// a skipped slot. So the skip alone cannot account for a duty that produced no
// attestation at all, and reporting the exonerating cause there would tell an
// operator whose validator client is down that there is nothing to fix. R-100
// therefore claims the exoneration only when the operator's own attestation is
// observed on chain, and reports the ambiguity honestly otherwise (ADR-0021).
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

	skipEvidence := domain.Evidence{
		At:        skipped.At,
		Statement: "a fully synced beacon node confirmed that the canonical chain skipped this slot",
		Source:    domain.SourceBeaconAPI,
	}

	// The attestation reached the chain, so the operator's own path — validator
	// client, beacon node, and publication — demonstrably worked. Whatever the
	// duty lost, the skip is what it lost it to, and the exoneration is a real
	// finding rather than an absence read as innocence.
	if included, includedOK := tl.Last(domain.ObsAttestationIncluded); includedOK {
		return &domain.Verdict{
			Cause:      domain.CauseProposerMissed,
			Confidence: domain.ConfidenceHigh,
			Evidence: []domain.Evidence{
				skipEvidence,
				{
					At:        included.At,
					Statement: "the attester's own attestation for this duty was included on chain, so the local attestation path was working while the slot was skipped",
					Source:    domain.SourceBeaconAPI,
				},
			},
		}, true
	}

	// Published but never included is R-500's question — non-inclusion of an
	// on-time attestation — not a proposer miss. Declining here keeps that
	// distinction with the rule that owns it.
	if tl.Has(domain.ObsAttestationPublished) {
		return nil, false
	}

	// The chain skipped the slot and nothing was ever seen from the operator's
	// own attestation path. Two readings fit equally: only the upstream
	// proposer failed, or it failed while the local path failed too. Nothing in
	// this timeline separates them, and R-400 cannot help — it needs block_seen
	// and head_updated before the deadline to prove the beacon node was
	// healthy, and a skipped slot has neither. I-8 takes the honest unknown
	// over the confident guess.
	return &domain.Verdict{
		Cause:      domain.CauseInsufficientData,
		Confidence: domain.ConfidenceLow,
		Evidence: []domain.Evidence{
			skipEvidence,
			{
				// Derived reasoning, not an event: timestamped at the slot's own
				// start the way R-010 does, so a reader does not take it for
				// something that was established at the instant of the skip.
				At:        tl.SlotStart,
				Statement: "no attestation_published and no attestation_included observation exists for this duty, so the skip cannot be shown to be the whole reason the duty was lost — an attester publishes and is included normally on a skipped slot",
				Source:    domain.SourceDerived,
			},
			{
				At: tl.SlotStart,
				Statement: fmt.Sprintf(
					"attributing this slot needs evidence the local path was alive at the %s deadline, which a skipped slot cannot supply: with no block there is no block_seen or head_updated to measure the beacon node against",
					tl.AttestationDeadline().Sub(tl.SlotStart)),
				Source: domain.SourceDerived,
			},
		},
		Remediation: []string{
			"check whether the validator client was running and connected at this slot; a canonical skip does not explain a missing attestation",
			// Deliberately only two lines. A third once suggested keeping
			// --cl-metrics-api set "so peer count and block timing are recorded
			// even on slots the chain skips", and both halves were false: peer
			// count comes from the Beacon API and is sampled on a timer
			// regardless of that flag (ADR-0023), while the arrival gauge needs a
			// block, so a skipped slot yields no timing from it either. There is
			// no configuration that makes this case diagnosable — the missing
			// fact is whether the validator client was alive, which the two lines
			// above already ask for — and a remediation naming a setting that
			// would not have helped is worse than one line fewer.
			"confirm the validator client logs show an attestation attempt for this slot, which is the one fact this timeline cannot supply",
		},
	}, true
}
