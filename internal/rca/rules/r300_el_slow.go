package rules

import (
	"fmt"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/rca"
)

// ELSlow is R-300: the execution client responded slowly to Engine API
// calls, consuming the validation budget.
//
// docs/causes.md's rule compares Engine API duration against
// cfg.EngineSpikeMultiplier times the node's own rolling p99 — a baseline
// nothing in this codebase computes yet (it would live as a derived
// domain.MetricSample on the Timeline, per domain.MetricSample's own doc
// comment, but internal/timeline.Assembler doesn't produce one). Absent
// that baseline, this rule uses direct engine_call evidence as its own
// corroboration: any engine_call observation for this slot is evidence EL
// was on the critical path at all (R-310, immediately after this rule in
// the order, is what fires when validation was slow and *no* engine_call
// evidence exists — the elimination case).
type ELSlow struct{}

// ID returns R-300.
func (ELSlow) ID() string { return "R-300" }

// Evaluate implements rca.Rule.
func (ELSlow) Evaluate(tl domain.Timeline, cfg rca.Config) (*domain.Verdict, bool) {
	if !postPropagationDominant(tl, cfg) {
		return nil, false
	}

	calls := engineCalls(tl)
	if len(calls) == 0 {
		return nil, false
	}

	subCause, subSignal := elSubCause(tl)
	confidence := domain.ConfidenceMedium
	if subCause != "" {
		confidence = domain.ConfidenceHigh
	}

	evidence := make([]domain.Evidence, 0, len(calls)+1)
	for _, c := range calls {
		evidence = append(evidence, domain.Evidence{
			At:        c.at,
			Statement: fmt.Sprintf("execution client's %s call took %.2fms", c.method, c.durationMS),
			Source:    domain.SourcePromScrape,
			Comparison: &domain.Comparison{
				Label:    c.method + " duration",
				Observed: c.durationMS,
				Unit:     domain.UnitMilliseconds,
			},
		})
	}
	if subSignal != "" {
		evidence = append(evidence, domain.Evidence{At: tl.SlotStart, Statement: subSignal, Source: domain.SourceHostMetrics})
	}

	remediation := []string{"check the execution client version against the latest release"}
	switch subCause {
	case domain.CauseELSlowSyncing:
		remediation = []string{"wait for sync to complete; do not attest from an unsynced node"}
	case domain.CauseELSlowDiskSaturation:
		remediation = []string{"this box needs a faster NVMe drive — consumer SATA SSDs are the most common cause of chronic attestation loss"}
	}

	return &domain.Verdict{
		Cause:       domain.CauseELSlow,
		SubCause:    subCause,
		Confidence:  confidence,
		Evidence:    evidence,
		Remediation: remediation,
	}, true
}
