package rca

import (
	"strconv"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// The consensus-spec timely-source bound is integer_squareroot(SLOTS_PER_EPOCH).
const timelySourceMaxInclusionDelay = 5

// deriveOutcome computes what actually happened to the duty from tl's
// observations, before any cause rule runs.
//
// Inclusion in a valid block proves the source checkpoint was correct. The
// adapter separately persists whether the voted target and head match the
// canonical roots; inclusion delay alone is never treated as that proof.
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

	var delay uint64
	haveDelay := false
	if delayStr, ok := included.Attr(domain.AttrInclusionDelay); ok {
		if parsed, err := strconv.ParseUint(delayStr, 10, 64); err == nil {
			delay, haveDelay = parsed, true
		}
	}
	headCorrect, haveHead := included.Attr(domain.AttrHeadCorrect)
	targetCorrect, haveTarget := included.Attr(domain.AttrTargetCorrect)
	matchingTarget := haveTarget && targetCorrect == "true"

	flags := &domain.RewardFlags{
		TimelySource: haveDelay && delay <= timelySourceMaxInclusionDelay,
		TimelyTarget: haveDelay && matchingTarget,
		TimelyHead:   haveDelay && delay == 1 && matchingTarget && haveHead && headCorrect == "true",
	}

	if flags.AllEarned() {
		return domain.OutcomeOK, flags
	}
	return domain.OutcomeDegraded, flags
}
