package rules

import (
	"math"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func obsWithOffset(t *testing.T, at time.Time, clockOffset time.Duration) domain.Observation {
	t.Helper()
	o, err := domain.NewObservation(domain.Observation{
		Slot: 100, Kind: domain.ObsBlockSeen, At: at, ClockOffset: clockOffset,
		ClockMeasured: true, ClockSampleAt: at, Source: domain.SourceBeaconAPI,
	})
	if err != nil {
		t.Fatalf("NewObservation: %v", err)
	}
	return o
}

func TestClockTrust(t *testing.T) {
	t.Run("matches when offset exceeds threshold", func(t *testing.T) {
		tl := timelineWith(t, obsWithOffset(t, offset(time.Second), 250*time.Millisecond))
		v, ok := ClockTrust{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Cause != domain.CauseHostClockDrift {
			t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseHostClockDrift)
		}
		if v.Confidence != domain.ConfidenceHigh {
			t.Errorf("Confidence = %q, want high", v.Confidence)
		}
	})

	t.Run("matches on negative offset beyond threshold (absolute value)", func(t *testing.T) {
		tl := timelineWith(t, obsWithOffset(t, offset(time.Second), -250*time.Millisecond))
		if _, ok := (ClockTrust{}).Evaluate(tl, defaultCfg); !ok {
			t.Fatal("want match, got no match")
		}
	})

	t.Run("does not trust minimum duration offset after absolute overflow", func(t *testing.T) {
		tl := timelineWith(t, obsWithOffset(t, offset(time.Second), time.Duration(math.MinInt64)))
		v, ok := (ClockTrust{}).Evaluate(tl, defaultCfg)
		if !ok || v.Cause != domain.CauseHostClockDrift {
			t.Fatalf("Evaluate() = %+v, %v, want clock drift", v, ok)
		}
	})

	t.Run("does not match within threshold", func(t *testing.T) {
		tl := timelineWith(t, obsWithOffset(t, offset(time.Second), 50*time.Millisecond))
		if _, ok := (ClockTrust{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("does not require local clock provenance for external baseline attributes", func(t *testing.T) {
		baseline, err := domain.NewObservation(domain.Observation{
			Slot: 100, Kind: domain.ObsNetworkBaselineSampled, At: slotStart, Source: domain.SourceXatu,
			Attrs: map[domain.AttrKey]string{
				domain.AttrBlockArrivalP50MS: "1000",
				domain.AttrBlockArrivalP90MS: "1500",
				domain.AttrSampleCount:       "50",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		tl := timelineWith(t, baseline)
		if _, ok := (ClockTrust{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("external baseline arrival time should not gate local clock trust")
		}
	})

	// An unmeasured clock is untrusted, not fine: I-9 forbids timing
	// attribution when the offset is unknown.
	t.Run("reports insufficient data when the clock was never measured", func(t *testing.T) {
		unmeasured, err := domain.NewObservation(domain.Observation{
			Slot: 100, Kind: domain.ObsBlockSeen, At: offset(time.Second), Source: domain.SourceBeaconAPI,
		})
		if err != nil {
			t.Fatalf("NewObservation: %v", err)
		}
		tl := timelineWith(t, unmeasured)
		v, ok := ClockTrust{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Cause != domain.CauseInsufficientData {
			t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseInsufficientData)
		}
		if v.Confidence != domain.ConfidenceLow {
			t.Errorf("Confidence = %q, want low", v.Confidence)
		}
	})

	t.Run("reports insufficient data when the clock sample is stale", func(t *testing.T) {
		obs := obsWithOffset(t, offset(3*time.Minute), 10*time.Millisecond)
		obs.ClockSampleAt = offset(0)
		tl := timelineWith(t, obs)
		v, ok := ClockTrust{}.Evaluate(tl, defaultCfg)
		if !ok || v.Cause != domain.CauseInsufficientData {
			t.Fatalf("Evaluate() = %+v, %v, want insufficient_data", v, ok)
		}
		if v.Confidence != domain.ConfidenceLow {
			t.Errorf("Confidence = %q, want low", v.Confidence)
		}
	})

	t.Run("reports insufficient data for an unmeasured metric sample", func(t *testing.T) {
		tl := timelineWith(t)
		tl.Samples = []domain.MetricSample{{
			At: slotStart, Component: domain.ComponentHost, Name: "host_mem_pressure_pct",
			Value: 12, Source: domain.SourceHostMetrics,
		}}
		v, ok := ClockTrust{}.Evaluate(tl, defaultCfg)
		if !ok || v.Cause != domain.CauseInsufficientData {
			t.Fatalf("Evaluate() = %+v, %v, want insufficient_data", v, ok)
		}
	})

	t.Run("uses only the latest clock provenance for each metric", func(t *testing.T) {
		tl := timelineWith(t)
		tl.Samples = []domain.MetricSample{
			{At: slotStart.Add(-time.Second), Component: domain.ComponentHost, Name: "host_mem_pressure_pct", Value: 1, Source: domain.SourceHostMetrics},
			{At: slotStart, Component: domain.ComponentHost, Name: "host_mem_pressure_pct", Value: 2, ClockMeasured: true, ClockSampleAt: slotStart, Source: domain.SourceHostMetrics},
		}
		if _, ok := (ClockTrust{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("chooses an insufficient sample deterministically", func(t *testing.T) {
		tl := timelineWith(t)
		tl.Samples = []domain.MetricSample{
			{At: slotStart, Component: domain.ComponentHost, Name: "z_metric", Value: 1, Source: domain.SourceHostMetrics},
			{At: slotStart, Component: domain.ComponentCL, Name: "a_metric", Value: 1, Source: domain.SourcePromScrape},
		}
		var want string
		for i := 0; i < 100; i++ {
			v, ok := (ClockTrust{}).Evaluate(tl, defaultCfg)
			if !ok || v.Cause != domain.CauseInsufficientData {
				t.Fatalf("Evaluate() = %+v, %v, want insufficient_data", v, ok)
			}
			if i == 0 {
				want = v.Evidence[0].Statement
			} else if v.Evidence[0].Statement != want {
				t.Fatalf("evidence changed: got %q, want %q", v.Evidence[0].Statement, want)
			}
		}
	})
}
