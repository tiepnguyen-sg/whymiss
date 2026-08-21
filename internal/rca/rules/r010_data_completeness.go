// Package rules holds one file per rule in docs/causes.md §6's ordering,
// and order.go, which declares that ordering explicitly (ADR-0003).
package rules

import (
	"github.com/CHANGEME/whymiss/internal/domain"
	"github.com/CHANGEME/whymiss/internal/rca"
)

// DataCompleteness is R-010: the timeline carries attestation_included with
// no block_seen behind it — internally inconsistent for this build's
// collection paths, since inclusion on chain can only be confirmed for a
// block this node's collector also observed.
//
// Deliberately does *not* also fire on attestation_published without
// block_seen: that combination is normal (a validator client attests to
// whatever head it currently has when its own slot's block hasn't arrived
// yet — see r100_proposer_missed.go's doc comment, confirmed against a
// real scenario, host-memory-pressure, where this happened and the correct
// attribution was local.host.memory_pressure, not "insufficient data"). An
// earlier version of this rule fired on that combination too and pre-empted
// every rule after it for that scenario — caught by the golden test.
type DataCompleteness struct{}

// ID returns R-010.
func (DataCompleteness) ID() string { return "R-010" }

// Evaluate implements rca.Rule.
func (DataCompleteness) Evaluate(tl domain.Timeline, _ rca.Config) (*domain.Verdict, bool) {
	if tl.Has(domain.ObsBlockSeen) || !tl.Has(domain.ObsAttestationIncluded) {
		return nil, false
	}

	return &domain.Verdict{
		Cause:      domain.CauseInsufficientData,
		Confidence: domain.ConfidenceLow,
		Evidence: []domain.Evidence{{
			At:        tl.SlotStart,
			Statement: "attestation activity was recorded for this slot but block_seen was not — the collected data is internally inconsistent",
			Source:    domain.SourceDerived,
		}},
		Remediation: []string{"check the collector's beacon API connection for the window around this slot; this combination should not occur under normal collection"},
	}, true
}
