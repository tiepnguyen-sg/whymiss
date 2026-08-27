package promscrape

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
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

// The sum-across-agent-labels path (SamplePrysmPeerCount's whole reason to
// scan every matching line rather than the first) has no test with more
// than one label: this devnet's real capture only ever had one peer
// connected, so only one connected_libp2p_peers{agent=...} series existed
// to capture. Hand-writing a second label's line to exercise the sum would
// be exactly the kind of invented response BUILD_PROMPT.md §8 rules out.
// Add a real multi-peer capture once this devnet (or a future one) has
// more than one peer connected at once.
