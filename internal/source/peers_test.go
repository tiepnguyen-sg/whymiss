package source

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

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

func TestSampleBlockTimingForSlotWaitsForGaugeCatchup(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		slot := 99
		if requests.Add(1) >= 2 {
			slot = 100
		}
		fmt.Fprintf(w, "beacon_head_slot %d\nblock_arrival_latency_milliseconds_gauge 451\n", slot)
	}))
	defer srv.Close()

	got, err := NewMetricsSampler().SampleBlockTimingForSlot(t.Context(), ConsensusPrysm, srv.URL, 100, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if got.Slot != 100 || requests.Load() != 2 {
		t.Fatalf("timing slot = %d after %d requests, want slot 100 after 2", got.Slot, requests.Load())
	}
}

func TestSampleBlockTimingForSlotRejectsAdvancedGauge(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "beacon_head_slot 101\nblock_arrival_latency_milliseconds_gauge 451\n")
	}))
	defer srv.Close()

	if _, err := NewMetricsSampler().SampleBlockTimingForSlot(t.Context(), ConsensusPrysm, srv.URL, 100, time.Now().Add(time.Second)); err == nil {
		t.Fatal("SampleBlockTimingForSlot accepted a gauge that had advanced beyond the expected slot")
	}
}

func TestSamplePeerCount_DispatchesLighthouse(t *testing.T) {
	srv := serveMetrics(t, "promscrape/testdata/lighthouse_metrics.txt")
	defer srv.Close()

	got, err := NewMetricsSampler().SamplePeerCount(context.Background(), ConsensusLighthouse, srv.URL)
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

	got, err := NewMetricsSampler().SamplePeerCount(context.Background(), ConsensusPrysm, srv.URL)
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
	if _, err := NewMetricsSampler().SamplePeerCount(context.Background(), ConsensusUnknown, "http://unused"); err == nil {
		t.Error("SamplePeerCount: want an error for an unrecognised client, got nil")
	}
}
