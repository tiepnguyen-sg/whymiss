package rules

import (
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func TestProposerMissed(t *testing.T) {
	t.Run("matches a canonically confirmed skipped slot", func(t *testing.T) {
		tl := timelineWith(t, mustObs(t, domain.ObsBlockSkipped, offset(3*time.Second), nil))
		v, ok := ProposerMissed{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Cause != domain.CauseProposerMissed {
			t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseProposerMissed)
		}
		if v.Confidence != domain.ConfidenceHigh {
			t.Errorf("Confidence = %q, want high", v.Confidence)
		}
		if got, want := v.Evidence[0].At, offset(3*time.Second); !got.Equal(want) {
			t.Errorf("Evidence[0].At = %s, want block_skipped timestamp %s", got, want)
		}
	})

	t.Run("does not infer a skipped slot from missing observations", func(t *testing.T) {
		tl := timelineWith(t)
		if _, ok := (ProposerMissed{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match without block_skipped evidence")
		}
	})

	t.Run("does not match when block_seen exists", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsBlockSeen, offset(time.Second), nil),
			mustObs(t, domain.ObsBlockSkipped, offset(3*time.Second), nil),
		)
		if _, ok := (ProposerMissed{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("does not match when head_updated proves a block existed", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsHeadUpdated, offset(time.Second), nil),
			mustObs(t, domain.ObsBlockSkipped, offset(3*time.Second), nil),
		)
		if _, ok := (ProposerMissed{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("allows a normal attestation for the previous head", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsAttestationPublished, offset(2*time.Second), nil),
			mustObs(t, domain.ObsBlockSkipped, offset(3*time.Second), nil),
		)
		if _, ok := (ProposerMissed{}).Evaluate(tl, defaultCfg); !ok {
			t.Fatal("want match: attesters can publish normally during a skipped slot")
		}
	})

	t.Run("does not exonerate the operator for its own missed proposer duty", func(t *testing.T) {
		tl := timelineWith(t, mustObs(t, domain.ObsBlockSkipped, offset(3*time.Second), nil))
		tl.Duty = &domain.Duty{Kind: domain.DutyProposer, Slot: tl.Slot, ValidatorIndex: 1}
		if _, ok := (ProposerMissed{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match for an operator proposer duty")
		}
	})

	t.Run("does not report proposer missed when block_proposed exists", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsBlockProposed, offset(time.Second), nil),
			mustObs(t, domain.ObsBlockSkipped, offset(3*time.Second), nil),
		)
		if _, ok := (ProposerMissed{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match when proposal evidence exists")
		}
	})
}
