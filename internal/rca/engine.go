package rca

import (
	"fmt"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// EngineVersion is stamped into every Verdict this build produces (I-10) —
// bump it whenever a rule's logic changes in a way that could change a
// past slot's verdict if re-analyzed.
const EngineVersion = "0.1.0"

// Rule is one cause-attribution rule, evaluated in the order rules.Order
// declares (ADR-0003).
type Rule interface {
	// ID names the rule for diagnostics and golden-test output, e.g.
	// "R-300" — matching docs/causes.md §6's numbering.
	ID() string

	// Evaluate reports whether the rule applies to tl under cfg. A rule
	// that does not apply returns (nil, false) and has no other effect.
	//
	// The returned Verdict is a draft: Slot, Outcome, Flags, EngineVersion,
	// and TaxonomyVersion are left zero. Analyze fills those in and is the
	// only place domain.NewVerdict is called, so a rule only ever has to
	// get Cause/SubCause/Confidence/Evidence/Remediation right.
	Evaluate(tl domain.Timeline, cfg Config) (*domain.Verdict, bool)
}

// order is set by rules.go's init-time wiring via SetOrder, avoiding an
// import cycle between rca (which rules depend on for the Rule interface)
// and rules (which rca would otherwise need to import to get Order).
var order []Rule

// SetOrder registers the ordered rule sequence. Called exactly once, from
// the rules package's init(), per doc comment there — this indirection
// exists only to break the import cycle Rule's definition would otherwise
// create (internal/rca/rules needs rca.Rule; internal/rca needs the
// ordered list to run).
func SetOrder(rs []Rule) {
	order = rs
}

// Analyze turns tl into a Verdict: pure, deterministic, no I/O (I-6,
// ADR-0003). Rules run in the order rules.init registered via SetOrder,
// first match wins. R-999 (rules.CatchAll) is unconditional, so the loop
// below always terminates with a match in correctly wired code — the
// fallback after the loop exists only to satisfy I-15 (no panics outside
// main) if that invariant is ever broken by a rule-ordering bug, not
// because it is expected to run.
func Analyze(tl domain.Timeline, cfg Config) domain.Verdict {
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

	for _, r := range order {
		if draft, ok := r.Evaluate(tl, cfg); ok {
			return finish(tl, *draft, outcome, flags)
		}
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
			At:        time.Now().UTC(),
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
