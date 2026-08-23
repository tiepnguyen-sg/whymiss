// Package rules holds one file per rule in docs/causes.md §6's ordering,
// and order.go, which declares that ordering explicitly (ADR-0003).
package rules

import "github.com/tiepnguyen-sg/whymiss/internal/domain"

// DataCompleteness is R-010: absence-based attribution is blocked while the
// collection window remains open, and included attestations must carry the
// canonical reward evidence required to distinguish healthy from degraded.
type DataCompleteness struct{}

// ID returns R-010.
func (DataCompleteness) ID() string { return "R-010" }

// Evaluate implements rca.Rule.
func (DataCompleteness) Evaluate(tl domain.Timeline, _ Config) (*domain.Verdict, bool) {
	if tl.Duty != nil && (!tl.CollectionComplete || !tl.Has(domain.ObsCollectionCompleted)) {
		return &domain.Verdict{
			Cause:      domain.CauseInsufficientData,
			Confidence: domain.ConfidenceLow,
			Evidence: []domain.Evidence{{
				At:        tl.SlotStart,
				Statement: "the collector did not record successful completion of the full observation and inclusion window, so missing evidence cannot be interpreted",
				Source:    domain.SourceDerived,
			}},
			Remediation: []string{"wait for collection to finish; if the window already elapsed, inspect collector errors and recollect rather than interpreting missing observations"},
		}, true
	}
	if included, ok := tl.Last(domain.ObsAttestationIncluded); ok {
		_, haveHead := included.Attr(domain.AttrHeadCorrect)
		_, haveTarget := included.Attr(domain.AttrTargetCorrect)
		if !haveHead || !haveTarget {
			return &domain.Verdict{
				Cause:      domain.CauseInsufficientData,
				Confidence: domain.ConfidenceLow,
				Evidence: []domain.Evidence{{
					At:        included.At,
					Statement: "the included attestation lacks canonical head/target verification, so its reward flags cannot be determined",
					Source:    domain.SourceDerived,
				}},
				Remediation: []string{"regenerate or recollect the slot with a collector that records head_correct and target_correct"},
			}, true
		}
	}
	return nil, false
}
