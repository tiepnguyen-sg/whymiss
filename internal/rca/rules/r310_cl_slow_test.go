package rules

import (
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func TestCLSlow(t *testing.T) {
	t.Run("matches when validation dominates and Engine time is minor", func(t *testing.T) {
		tl := validationDominantWithEngine(t, "100")
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
		if len(v.Evidence) != 4 {
			t.Fatalf("Evidence count = %d, want validation, two Engine methods, and residual comparison", len(v.Evidence))
		}
		for i, item := range v.Evidence {
			if item.Comparison == nil {
				t.Fatalf("Evidence %d has no machine-checkable comparison: %+v", i, item)
			}
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

	t.Run("does not infer CL slowness from inclusion delay without validation timing", func(t *testing.T) {
		obs := []domain.Observation{mustObs(t, domain.ObsBlockSeen, offset(1400*time.Millisecond), nil)}
		obs = append(obs, engineCallPair(t, 2*time.Second, "100")...)
		obs = append(obs, mustObs(t, domain.ObsAttestationIncluded, offset(35*time.Second), map[domain.AttrKey]string{domain.AttrInclusionDelay: "2"}))
		tl := timelineWith(t, obs...)
		if _, ok := (CLSlow{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match without validation timing")
		}
	})

	t.Run("does not match when Engine time consumes at least half of validation", func(t *testing.T) {
		tl := validationDominantWithEngine(t, "2500")
		if _, ok := (CLSlow{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("does not match without engine_call evidence", func(t *testing.T) {
		tl := validationDominantTL(t)
		if _, ok := (CLSlow{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("does not treat duplicate Engine calls as an exact per-slot sample", func(t *testing.T) {
		extra := append(engineCallPair(t, 2*time.Second, "100"),
			engineCallObs(t, 2*time.Second+2*time.Nanosecond, domain.EngineMethodForkchoiceUpdated, "1"))
		tl := validationDominantTL(t, extra...)
		if _, ok := (CLSlow{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match for an ambiguous Engine counter window")
		}
	})

	t.Run("does not match when inclusion delay is 1 (timely head)", func(t *testing.T) {
		obs := []domain.Observation{mustObs(t, domain.ObsBlockSeen, offset(1400*time.Millisecond), nil)}
		obs = append(obs, engineCallPair(t, 2*time.Second, "100")...)
		obs = append(obs, mustObs(t, domain.ObsAttestationIncluded, offset(35*time.Second), map[domain.AttrKey]string{domain.AttrInclusionDelay: "1"}))
		tl := timelineWith(t, obs...)
		if _, ok := (CLSlow{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})
}
