package rules

import (
	"fmt"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// CatchAll is R-999: data was complete and trustworthy, yet no rule
// matched. Unconditional — always matches, which is what guarantees
// rca.Analyze's rule loop always terminates. Reported as a taxonomy gap and
// a project bug, never a guess (I-8): tracking this cause's rate is a
// project health metric, per docs/causes.md §7.
type CatchAll struct{}

// ID returns R-999.
func (CatchAll) ID() string { return "R-999" }

// Evaluate implements rca.Rule.
func (CatchAll) Evaluate(tl domain.Timeline, _ Config) (*domain.Verdict, bool) {
	stages := ComputeStages(tl)
	verdictAt := tl.SlotStart
	if completed, ok := tl.First(domain.ObsCollectionCompleted); ok {
		verdictAt = completed.At
	}
	evidence := []domain.Evidence{{
		At:        verdictAt,
		Statement: "data was complete and trustworthy, yet no rule in the ordered sequence matched — this is a taxonomy gap, not an operator problem",
		Source:    domain.SourceDerived,
	}}
	total := stages.Total()
	for _, stage := range []struct {
		name  domain.Stage
		value time.Duration
		known bool
	}{
		{name: domain.StagePropagation, value: stages.Propagation, known: stages.HasPropagation},
		{name: domain.StageValidation, value: stages.Validation, known: stages.HasValidation},
		{name: domain.StageSigning, value: stages.Signing, known: stages.HasSigning},
	} {
		if !stage.known {
			evidence = append(evidence, domain.Evidence{
				At: tl.SlotStart, Statement: fmt.Sprintf("%s stage duration and share were unavailable because its timing boundary was not observed", stage.name), Source: domain.SourceDerived,
			})
			continue
		}
		share, _ := stages.Share(stage.name)
		evidence = append(evidence,
			domain.Evidence{
				At: tl.SlotStart, Statement: fmt.Sprintf("%s stage duration was %s", stage.name, stage.value), Source: domain.SourceDerived,
				Comparison: &domain.Comparison{Label: string(stage.name) + " duration", Observed: stage.value.Seconds() * 1000, Expected: total.Seconds() * 1000, Unit: domain.UnitMilliseconds},
			},
			domain.Evidence{
				At: tl.SlotStart, Statement: fmt.Sprintf("%s stage accounted for %.2f%% of known stage time", stage.name, share*100), Source: domain.SourceDerived,
				Comparison: &domain.Comparison{Label: string(stage.name) + " share", Observed: share, Expected: 1, Unit: domain.UnitRatio},
			},
		)
	}
	return &domain.Verdict{
		Cause:       domain.CauseNoRuleMatched,
		Confidence:  domain.ConfidenceLow,
		Evidence:    evidence,
		Remediation: []string{"this is a taxonomy gap and a project bug, not an operator problem — file an issue with this timeline attached; every occurrence should become a corpus scenario"},
	}, true
}
