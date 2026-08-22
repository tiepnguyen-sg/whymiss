package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// Reuses promscrape's own real captured testdata rather than duplicating
// it — same devnet captures, same discipline (BUILD_PROMPT.md §8).
func serveMetrics(t *testing.T, path string) *httptest.Server {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data) //nolint:errcheck // test helper; a write failure would fail the test via a mismatched response anyway
	}))
}

func TestSamplePeerCount_DispatchesLighthouse(t *testing.T) {
	srv := serveMetrics(t, "promscrape/testdata/lighthouse_metrics.txt")
	defer srv.Close()

	got, err := SamplePeerCount(context.Background(), ConsensusLighthouse, srv.URL)
	if err != nil {
		t.Fatalf("SamplePeerCount: %v", err)
	}
	if got.Component != domain.ComponentCL {
		t.Errorf("Component = %q, want %q", got.Component, domain.ComponentCL)
	}
}

func TestSamplePeerCount_DispatchesPrysm(t *testing.T) {
	srv := serveMetrics(t, "promscrape/testdata/prysm_metrics.txt")
	defer srv.Close()

	got, err := SamplePeerCount(context.Background(), ConsensusPrysm, srv.URL)
	if err != nil {
		t.Fatalf("SamplePeerCount: %v", err)
	}
	if got.Value != 1 {
		t.Errorf("Value = %v, want 1 (the real captured reading)", got.Value)
	}
}

func TestSamplePeerCount_UnknownClientDegrades(t *testing.T) {
	// I-8: an unrecognised client is a clear error, not a guess at some
	// other client's metric format.
	if _, err := SamplePeerCount(context.Background(), ConsensusUnknown, "http://unused"); err == nil {
		t.Error("SamplePeerCount: want an error for an unrecognised client, got nil")
	}
}
