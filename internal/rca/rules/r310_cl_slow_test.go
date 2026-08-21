package rules

import (
	"testing"
	"time"

	"github.com/CHANGEME/whymiss/internal/domain"
)

func TestCLSlow(t *testing.T) {
	t.Run("matches: validation dominant, no engine_call evidence, no vc_slow shape", func(t *testing.T) {
		tl := validationDominantTL(t) // block_seen @1s, published @4s == deadline, not vc_slow shape
		v, ok := CLSlow{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Cause != domain.CauseCLSlow {
			t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseCLSlow)
		}
		if v.Confidence != domain.ConfidenceMedium {
			t.Errorf("Confidence = %q, want medium", v.Confidence)
		}
	})

	t.Run("defers to R-410 when the vc_slow timing shape is present", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsBlockSeen, offset(1*time.Second), nil),
			mustObs(t, domain.ObsAttestationPublished, offset(4500*time.Millisecond), nil),
		)
		if _, ok := (CLSlow{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match (defers to R-410), got a match")
		}
	})

	t.Run("matches via inclusion-delay elimination when validation stage is unknown (cl-slow-cpu shape)", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsBlockSeen, offset(1400*time.Millisecond), nil),
			mustObs(t, domain.ObsAttestationIncluded, offset(35*time.Second), map[domain.AttrKey]string{domain.AttrInclusionDelay: "2"}),
		)
		v, ok := CLSlow{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Cause != domain.CauseCLSlow {
			t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseCLSlow)
		}
	})

	t.Run("does not match when engine_call evidence exists (defers to R-300)", func(t *testing.T) {
		tl := validationDominantTL(t, engineCallObs(t, 2*time.Second, "500"))
		if _, ok := (CLSlow{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("does not match when inclusion delay is 1 (timely head)", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsBlockSeen, offset(1400*time.Millisecond), nil),
			mustObs(t, domain.ObsAttestationIncluded, offset(35*time.Second), map[domain.AttrKey]string{domain.AttrInclusionDelay: "1"}),
		)
		if _, ok := (CLSlow{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})
}
