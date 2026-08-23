package rules

import (
	"fmt"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

const metricELEngineCallsP99MS domain.MetricName = "el_engine_calls_p99_ms"

// ELSlow requires per-slot Engine evidence and a rolling p99 baseline.
type ELSlow struct{}

// ID returns R-300.
func (ELSlow) ID() string { return "R-300" }

// Evaluate implements rca.Rule.
func (ELSlow) Evaluate(tl domain.Timeline, cfg Config) (*domain.Verdict, bool) {
	head, hasHead := tl.First(domain.ObsHeadUpdated)
	if !hasHead || !head.At.After(tl.AttestationDeadline()) {
		return nil, false
	}
	stages := ComputeStages(tl)
	dominant, ok := stages.Dominant(cfg)
	if !ok || dominant != domain.StageValidation || !stages.HasValidation {
		return nil, false
	}

	calls, complete := completeEngineCalls(tl)
	if !complete {
		return nil, false
	}
	total := engineCallTotal(calls)
	if total < stages.Validation/2 {
		return nil, false
	}
	baselineMS, ok := tl.SampleValue(domain.ComponentEL, metricELEngineCallsP99MS)
	if !ok || baselineMS <= 0 {
		return nil, false
	}
	thresholdMS := cfg.EngineSpikeMultiplier * baselineMS
	if total.Seconds()*1000 <= thresholdMS {
		return nil, false
	}

	evidence := make([]domain.Evidence, 0, len(calls)+1)
	for _, c := range calls {
		evidence = append(evidence, domain.Evidence{
			At:        c.at,
			Statement: fmt.Sprintf("execution client's %d %s call(s) totaled %.2fms in the canonical-head window", c.count, c.method, c.durationMS),
			Source:    domain.SourcePromScrape,
			Comparison: &domain.Comparison{
				Label:    c.method + " duration",
				Observed: c.durationMS,
				Unit:     domain.UnitMilliseconds,
			},
		})
	}
	evidence = append(evidence, domain.Evidence{
		At:        calls[len(calls)-1].at,
		Statement: fmt.Sprintf("Engine API calls totaled %s against a %.2fms rolling p99 and consumed at least half of validation", total, baselineMS),
		Source:    domain.SourceDerived,
		Comparison: &domain.Comparison{
			Label:    "Engine API total vs spike threshold",
			Observed: total.Seconds() * 1000,
			Expected: thresholdMS,
			Unit:     domain.UnitMilliseconds,
		},
	})
	return &domain.Verdict{
		Cause:      domain.CauseELSlow,
		Confidence: domain.ConfidenceMedium,
		Evidence:   evidence,
		Remediation: []string{
			"check the execution client version against the latest release",
			"inspect execution-client logs and host I/O telemetry for the exact canonical-head window before assigning a sub-cause",
		},
	}, true
}
