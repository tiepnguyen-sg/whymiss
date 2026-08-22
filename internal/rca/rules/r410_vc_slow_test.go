package rules

import (
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func TestVCSlow(t *testing.T) {
	t.Run("matches when block is available before the deadline but published after it", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsBlockSeen, offset(1670*time.Millisecond), nil),
			mustObs(t, domain.ObsAttestationPublished, offset(4180*time.Millisecond), nil),
		)
		v, ok := VCSlow{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Cause != domain.CauseVCSlow {
			t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseVCSlow)
		}
	})

	t.Run("does not match when attestation_published is missing", func(t *testing.T) {
		tl := timelineWith(t, mustObs(t, domain.ObsBlockSeen, offset(1670*time.Millisecond), nil))
		if _, ok := (VCSlow{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("does not match when block_seen is missing", func(t *testing.T) {
		tl := timelineWith(t, mustObs(t, domain.ObsAttestationPublished, offset(4180*time.Millisecond), nil))
		if _, ok := (VCSlow{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("does not match when block_seen is itself after the deadline", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsBlockSeen, offset(5*time.Second), nil),
			mustObs(t, domain.ObsAttestationPublished, offset(5200*time.Millisecond), nil),
		)
		if _, ok := (VCSlow{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("does not match when published is itself before the deadline", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsBlockSeen, offset(1*time.Second), nil),
			mustObs(t, domain.ObsAttestationPublished, offset(3900*time.Millisecond), nil),
		)
		if _, ok := (VCSlow{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})
}
