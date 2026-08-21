package rca

import (
	"strconv"

	"github.com/CHANGEME/whymiss/internal/domain"
)

// deriveOutcome computes what actually happened to the duty from tl's
// observations, before any cause rule runs.
//
// This is a documented simplification, not a hidden one: the domain model
// has no observation kind independently confirming a published
// attestation's source/target checkpoint was correct (docs/causes.md §8's
// closed vocabulary has no such kind), so TimelySource and TimelyTarget are
// treated as earned whenever the attestation was included at all — this
// build does not independently verify checkpoint correctness. TimelyHead is
// the one flag this data actually distinguishes: earned only when
// inclusion_delay == 1, per its own definition in docs/causes.md §8.1.
func deriveOutcome(tl domain.Timeline) (domain.Outcome, *domain.RewardFlags) {
	if tl.Duty == nil {
		return domain.OutcomeNoDuty, nil
	}

	if tl.Duty.Kind == domain.DutyProposer {
		if tl.Has(domain.ObsBlockProposed) {
			return domain.OutcomeOK, nil
		}
		return domain.OutcomeMissed, nil
	}

	included, ok := tl.Last(domain.ObsAttestationIncluded)
	if !ok {
		return domain.OutcomeMissed, nil
	}

	timelyHead := false
	if delayStr, ok := included.Attr(domain.AttrInclusionDelay); ok {
		if delay, err := strconv.ParseUint(delayStr, 10, 64); err == nil {
			timelyHead = delay == 1
		}
		// An unparseable inclusion_delay defensively falls to timelyHead =
		// false: the attestation is confirmed included either way, and
		// "not confirmed timely" is the safe direction to be wrong in
		// (I-8) — it never produces a false OutcomeOK.
	}

	if timelyHead {
		return domain.OutcomeOK, &domain.RewardFlags{TimelySource: true, TimelyTarget: true, TimelyHead: true}
	}
	return domain.OutcomeDegraded, &domain.RewardFlags{TimelySource: true, TimelyTarget: true, TimelyHead: false}
}
