package promscrape

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/CHANGEME/whymiss/internal/domain"
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

// testdata/lighthouse_metrics.txt is a real, unmodified /metrics response
// captured against a live devnet Lighthouse node (BUILD_PROMPT.md §8) —
// libp2p_peers read 0 at capture time (right after the devnet VM restarted
// and peers had not yet reconnected), which is itself a real, valid value
// to parse, not a placeholder.
func TestSampleLighthousePeerCount(t *testing.T) {
	srv := serveTestdata(t, "lighthouse_metrics.txt")
	defer srv.Close()

	got, err := SampleLighthousePeerCount(context.Background(), srv.URL)
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

	got, err := SamplePrysmPeerCount(context.Background(), srv.URL)
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

	if _, err := SampleLighthousePeerCount(context.Background(), srv.URL); err == nil {
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
