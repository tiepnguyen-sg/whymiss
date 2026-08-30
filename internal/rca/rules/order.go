package rules

import "github.com/tiepnguyen-sg/whymiss/internal/domain"

// Rule is one deterministic cause-attribution rule.
type Rule interface {
	ID() string
	Evaluate(domain.Timeline, Config) (*domain.Verdict, bool)
}

// Order returns a fresh ordered rule sequence. First match wins (ADR-0003).
// A fresh slice prevents callers and tests from mutating analyzer state.
func Order() []Rule {
	return []Rule{
		DataCompleteness{},
		ClockTrust{},
		ProposerMissed{},
		PayloadLate{},
		NetworkLateBlock{},
		P2PDegraded{},
		ELSlow{},
		CLSlow{},
		VCDisconnected{},
		VCSlow{},
		InclusionFailure{},
		HostFallback{},
		CatchAll{},
	}
}
