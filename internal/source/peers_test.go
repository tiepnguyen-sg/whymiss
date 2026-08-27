package source

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

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
