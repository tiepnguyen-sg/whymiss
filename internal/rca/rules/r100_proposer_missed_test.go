package rules

import (
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func TestProposerMissed(t *testing.T) {
	t.Run("exonerates a confirmed skip when the attestation was included", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsBlockSkipped, offset(3*time.Second), nil),
			mustObs(t, domain.ObsAttestationIncluded, offset(14*time.Second), map[domain.AttrKey]string{
				domain.AttrInclusionDelay: "1", domain.AttrHeadCorrect: "true", domain.AttrTargetCorrect: "true",
			}),
		)
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
		if len(v.Evidence) < 2 {
			t.Fatalf("Evidence has %d entries, want the inclusion cited alongside the skip", len(v.Evidence))
		}
	})

	// The defect this rule was rewritten for: five recorded devnet scenarios
	// whose validator client was paused or capped to 0.1% of a core produced
	// exactly this shape, and R-100 answered network.proposer_missed at high
	// confidence with no remediation — telling an operator whose VC was down
	// that nothing was theirs to fix.
	t.Run("refuses to exonerate when no attestation was ever observed", func(t *testing.T) {
		tl := timelineWith(t, mustObs(t, domain.ObsBlockSkipped, offset(3*time.Second), nil))
		v, ok := ProposerMissed{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want a match: the ambiguity itself is the verdict")
		}
		if v.Cause != domain.CauseInsufficientData {
			t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseInsufficientData)
		}
		if v.Confidence != domain.ConfidenceLow {
			t.Errorf("Confidence = %q, want low", v.Confidence)
		}
		if len(v.Remediation) == 0 {
			t.Error("want remediation naming the validator client; an operator with a dead VC must not be told there is nothing to do")
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

	t.Run("leaves a published but never included attestation to R-500", func(t *testing.T) {
		// Non-inclusion of an on-time attestation is R-500's question. R-100
		// answering it would exonerate the network for a loss it has not been
		// shown to have caused.
		tl := timelineWith(t,
			mustObs(t, domain.ObsAttestationPublished, offset(2*time.Second), nil),
			mustObs(t, domain.ObsBlockSkipped, offset(3*time.Second), nil),
		)
		if _, ok := (ProposerMissed{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match so R-500 can own non-inclusion")
		}
	})

	t.Run("exonerates when a published attestation was also included", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsAttestationPublished, offset(2*time.Second), nil),
			mustObs(t, domain.ObsBlockSkipped, offset(3*time.Second), nil),
			mustObs(t, domain.ObsAttestationIncluded, offset(14*time.Second), map[domain.AttrKey]string{
				domain.AttrInclusionDelay: "1", domain.AttrHeadCorrect: "true", domain.AttrTargetCorrect: "true",
			}),
		)
		v, ok := ProposerMissed{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match: attesters publish and are included normally during a skipped slot")
		}
		if v.Cause != domain.CauseProposerMissed || v.Confidence != domain.ConfidenceHigh {
			t.Errorf("got %q/%q, want %q/high", v.Cause, v.Confidence, domain.CauseProposerMissed)
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
