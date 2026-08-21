package rules

import (
	"testing"

	"github.com/CHANGEME/whymiss/internal/domain"
)

func hostMetric(t *testing.T, metric, value string) domain.Observation {
	t.Helper()
	return mustObs(t, domain.ObsHostSampled, offset(0), map[domain.AttrKey]string{
		domain.AttrMetric: metric, domain.AttrValue: value,
	})
}

func TestHostFallback(t *testing.T) {
	t.Run("matches disk I/O pressure above threshold", func(t *testing.T) {
		tl := timelineWith(t, hostMetric(t, "iowait_pct", "45.0"))
		v, ok := HostFallback{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Cause != domain.CauseHostDiskIO {
			t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseHostDiskIO)
		}
		if v.Confidence != domain.ConfidenceMedium {
			t.Errorf("Confidence = %q, want medium", v.Confidence)
		}
	})

	t.Run("matches CPU steal above threshold", func(t *testing.T) {
		tl := timelineWith(t, hostMetric(t, "cpu_steal_pct", "20.0"))
		v, ok := HostFallback{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Cause != domain.CauseHostCPUSteal {
			t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseHostCPUSteal)
		}
	})

	t.Run("matches memory pressure above threshold", func(t *testing.T) {
		tl := timelineWith(t, hostMetric(t, "mem_pressure_pct", "35.0"))
		v, ok := HostFallback{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Cause != domain.CauseHostMemoryPressure {
			t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseHostMemoryPressure)
		}
	})

	t.Run("checks disk before CPU when both exceed threshold", func(t *testing.T) {
		tl := timelineWith(t, hostMetric(t, "iowait_pct", "45.0"), hostMetric(t, "cpu_steal_pct", "20.0"))
		v, ok := HostFallback{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Cause != domain.CauseHostDiskIO {
			t.Errorf("Cause = %q, want %q (disk checked first)", v.Cause, domain.CauseHostDiskIO)
		}
	})

	t.Run("does not match when every metric is under its threshold", func(t *testing.T) {
		tl := timelineWith(t, hostMetric(t, "iowait_pct", "1.0"), hostMetric(t, "cpu_steal_pct", "0.5"), hostMetric(t, "mem_pressure_pct", "2.0"))
		if _, ok := (HostFallback{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("does not match when no host metrics were sampled", func(t *testing.T) {
		tl := timelineWith(t)
		if _, ok := (HostFallback{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})
}
