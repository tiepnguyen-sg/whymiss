package beaconapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// TestPeerCount replays testdata/node_peer_count.json, captured from a live
// Kurtosis devnet Lighthouse node (test/e2e/kurtosis) at the same moment its
// Prometheus `libp2p_peers` gauge read 0. That pairing is the whole point of
// this test: the node was peered, the standardised endpoint said so, and the
// metrics surface did not.
func TestPeerCount(t *testing.T) {
	t.Parallel()
	srv := serveTestdata(t, map[string]string{"/eth/v1/node/peer_count": "node_peer_count.json"})
	defer srv.Close()

	sample, err := NewClient(srv.URL, 0).PeerCount(t.Context())
	if err != nil {
		t.Fatalf("PeerCount: %v", err)
	}
	if sample.Value != 1 {
		t.Errorf("Value = %v, want 1 connected peer", sample.Value)
	}
	if sample.Name != MetricCLPeerCount {
		t.Errorf("Name = %q, want %q", sample.Name, MetricCLPeerCount)
	}
	if sample.Component != domain.ComponentCL {
		t.Errorf("Component = %q, want %q", sample.Component, domain.ComponentCL)
	}
	if sample.Source != domain.SourceBeaconAPI {
		t.Errorf("Source = %q, want %q", sample.Source, domain.SourceBeaconAPI)
	}
	if sample.At.IsZero() {
		t.Error("At is zero; a sample must carry the instant it was taken")
	}
}

func TestPeerCountRejectsUnusableResponses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{"non-numeric", `{"data":{"connected":"many"}}`},
		{"negative", `{"data":{"connected":"-1"}}`},
		{"missing field", `{"data":{"connecting":"3"}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			if _, err := NewClient(srv.URL, 0).PeerCount(t.Context()); err == nil {
				t.Fatalf("PeerCount accepted %s", tc.body)
			}
		})
	}
}

func TestPeerCountReportsMissingEndpoint(t *testing.T) {
	t.Parallel()
	// A node that does not serve the endpoint must surface an error, not a
	// zero: a fabricated zero is what made R-200's peer corroboration vacuous
	// in the first place.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	if _, err := NewClient(srv.URL, 0).PeerCount(t.Context()); err == nil {
		t.Fatal("a missing peer_count endpoint was reported as a valid sample")
	}
}
