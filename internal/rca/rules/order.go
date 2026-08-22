package rules

import "github.com/tiepnguyen-sg/whymiss/internal/rca"

// Order is the ordered rule sequence, first match wins (ADR-0003,
// docs/causes.md §6). R-001 (duty guard) is not a Rule here — it's handled
// directly in rca.Analyze before this list ever runs, since "nothing was
// owed" isn't a cause attribution at all (domain.Verdict forbids a Cause
// when Outcome is OutcomeNoDuty).
var Order = []rca.Rule{
	DataCompleteness{}, // R-010: cannot reason on missing data — before clock trust, since a malformed timeline is worse than an untrusted clock.
	ClockTrust{},       // R-011: a bad clock invalidates every timing measurement below it (I-9) — must run before any timing-based rule.
	ProposerMissed{},   // R-100: exonerates the operator immediately — checked before anything that would blame this node for a block nobody produced.
	NetworkLateBlock{}, // R-110: needs a baseline; only matches once one exists, otherwise falls through to R-200.
	P2PDegraded{},      // R-200: propagation precedes validation in the latency budget, so it's attributed before anything downstream of it.
	ELSlow{},           // R-300: most common local cause in practice (docs/causes.md §6).
	CLSlow{},           // R-310: the elimination case right after R-300 — validation remainder once EL is ruled out.
	VCDisconnected{},   // R-400: binary and unambiguous, checked before the softer VCSlow.
	VCSlow{},           // R-410: last stage in the chain.
	InclusionFailure{}, // R-500: published on time yet absent on chain — after every local cause, since this is a network-side failure once the operator's own stack is ruled out.
	HostFallback{},     // R-600: terminal only when no layer above matched — host signals are corroboration inside R-300/R-310 first.
	CatchAll{},         // R-999: unconditional; guarantees the sequence always terminates with a match.
}

func init() {
	rca.SetOrder(Order)
}
