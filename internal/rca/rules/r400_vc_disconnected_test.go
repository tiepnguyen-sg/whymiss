package rules

import (
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func TestVCDisconnected(t *testing.T) {
	t.Run("matches when neither published nor included exist and there is no block_seen either", func(t *testing.T) {
		tl := timelineWith(t)
		v, ok := VCDisconnected{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Cause != domain.CauseVCDisconnected {
			t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseVCDisconnected)
		}
		if v.Confidence != domain.ConfidenceHigh {
			t.Errorf("Confidence = %q, want high", v.Confidence)
		}
	})

	t.Run("matches when block_seen arrived before the deadline but nothing else exists", func(t *testing.T) {
		// A head was ready in time and the validator client still never
		// attested — genuine, direct evidence of disconnection.
		tl := timelineWith(t, mustObs(t, domain.ObsBlockSeen, offset(1*time.Second), nil))
		v, ok := VCDisconnected{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Confidence != domain.ConfidenceHigh {
			t.Errorf("Confidence = %q, want high", v.Confidence)
		}
	})

	t.Run("does not match when block_seen arrived at or after the deadline (defers to R-200)", func(t *testing.T) {
		// The block itself was severely late — propagation is the more
		// specific explanation for why nothing was ever published, not
		// VC-BN connectivity. A real corpus scenario produced exactly
		// this shape: a propagation-degraded slot whose validator client
		// gave up on the duty rather than publishing late.
		tl := timelineWith(t, mustObs(t, domain.ObsBlockSeen, offset(18*time.Second), nil))
		if _, ok := (VCDisconnected{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("does not match when attestation_published exists", func(t *testing.T) {
		tl := timelineWith(t, mustObs(t, domain.ObsAttestationPublished, offset(2*time.Second), nil))
		if _, ok := (VCDisconnected{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("does not match when attestation_included exists despite no published (cl-slow-cpu shape)", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsBlockSeen, offset(1400*time.Millisecond), nil),
			mustObs(t, domain.ObsAttestationIncluded, offset(35*time.Second), map[domain.AttrKey]string{domain.AttrInclusionDelay: "2"}),
		)
		if _, ok := (VCDisconnected{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})
}
