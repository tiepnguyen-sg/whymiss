package promscrape

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/CHANGEME/whymiss/internal/domain"
)

// testdata/geth_metrics.txt is a real captured response from geth's
// /debug/metrics/prometheus endpoint against this project's devnet
// (test/e2e/kurtosis) — see BUILD_PROMPT.md §8: never hand-write a mock
// response.

func TestSampleEngineCalls(t *testing.T) {
	srv := serveTestdata(t, "geth_metrics.txt")
	defer srv.Close()

	samples, err := SampleEngineCalls(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("SampleEngineCalls: %v", err)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i].Name < samples[j].Name })

	if len(samples) != 2 {
		t.Fatalf("got %d samples, want 2: %+v", len(samples), samples)
	}

	for _, s := range samples {
		if s.Component != domain.ComponentEL {
			t.Errorf("sample %q: Component = %q, want %q", s.Name, s.Component, domain.ComponentEL)
		}
		if s.Source != domain.SourcePromScrape {
			t.Errorf("sample %q: Source = %q, want %q", s.Name, s.Source, domain.SourcePromScrape)
		}
	}

	// forkchoiceUpdated: quantile 0.5 = 2.22248e+06 ns = 2.22248 ms.
	if got := samples[0]; got.Name != MetricELForkchoiceUpdatedMS || abs(got.Value-2.22248) > 1e-6 {
		t.Errorf("samples[0] = %+v, want %s = 2.22248", got, MetricELForkchoiceUpdatedMS)
	}
	// newPayload: quantile 0.5 = 2.12666e+06 ns = 2.12666 ms.
	if got := samples[1]; got.Name != MetricELNewPayloadMS || abs(got.Value-2.12666) > 1e-6 {
		t.Errorf("samples[1] = %+v, want %s = 2.12666", got, MetricELNewPayloadMS)
	}
}

func TestSampleEngineCalls_IgnoresUnrelatedMetrics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("# TYPE engine_getblobs_available gauge\nengine_getblobs_available 0\n")) //nolint:errcheck // test helper
	}))
	defer srv.Close()

	samples, err := SampleEngineCalls(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("SampleEngineCalls: %v", err)
	}
	if len(samples) != 0 {
		t.Errorf("SampleEngineCalls = %+v, want none", samples)
	}
}

func TestSampleEngineCalls_RejectsBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := SampleEngineCalls(context.Background(), srv.URL); err == nil {
		t.Error("SampleEngineCalls error = nil, want an error for 404")
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
