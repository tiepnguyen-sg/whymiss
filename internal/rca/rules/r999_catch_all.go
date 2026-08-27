package rules

import (
	"fmt"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// CatchAll is R-999, the terminal fall-through. Unconditional — always
// matches, which is what guarantees rca.Analyze's rule loop always terminates.
//
// It reports one of two causes, and telling them apart is the point.
// unknown.no_rule_matched means what docs/causes.md says it means: the data was
// complete and trustworthy and the taxonomy still had nothing to say, which is a
// project bug worth filing. That claim is only honest when the stage
// decomposition was actually measured. When no stage boundary was observed at
// all there is nothing to have matched against, and calling that a taxonomy gap
// blames the project for evidence the deployment never collected — so it reports
// unknown.insufficient_data instead, and names the flag that would fix it
// (ADR-0024).
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

	// Nothing was timed, so nothing could have matched. Stage boundaries come
	// from a block_seen measured by client metrics (timedBlockSeen); a
	// collector running without --cl-metrics-api records only the Beacon API's
	// polled block_seen, whose timestamp is when the collector noticed rather
	// than when the block arrived. Every timing rule then declines for want of
	// input, and reporting that as a taxonomy gap tells an operator to file a
	// bug about their own missing configuration.
	//
	// Gated on the duty actually having lost something. A duty that earned every
	// reward flag needs no attribution at all, measured or not, and the engine
	// turns R-999's other branch into its clean-pass verdict — reporting
	// insufficient data for a healthy duty would replace "nothing went wrong"
	// with "we could not tell", which is strictly worse and false besides.
	if dutyHasObservableLoss(tl) && !stages.HasPropagation && !stages.HasValidation && !stages.HasSigning {
		return &domain.Verdict{
			Cause:      domain.CauseInsufficientData,
			Confidence: domain.ConfidenceLow,
			Evidence: []domain.Evidence{
				{
					At:        verdictAt,
					Statement: "no stage of this duty was timed: block arrival was never measured, so propagation, validation, and signing durations are all unknown and no timing rule could be evaluated",
					Source:    domain.SourceDerived,
				},
				{
					At:        tl.SlotStart,
					Statement: "the Beacon API's polled block_seen records when the collector noticed the block, not when it arrived, and is deliberately not used as a stage boundary",
					Source:    domain.SourceDerived,
				},
			},
			Remediation: []string{
				"set --cl-metrics-api to the consensus client's Prometheus endpoint so block arrival is measured rather than polled; without it no timing-based cause can be attributed",
				"set --baseline-beacon-api as well if you need network-wide lateness told apart from local lateness — any independent node you can reach will do, and --baseline-metrics-api on top of it is optional (ADR-0025)",
			},
		}, true
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
