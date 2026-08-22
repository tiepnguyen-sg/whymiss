package rules

import (
	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/rca"
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
func (CatchAll) Evaluate(tl domain.Timeline, _ rca.Config) (*domain.Verdict, bool) {
	stages := rca.ComputeStages(tl)
	return &domain.Verdict{
		Cause:      domain.CauseNoRuleMatched,
		Confidence: domain.ConfidenceLow,
		Evidence: []domain.Evidence{{
			At:        tl.SlotStart,
			Statement: "data was complete and trustworthy, yet no rule in the ordered sequence matched — this is a taxonomy gap, not an operator problem",
			Source:    domain.SourceDerived,
			Comparison: &domain.Comparison{
				Label:    "stage total",
				Observed: stages.Total().Seconds() * 1000,
				Unit:     domain.UnitMilliseconds,
			},
		}},
		Remediation: []string{"this is a taxonomy gap and a project bug, not an operator problem — file an issue with this timeline attached; every occurrence should become a corpus scenario"},
	}, true
}
