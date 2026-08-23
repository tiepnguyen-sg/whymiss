package rca

import (
	"fmt"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/rca/rules"
)

// EngineVersion is stamped into every Verdict this build produces (I-10) —
// bump it whenever a rule's logic changes in a way that could change a
// past slot's verdict if re-analyzed.
const EngineVersion = "0.13.0"

// Rule is one cause-attribution rule.
type Rule = rules.Rule

// Analyze turns tl into a Verdict: pure, deterministic, no I/O (I-6,
// ADR-0003). Rules run in the fixed order returned by rules.Order,
// first match wins. R-999 (rules.CatchAll) is unconditional, so the loop
// below always terminates with a match in correctly wired code — the
// fallback after the loop exists only to satisfy I-15 (no panics outside
// main) if that invariant is ever broken by a rule-ordering bug, not
// because it is expected to run.
//
// Two outcomes carry no cause at all: no_duty (nothing was owed) and a
// clean ok (nothing went wrong for any rule to attribute) — see the
// branches below for why the second is decided after the rule loop, not
// before it.
func Analyze(tl domain.Timeline, cfg Config) domain.Verdict {
	return analyze(tl, cfg, rules.Order())
}

func analyze(tl domain.Timeline, cfg Config, ordered []Rule) domain.Verdict {
	outcome, flags := deriveOutcome(tl)

	if outcome == domain.OutcomeNoDuty {
		return finish(tl, domain.Verdict{
			Confidence: domain.ConfidenceHigh,
			Evidence: []domain.Evidence{{
				At:        tl.SlotStart,
				Statement: "no attester or proposer duty was assigned for this slot",
				Source:    domain.SourceDerived,
			}},
		}, outcome, nil)
	}

	for _, r := range ordered {
		draft, ok := r.Evaluate(tl, cfg)
		if !ok {
			continue
		}
		// Only the unconditional catch-all matched (CauseNoRuleMatched is
		// produced nowhere else), and nothing actually went wrong. That is a
		// clean pass, not the taxonomy gap R-999 reports it as — telling an
		// operator their healthy validator is a project bug is exactly the
		// kind of false signal I-8 exists to prevent.
		//
		// This check deliberately runs after the loop rather than
		// short-circuiting before it, the way OutcomeNoDuty does above: no
		// rule inspects Outcome, so a real rule can still match on a duty
		// that ended up ok (a VC that was measurably slow yet beat the
		// deadline — test/corpus/vc-slow-cpu is exactly that). Skipping the
		// loop would silently discard those.
		if outcome == domain.OutcomeOK && draft.Cause == domain.CauseNoRuleMatched {
			verdictAt := tl.SlotStart
			if completed, ok := tl.First(domain.ObsCollectionCompleted); ok {
				verdictAt = completed.At
			}
			return finish(tl, domain.Verdict{
				Confidence: domain.ConfidenceHigh,
				Evidence: []domain.Evidence{{
					At:        verdictAt,
					Statement: "duty fulfilled with every reward flag earned, and no rule found a problem",
					Source:    domain.SourceDerived,
				}},
			}, outcome, flags)
		}
		return finish(tl, *draft, outcome, flags)
	}

	// Defensive-only path — see the doc comment above.
	return finish(tl, domain.Verdict{
		Cause:      domain.CauseNoRuleMatched,
		Confidence: domain.ConfidenceLow,
		Evidence: []domain.Evidence{{
			At:        tl.SlotStart,
			Statement: "no rule in the ordered sequence matched, including the unconditional catch-all — this is an engine bug",
			Source:    domain.SourceDerived,
		}},
		Remediation: []string{"this is a taxonomy gap and a project bug, not an operator problem — file an issue with this timeline attached"},
	}, outcome, flags)
}

// finish stamps the fields only Analyze can fill in and validates the
// result. On a validation error — a bug in the rule that produced draft,
// never expected in correctly implemented rules — it falls back to a
// hand-built, statically-valid unknown.no_rule_matched verdict rather than
// panicking (I-15) or returning an error Analyze's ADR-0003-fixed signature
// has no room for.
func finish(tl domain.Timeline, draft domain.Verdict, outcome domain.Outcome, flags *domain.RewardFlags) domain.Verdict {
	draft.Slot = tl.Slot
	draft.Outcome = outcome
	draft.Flags = flags
	draft.EngineVersion = EngineVersion
	for i := range draft.Evidence {
		draft.Evidence[i].Offset = draft.Evidence[i].At.Sub(tl.SlotStart)
	}

	v, err := domain.NewVerdict(draft)
	if err != nil {
		return safeFallback(tl, outcome, flags, err)
	}
	return v
}

// safeFallback builds a verdict that is valid by inspection — every field
// satisfies domain.Verdict.Validate's rules directly, so this call cannot
// itself fail and recurse.
func safeFallback(tl domain.Timeline, outcome domain.Outcome, flags *domain.RewardFlags, cause error) domain.Verdict {
	v := domain.Verdict{
		Slot:       tl.Slot,
		Outcome:    outcome,
		Flags:      flags,
		Cause:      domain.CauseNoRuleMatched,
		Confidence: domain.ConfidenceLow,
		Evidence: []domain.Evidence{{
			// SlotStart, not time.Now: the engine reads no clock (I-6).
			At:        tl.SlotStart,
			Statement: fmt.Sprintf("a matching rule produced an invalid verdict (%v) — this is an engine bug, not an attribution", cause),
			Source:    domain.SourceDerived,
		}},
		Remediation:     []string{"this is a taxonomy gap and a project bug, not an operator problem — file an issue with this timeline attached"},
		EngineVersion:   EngineVersion,
		TaxonomyVersion: domain.TaxonomyVersion,
	}
	if outcome == domain.OutcomeNoDuty {
		v.Cause = ""
		v.Flags = nil
	}
	return v
}
