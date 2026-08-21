package rules

import (
	"fmt"

	"github.com/CHANGEME/whymiss/internal/domain"
	"github.com/CHANGEME/whymiss/internal/rca"
)

// CLSlow is R-310: the consensus client consumed the validation budget,
// and the execution client is not responsible — the elimination case right
// after R-300: reached only when R-300 didn't find engine_call evidence to
// implicate EL specifically.
//
// Fires two ways: (1) the ordinary case, post-propagation stage dominant
// with no engine_call evidence (see postPropagationDominant's doc comment
// on the head_updated gap this depends on) and no more specific R-410
// evidence available (see vcTimingShapeMatches — without this exclusion,
// this branch preempts R-410 for every vc_slow case, since R-310 runs
// first and its condition is a strict superset of R-410's when signing
// can't be separated from validation); (2) a degraded inclusion with
// no attestation_published observation and no propagation problem —
// covers a real corpus shape (cl-slow-cpu) where attestation_published was
// never captured (the polling window closed before the pool endpoint
// reflected it — a documented measurement gap in that scenario's own
// README) but attestation_included's delay > 1 is still direct evidence
// something past propagation was slow.
type CLSlow struct{}

// ID returns R-310.
func (CLSlow) ID() string { return "R-310" }

// Evaluate implements rca.Rule.
func (CLSlow) Evaluate(tl domain.Timeline, cfg rca.Config) (*domain.Verdict, bool) {
	if len(engineCalls(tl)) > 0 {
		return nil, false
	}

	if postPropagationDominant(tl, cfg) && !vcTimingShapeMatches(tl) {
		return clSlowVerdict(tl, "the post-propagation span (validation, and signing where separable) dominated this duty's overspend, with no engine_call evidence implicating the execution client")
	}

	if delay, ok := inclusionDelay(tl); ok && delay > 1 && !tl.Has(domain.ObsAttestationPublished) {
		return clSlowVerdict(tl, fmt.Sprintf("attestation_included recorded an inclusion delay of %d (not the timely-head value of 1) with no attestation_published observation to isolate signing separately, and no engine_call evidence implicating the execution client", delay))
	}

	return nil, false
}

func clSlowVerdict(tl domain.Timeline, statement string) (*domain.Verdict, bool) {
	return &domain.Verdict{
		Cause:      domain.CauseCLSlow,
		Confidence: domain.ConfidenceMedium, // CL internals are poorly instrumented across clients (docs/causes.md §7) — never high without a client-specific corroborating metric this build doesn't collect.
		Evidence: []domain.Evidence{{
			At:        tl.SlotStart,
			Statement: statement,
			Source:    domain.SourceDerived,
		}},
		Remediation: []string{
			"check the consensus client version against the latest release",
			"review consensus client logs for the slot window",
		},
	}, true
}
