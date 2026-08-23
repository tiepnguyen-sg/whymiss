package promscrape

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func serveTestdata(t *testing.T, file string) *httptest.Server {
	t.Helper()
	data, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatalf("read testdata/%s: %v", file, err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data) //nolint:errcheck // test helper; a write failure would fail the test via a mismatched response anyway
	}))
}

func TestMetricsHTTPBounds(t *testing.T) {
	scraper := New()
	transport, ok := scraper.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", scraper.httpClient.Transport)
	}
	if transport.MaxConnsPerHost != maxMetricConns {
		t.Fatalf("MaxConnsPerHost = %d, want %d", transport.MaxConnsPerHost, maxMetricConns)
	}
	if transport.Proxy != nil {
		t.Fatal("metrics transport honors ambient proxy settings; only explicitly configured egress is permitted")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(maxMetricsBodyBytes+1))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	if _, err := New().fetchMetricsLines(context.Background(), srv.URL); err == nil {
		t.Fatal("fetchMetricsLines: want oversized response error, got nil")
	}
}

func TestMetricsClientDoesNotFollowRedirects(t *testing.T) {
	redirected := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected = true }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()

	if _, err := New().fetchMetricsLines(context.Background(), source.URL); err == nil {
		t.Fatal("fetchMetricsLines followed or accepted a redirect")
	}
	if redirected {
		t.Fatal("metrics client followed a redirect outside the configured endpoint")
	}
}

func TestPeerCountRejectsImpossibleValues(t *testing.T) {
	for _, value := range []string{"NaN", "+Inf", "-1"} {
		t.Run(value, func(t *testing.T) {
			if _, err := buildPeerCountSample(value); err == nil {
				t.Fatalf("buildPeerCountSample(%q) accepted an impossible peer count", value)
			}
		})
	}
}

// testdata/lighthouse_metrics.txt is a real, unmodified /metrics response
// captured against a live devnet Lighthouse node (BUILD_PROMPT.md §8) —
// libp2p_peers read 0 at capture time (right after the devnet VM restarted
// and peers had not yet reconnected), which is itself a real, valid value
// to parse, not a placeholder.
func TestSampleLighthousePeerCount(t *testing.T) {
	srv := serveTestdata(t, "lighthouse_metrics.txt")
	defer srv.Close()

	got, err := New().SampleLighthousePeerCount(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("SampleLighthousePeerCount: %v", err)
	}
	if got.Name != MetricCLPeerCount {
		t.Errorf("Name = %q, want %q", got.Name, MetricCLPeerCount)
	}
	if got.Component != domain.ComponentCL {
		t.Errorf("Component = %q, want %q", got.Component, domain.ComponentCL)
	}
	if got.Value != 0 {
		t.Errorf("Value = %v, want 0 (the real captured reading)", got.Value)
	}
}

// testdata/prysm_metrics.txt is a real, unmodified /metrics response
// captured against a live devnet Prysm node: one series,
// connected_libp2p_peers{agent="lighthouse"} 1 — a single connected peer.
func TestSamplePrysmPeerCount(t *testing.T) {
	srv := serveTestdata(t, "prysm_metrics.txt")
	defer srv.Close()

	got, err := New().SamplePrysmPeerCount(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("SamplePrysmPeerCount: %v", err)
	}
	if got.Name != MetricCLPeerCount {
		t.Errorf("Name = %q, want %q", got.Name, MetricCLPeerCount)
	}
	if got.Value != 1 {
		t.Errorf("Value = %v, want 1 (the real captured connected_libp2p_peers{agent=\"lighthouse\"} reading)", got.Value)
	}
}

func TestSampleLighthousePeerCount_MetricAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("# TYPE unrelated_metric gauge\nunrelated_metric 1\n")) //nolint:errcheck // test helper
	}))
	defer srv.Close()

	if _, err := New().SampleLighthousePeerCount(context.Background(), srv.URL); err == nil {
		t.Error("SampleLighthousePeerCount: want an error when libp2p_peers is absent, got nil")
	}
}

// The sum-across-agent-labels path (SamplePrysmPeerCount's whole reason to
// scan every matching line rather than the first) has no test with more
// than one label: this devnet's real capture only ever had one peer
// connected, so only one connected_libp2p_peers{agent=...} series existed
// to capture. Hand-writing a second label's line to exercise the sum would
// be exactly the kind of invented response BUILD_PROMPT.md §8 rules out.
// Add a real multi-peer capture once this devnet (or a future one) has
// more than one peer connected at once.
