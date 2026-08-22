package rules

import (
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func validationDominantTL(t *testing.T, extra ...domain.Observation) domain.Timeline {
	t.Helper()
	obs := []domain.Observation{
		mustObs(t, domain.ObsBlockSeen, offset(1*time.Second), nil),
	}
	obs = append(obs, extra...)
	obs = append(obs, mustObs(t, domain.ObsAttestationPublished, offset(4*time.Second), nil))
	return timelineWith(t, obs...)
}

func engineCallObs(t *testing.T, at time.Duration, durationMS string) domain.Observation {
	t.Helper()
	return mustObs(t, domain.ObsEngineCall, offset(at), map[domain.AttrKey]string{
		domain.AttrEngineMethod: "engine_newPayloadV3",
		domain.AttrDurationMS:   durationMS,
	})
}

func TestELSlow(t *testing.T) {
	t.Run("matches at medium confidence with engine_call evidence and no sub-cause signal", func(t *testing.T) {
		tl := validationDominantTL(t, engineCallObs(t, 2*time.Second, "2500"))
		v, ok := ELSlow{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Cause != domain.CauseELSlow {
			t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseELSlow)
		}
		if v.Confidence != domain.ConfidenceMedium {
			t.Errorf("Confidence = %q, want medium", v.Confidence)
		}
		if v.SubCause != "" {
			t.Errorf("SubCause = %q, want empty", v.SubCause)
		}
	})

	t.Run("matches at high confidence with disk_saturation sub-cause", func(t *testing.T) {
		hostSample := mustObs(t, domain.ObsHostSampled, offset(1500*time.Millisecond), map[domain.AttrKey]string{
			domain.AttrMetric: "iowait_pct", domain.AttrValue: "45.0",
		})
		tl := validationDominantTL(t, hostSample, engineCallObs(t, 2*time.Second, "2500"))
		v, ok := ELSlow{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.SubCause != domain.CauseELSlowDiskSaturation {
			t.Errorf("SubCause = %q, want %q", v.SubCause, domain.CauseELSlowDiskSaturation)
		}
		if v.Confidence != domain.ConfidenceHigh {
			t.Errorf("Confidence = %q, want high", v.Confidence)
		}
	})

	t.Run("does not match without engine_call evidence", func(t *testing.T) {
		tl := validationDominantTL(t)
		if _, ok := (ELSlow{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("does not match when propagation is dominant instead of validation", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsBlockSeen, offset(5*time.Second), nil),
			engineCallObs(t, 5100*time.Millisecond, "50"),
			mustObs(t, domain.ObsAttestationPublished, offset(5200*time.Millisecond), nil),
		)
		if _, ok := (ELSlow{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})
}
