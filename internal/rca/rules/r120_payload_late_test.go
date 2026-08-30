package rules

import (
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// postEPBSSchedule mirrors the timing a Glamsterdam node reports for itself:
// ATTESTATION_DUE_BPS_GLOAS 2500, PAYLOAD_DUE_BPS 5000 and
// PAYLOAD_ATTESTATION_DUE_BPS 7500 over a 12s slot (ADR-0026).
func postEPBSSchedule() domain.SlotSchedule {
	s := domain.MainnetPreEPBS()
	s.AttestationDeadline = 3 * time.Second
	s.PayloadRevealDeadline = 6 * time.Second
	s.PTCDeadline = 9 * time.Second
	return s
}

// includedOnTime is the attestation_included a duty that lost nothing carries:
// inclusion delay 1, head and target both correct.
func includedOnTime(t *testing.T) domain.Observation {
	t.Helper()

	return mustObs(t, domain.ObsAttestationIncluded, offset(14*time.Second), map[domain.AttrKey]string{
		domain.AttrValidatorIndex: "1",
		domain.AttrInclusionDelay: "1",
		domain.AttrHeadCorrect:    "true",
		domain.AttrTargetCorrect:  "true",
	})
}

func payloadTL(t *testing.T, schedule domain.SlotSchedule, obs ...domain.Observation) domain.Timeline {
	t.Helper()

	tl, err := domain.NewTimeline(domain.Timeline{
		Slot:         100,
		SlotStart:    slotStart,
		Schedule:     schedule,
		Duty:         &domain.Duty{Kind: domain.DutyAttester, Slot: 100, ValidatorIndex: 1},
		Observations: obs,
	})
	if err != nil {
		t.Fatalf("NewTimeline: %v", err)
	}
	return tl
}

func ptcVote(t *testing.T, present string, votes string) domain.Observation {
	t.Helper()

	return mustObs(t, domain.ObsPayloadAttested, offset(13*time.Second), map[domain.AttrKey]string{
		domain.AttrPayloadPresent: present,
		domain.AttrPTCVotes:       votes,
	})
}

func TestPayloadLate(t *testing.T) {
	t.Run("reports the builder when the committee says the payload was absent", func(t *testing.T) {
		tl := payloadTL(t, postEPBSSchedule(),
			mustObs(t, domain.ObsBlockSeen, offset(time.Second), nil),
			ptcVote(t, "false", "3"))

		v, ok := (PayloadLate{}).Evaluate(tl, defaultCfg)
		if !ok || v.Cause != domain.CausePayloadLate {
			t.Fatalf("verdict=%+v matched=%t, want network.payload_late", v, ok)
		}
		if v.Confidence != domain.ConfidenceHigh {
			t.Errorf("confidence = %s, want high on a three-vote sample", v.Confidence)
		}
		if len(v.Evidence) < 2 {
			t.Errorf("evidence = %d items, want the committee's finding and the deadline it missed", len(v.Evidence))
		}
	})

	t.Run("a single vote is medium, not high", func(t *testing.T) {
		tl := payloadTL(t, postEPBSSchedule(),
			mustObs(t, domain.ObsBlockSeen, offset(time.Second), nil),
			ptcVote(t, "false", "1"))

		v, ok := (PayloadLate{}).Evaluate(tl, defaultCfg)
		if !ok || v.Confidence != domain.ConfidenceMedium {
			t.Fatalf("verdict=%+v matched=%t, want medium confidence", v, ok)
		}
	})

	t.Run("declines when the committee says the payload was present", func(t *testing.T) {
		tl := payloadTL(t, postEPBSSchedule(),
			mustObs(t, domain.ObsBlockSeen, offset(time.Second), nil),
			ptcVote(t, "true", "3"))

		if v, ok := (PayloadLate{}).Evaluate(tl, defaultCfg); ok {
			t.Fatalf("claimed %+v for a payload the committee found present", v)
		}
	})

	// The whole point of deciding by schedule rather than fork name: on every
	// pre-Glamsterdam chain this rule must be inert.
	t.Run("declines on a pre-ePBS schedule even with a vote present", func(t *testing.T) {
		tl := payloadTL(t, domain.MainnetPreEPBS(),
			mustObs(t, domain.ObsBlockSeen, offset(time.Second), nil),
			ptcVote(t, "false", "3"))

		if v, ok := (PayloadLate{}).Evaluate(tl, defaultCfg); ok {
			t.Fatalf("claimed %+v on a schedule with no payload deadline", v)
		}
	})

	// A skipped slot is R-100's, and blaming a builder for it names the wrong party.
	t.Run("declines when the slot was skipped", func(t *testing.T) {
		tl := payloadTL(t, postEPBSSchedule(),
			mustObs(t, domain.ObsBlockSkipped, offset(time.Second), nil),
			ptcVote(t, "false", "3"))

		if v, ok := (PayloadLate{}).Evaluate(tl, defaultCfg); ok {
			t.Fatalf("claimed %+v for a slot the chain skipped", v)
		}
	})

	// The gate that a live run caught missing: on a chain where most payloads are
	// late, a duty that still earned every flag must not be given a cause.
	t.Run("declines when the duty lost nothing", func(t *testing.T) {
		tl := payloadTL(t, postEPBSSchedule(),
			mustObs(t, domain.ObsBlockSeen, offset(time.Second), nil),
			ptcVote(t, "false", "3"),
			includedOnTime(t))

		if v, ok := (PayloadLate{}).Evaluate(tl, defaultCfg); ok {
			t.Fatalf("claimed %+v for a duty that earned every reward flag", v)
		}
	})

	t.Run("declines with no committee vote at all", func(t *testing.T) {
		tl := payloadTL(t, postEPBSSchedule(),
			mustObs(t, domain.ObsBlockSeen, offset(time.Second), nil))

		if v, ok := (PayloadLate{}).Evaluate(tl, defaultCfg); ok {
			t.Fatalf("claimed %+v with no payload_attested observation", v)
		}
	})
}
