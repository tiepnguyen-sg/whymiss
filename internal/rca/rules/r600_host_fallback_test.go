package rules

import (
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
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
		sampledAt := offset(2 * time.Second)
		metric := hostMetric(t, "mem_pressure_pct", "35.0")
		metric.At = sampledAt
		tl := timelineWith(t, metric)
		v, ok := HostFallback{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Cause != domain.CauseHostMemoryPressure {
			t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseHostMemoryPressure)
		}
		if !v.Evidence[0].At.Equal(sampledAt) || v.Evidence[0].Source != domain.SourceHostMetrics {
			t.Errorf("evidence = %+v, want actual sample timestamp and source", v.Evidence[0])
		}
	})

	t.Run("preserves live sample provenance", func(t *testing.T) {
		tl := timelineWith(t)
		sampledAt := offset(3 * time.Second)
		tl.Samples = []domain.MetricSample{{
			At: sampledAt, Component: domain.ComponentHost, Name: "host_cpu_steal_pct",
			Value: 20, Source: domain.SourceHostMetrics,
		}}
		v, ok := HostFallback{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match")
		}
		if !v.Evidence[0].At.Equal(sampledAt) || v.Evidence[0].Source != domain.SourceHostMetrics {
			t.Errorf("evidence = %+v, want live sample provenance", v.Evidence[0])
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

	t.Run("does not attribute pressure when the duty earned every reward flag", func(t *testing.T) {
		tl := timelineWith(t,
			hostMetric(t, "mem_pressure_pct", "35.0"),
			mustObs(t, domain.ObsAttestationIncluded, offset(time.Second), map[domain.AttrKey]string{
				domain.AttrInclusionDelay: "1", domain.AttrHeadCorrect: "true", domain.AttrTargetCorrect: "true",
			}),
		)
		if _, ok := (HostFallback{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match for a healthy duty")
		}
	})
}
