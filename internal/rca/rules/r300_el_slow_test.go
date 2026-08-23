package rules

import (
	"strconv"
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
	obs = append(obs,
		mustObs(t, domain.ObsHeadUpdated, offset(5*time.Second), nil),
		mustObs(t, domain.ObsAttestationPublished, offset(5500*time.Millisecond), nil),
	)
	return timelineWith(t, obs...)
}

func engineCallObs(t *testing.T, at time.Duration, method, durationMS string) domain.Observation {
	t.Helper()
	return mustObs(t, domain.ObsEngineCall, offset(at), map[domain.AttrKey]string{
		domain.AttrEngineMethod: method,
		domain.AttrDurationMS:   durationMS,
		domain.AttrSampleCount:  "1",
	})
}

func engineCallPair(t *testing.T, at time.Duration, totalMS string) []domain.Observation {
	t.Helper()
	total, err := strconv.ParseFloat(totalMS, 64)
	if err != nil {
		t.Fatal(err)
	}
	half := strconv.FormatFloat(total/2, 'f', -1, 64)
	return []domain.Observation{
		engineCallObs(t, at, domain.EngineMethodNewPayload, half),
		engineCallObs(t, at+time.Nanosecond, domain.EngineMethodForkchoiceUpdated, half),
	}
}

func validationDominantWithEngine(t *testing.T, totalMS string, extra ...domain.Observation) domain.Timeline {
	t.Helper()
	extra = append(extra, engineCallPair(t, 2*time.Second, totalMS)...)
	return validationDominantTL(t, extra...)
}

func withEngineP99(t *testing.T, tl domain.Timeline, value float64) domain.Timeline {
	t.Helper()
	tl.Samples = []domain.MetricSample{{
		At: slotStart, Component: domain.ComponentEL, Name: metricELEngineCallsP99MS,
		Value: value, Source: domain.SourceDerived,
	}}
	out, err := domain.NewTimeline(tl)
	if err != nil {
		t.Fatalf("NewTimeline: %v", err)
	}
	return out
}

func TestELSlow(t *testing.T) {
	t.Run("accepts multiple exact calls of one method in the head window", func(t *testing.T) {
		calls := []domain.Observation{
			mustObs(t, domain.ObsEngineCall, offset(2*time.Second), map[domain.AttrKey]string{
				domain.AttrEngineMethod: domain.EngineMethodNewPayload,
				domain.AttrDurationMS:   "1500",
				domain.AttrSampleCount:  "1",
			}),
			mustObs(t, domain.ObsEngineCall, offset(2*time.Second+time.Nanosecond), map[domain.AttrKey]string{
				domain.AttrEngineMethod: domain.EngineMethodForkchoiceUpdated,
				domain.AttrDurationMS:   "1000",
				domain.AttrSampleCount:  "2",
			}),
		}
		tl := withEngineP99(t, validationDominantTL(t, calls...), 500)
		v, ok := (ELSlow{}).Evaluate(tl, defaultCfg)
		if !ok || v.Cause != domain.CauseELSlow {
			t.Fatalf("verdict=%+v matched=%t, want local.el_slow", v, ok)
		}
	})

	t.Run("matches at medium confidence with engine_call evidence and no sub-cause signal", func(t *testing.T) {
		tl := withEngineP99(t, validationDominantWithEngine(t, "2500"), 500)
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

	t.Run("does not infer disk saturation from host-wide PSI", func(t *testing.T) {
		hostSample := mustObs(t, domain.ObsHostSampled, offset(1500*time.Millisecond), map[domain.AttrKey]string{
			domain.AttrMetric: "iowait_pct", domain.AttrValue: "45.0",
		})
		tl := withEngineP99(t, validationDominantWithEngine(t, "2500", hostSample), 500)
		v, ok := ELSlow{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.SubCause != "" {
			t.Errorf("SubCause = %q, want empty without EL-specific disk evidence", v.SubCause)
		}
		if v.Confidence != domain.ConfidenceMedium {
			t.Errorf("Confidence = %q, want medium", v.Confidence)
		}
	})

	t.Run("does not match without engine_call evidence", func(t *testing.T) {
		tl := withEngineP99(t, validationDominantTL(t), 500)
		if _, ok := (ELSlow{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("does not treat duplicate Engine calls as an exact per-slot sample", func(t *testing.T) {
		extra := append(engineCallPair(t, 2*time.Second, "2500"),
			engineCallObs(t, 2*time.Second+2*time.Nanosecond, domain.EngineMethodNewPayload, "1"))
		tl := withEngineP99(t, validationDominantTL(t, extra...), 500)
		if _, ok := (ELSlow{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match for an ambiguous Engine counter window")
		}
	})

	t.Run("does not match without a rolling p99 baseline", func(t *testing.T) {
		tl := validationDominantWithEngine(t, "2500")
		if _, ok := (ELSlow{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("does not match below the three-times-p99 threshold", func(t *testing.T) {
		tl := withEngineP99(t, validationDominantWithEngine(t, "2500"), 1000)
		if _, ok := (ELSlow{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("does not match when Engine time is under half of validation", func(t *testing.T) {
		tl := withEngineP99(t, validationDominantWithEngine(t, "500"), 100)
		if _, ok := (ELSlow{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("does not match when propagation is dominant instead of validation", func(t *testing.T) {
		obs := []domain.Observation{mustObs(t, domain.ObsBlockSeen, offset(5*time.Second), nil)}
		obs = append(obs, engineCallPair(t, 5100*time.Millisecond, "50")...)
		obs = append(obs, mustObs(t, domain.ObsAttestationPublished, offset(5200*time.Millisecond), nil))
		tl := withEngineP99(t, timelineWith(t, obs...), 10)
		if _, ok := (ELSlow{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})
}
