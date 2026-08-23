package rules

import (
	"fmt"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// CLSlow attributes dominant validation time after excluding Engine latency.
type CLSlow struct{}

// ID returns R-310.
func (CLSlow) ID() string { return "R-310" }

// Evaluate implements rca.Rule.
func (CLSlow) Evaluate(tl domain.Timeline, cfg Config) (*domain.Verdict, bool) {
	head, hasHead := tl.First(domain.ObsHeadUpdated)
	if !hasHead || !head.At.After(tl.AttestationDeadline()) {
		return nil, false
	}
	calls, complete := completeEngineCalls(tl)
	if !complete {
		return nil, false
	}
	engineTotal := engineCallTotal(calls)
	stages := ComputeStages(tl)

	if stages.HasValidation && postPropagationDominant(tl, cfg) && engineTotal < stages.Validation/2 {
		return clSlowVerdict(tl, cfg, stages, calls, engineTotal)
	}

	return nil, false
}

func clSlowVerdict(tl domain.Timeline, cfg Config, stages Stages, calls []engineCall, engineTotal time.Duration) (*domain.Verdict, bool) {
	share, _ := stages.Share(domain.StageValidation)
	evidence := []domain.Evidence{{
		At:        tl.SlotStart,
		Statement: fmt.Sprintf("consensus validation consumed %s and %.2f%% of known stage time", stages.Validation, share*100),
		Source:    domain.SourceDerived,
		Comparison: &domain.Comparison{
			Label: "validation stage share", Observed: share, Expected: cfg.Dominance, Unit: domain.UnitRatio,
		},
	}}
	for _, call := range calls {
		evidence = append(evidence, domain.Evidence{
			At:        call.at,
			Statement: fmt.Sprintf("execution client's %d %s call(s) totaled %.2fms in the canonical-head window", call.count, call.method, call.durationMS),
			Source:    domain.SourcePromScrape,
			Comparison: &domain.Comparison{
				Label: call.method + " duration", Observed: call.durationMS, Unit: domain.UnitMilliseconds,
			},
		})
	}
	evidence = append(evidence, domain.Evidence{
		At:        calls[len(calls)-1].at,
		Statement: fmt.Sprintf("sampled Engine API time (%s) accounted for less than half of the %s validation span", engineTotal, stages.Validation),
		Source:    domain.SourceDerived,
		Comparison: &domain.Comparison{
			Label: "Engine API total vs half of validation", Observed: engineTotal.Seconds() * 1000,
			Expected: (stages.Validation / 2).Seconds() * 1000, Unit: domain.UnitMilliseconds,
		},
	})
	return &domain.Verdict{
		Cause:      domain.CauseCLSlow,
		Confidence: domain.ConfidenceMedium,
		Evidence:   evidence,
		Remediation: []string{
			"check the consensus client version against the latest release",
			"review consensus client logs for the slot window",
		},
	}, true
}
