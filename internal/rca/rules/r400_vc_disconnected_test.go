package rules

import (
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func TestVCDisconnected(t *testing.T) {
	t.Run("does not match without evidence that the beacon node had a timely head", func(t *testing.T) {
		tl := timelineWith(t)
		if _, ok := (VCDisconnected{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("matches when block and head arrived before the deadline but no attestation exists", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsBlockSeen, offset(1*time.Second), nil),
			mustObs(t, domain.ObsHeadUpdated, offset(2*time.Second), nil),
		)
		v, ok := VCDisconnected{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Cause != domain.CauseVCDisconnected {
			t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseVCDisconnected)
		}
		if v.Confidence != domain.ConfidenceMedium {
			t.Errorf("Confidence = %q, want medium", v.Confidence)
		}
		completed, ok := tl.First(domain.ObsCollectionCompleted)
		if !ok {
			t.Fatal("test timeline has no collection_completed observation")
		}
		if got := v.Evidence[0].At; !got.Equal(completed.At) {
			t.Errorf("Evidence[0].At = %s, want collection_completed timestamp %s", got, completed.At)
		}
	})

	t.Run("does not blame the validator client when head was never updated", func(t *testing.T) {
		tl := timelineWith(t, mustObs(t, domain.ObsBlockSeen, offset(time.Second), nil))
		if _, ok := (VCDisconnected{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match without a validated head")
		}
	})

	t.Run("does not blame the validator client when head updated after the deadline", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsBlockSeen, offset(time.Second), nil),
			mustObs(t, domain.ObsHeadUpdated, offset(5*time.Second), nil),
		)
		if _, ok := (VCDisconnected{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match for a late beacon-node head")
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
		tl := timelineWith(t,
			mustObs(t, domain.ObsBlockSeen, offset(time.Second), nil),
			mustObs(t, domain.ObsHeadUpdated, offset(1500*time.Millisecond), nil),
			mustObs(t, domain.ObsAttestationPublished, offset(2*time.Second), nil),
		)
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
