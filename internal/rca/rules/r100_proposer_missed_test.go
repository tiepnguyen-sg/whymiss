package rules

import (
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func TestProposerMissed(t *testing.T) {
	t.Run("matches when neither block_seen nor attestation_published exist", func(t *testing.T) {
		tl := timelineWith(t)
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
	})

	t.Run("does not match when block_seen exists", func(t *testing.T) {
		tl := timelineWith(t, mustObs(t, domain.ObsBlockSeen, offset(time.Second), nil))
		if _, ok := (ProposerMissed{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("does not match when attestation_published exists despite no block_seen (host-memory-pressure shape)", func(t *testing.T) {
		tl := timelineWith(t, mustObs(t, domain.ObsAttestationPublished, offset(2*time.Second), nil))
		if _, ok := (ProposerMissed{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})
}
