package rules

import (
	"fmt"
	"strconv"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// ptcMajorityVotes is the smallest committee sample this rule will call high
// confidence. One voter's view is a data point, not a committee's finding.
const ptcMajorityVotes = 2

// PayloadLate is R-120: under ePBS the payload-timeliness committee found the
// execution payload was not present in time, for a slot whose consensus block
// exists.
//
// It sits above the local timing rules on purpose. A payload the builder did not
// reveal makes the operator's own layers look slow — the head arrives late
// through no fault of theirs — and attributing that to their consensus client or
// disk would be exactly the confident wrong verdict I-8 exists to prevent.
type PayloadLate struct{}

// ID returns R-120.
func (PayloadLate) ID() string { return "R-120" }

// Evaluate implements rca.Rule.
func (PayloadLate) Evaluate(tl domain.Timeline, _ Config) (*domain.Verdict, bool) {
	// A pre-ePBS schedule has no separate payload, so there is nothing to be
	// late. Decided by the schedule rather than a fork name (ADR-0026).
	deadline, ok := tl.Schedule.PayloadRevealDeadlineAt(tl.SlotStart)
	if !ok {
		return nil, false
	}
	// A slot with no block at all belongs to R-100, not here: blaming a builder
	// for a payload nobody asked for would name the wrong party.
	if tl.Has(domain.ObsBlockSkipped) {
		return nil, false
	}
	// A duty that earned every reward flag needs no attribution, and this gate is
	// not a formality. On the public Glamsterdam network measured 2026-08-30, 32
	// of 51 committee votes reported the payload absent while duties were being
	// included on time — without this check every healthy duty on that chain
	// would carry a cause, which is the opposite of what a verdict is for.
	if !dutyHasObservableLoss(tl) {
		return nil, false
	}
	attested, found := tl.Last(domain.ObsPayloadAttested)
	if !found {
		return nil, false
	}
	if attested.Attrs[domain.AttrPayloadPresent] != "false" {
		return nil, false
	}

	votes, err := strconv.Atoi(attested.Attrs[domain.AttrPTCVotes])
	if err != nil || votes < 1 {
		return nil, false
	}
	confidence := domain.ConfidenceMedium
	if votes >= ptcMajorityVotes {
		confidence = domain.ConfidenceHigh
	}

	return &domain.Verdict{
		Cause:      domain.CausePayloadLate,
		Confidence: confidence,
		Evidence: []domain.Evidence{
			{
				At: attested.At,
				Statement: fmt.Sprintf(
					"the payload-timeliness committee reported the execution payload was not present in time for this slot, across %d vote(s)", votes),
				Source: domain.SourceBeaconAPI,
			},
			{
				At: tl.SlotStart,
				Statement: fmt.Sprintf(
					"the payload was due %s into the slot, at %s, under the timing model this node reported",
					deadline.Sub(tl.SlotStart), deadline.UTC().Format("15:04:05.000Z")),
				Source: domain.SourceDerived,
				Comparison: &domain.Comparison{
					Label:    "payload reveal deadline",
					Observed: 0,
					Expected: deadline.Sub(tl.SlotStart).Seconds() * 1000,
					Unit:     domain.UnitMilliseconds,
				},
			},
		},
		Remediation: []string{
			"nothing to change locally: the execution payload for this slot is the builder's to reveal, and the committee found it was not there in time",
			"if this repeats across many slots, the network's builders are struggling rather than your node — compare against a second beacon node before changing anything",
		},
	}, true
}
