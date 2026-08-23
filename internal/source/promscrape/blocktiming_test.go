package promscrape

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSampleLighthouseBlockTiming(t *testing.T) {
	t.Parallel()
	server := serveTestdata(t, "lighthouse_metrics.txt")
	t.Cleanup(server.Close)
	got, err := New().SampleLighthouseBlockTiming(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got.Propagation != 33*time.Millisecond {
		t.Fatalf("propagation = %s, want 33ms", got.Propagation)
	}
	if got.Slot != 3910 {
		t.Fatalf("slot = %d, want 3910", got.Slot)
	}
}

func TestSampleBlockTimingRejectsImpossibleValues(t *testing.T) {
	for _, value := range []string{"NaN", "+Inf", "-1"} {
		t.Run(value, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("beacon_head_slot 10\nblock_arrival_latency_milliseconds_gauge " + value + "\n"))
			}))
			defer server.Close()
			if _, err := New().SamplePrysmBlockTiming(context.Background(), server.URL); err == nil {
				t.Fatalf("SamplePrysmBlockTiming accepted %q", value)
			}
		})
	}
}

func TestSamplePrysmBlockTiming(t *testing.T) {
	t.Parallel()
	server := serveTestdata(t, "prysm_metrics.txt")
	t.Cleanup(server.Close)
	got, err := New().SamplePrysmBlockTiming(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got.Propagation != 451*time.Millisecond {
		t.Fatalf("propagation = %s, want 451ms", got.Propagation)
	}
	if got.Slot != 0 {
		t.Fatalf("slot = %d, want 0", got.Slot)
	}
}

func TestSampleBlockTimingRequiresSlotInSameScrape(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("block_arrival_latency_milliseconds_gauge 451\n"))
	}))
	defer server.Close()
	if _, err := New().SamplePrysmBlockTiming(context.Background(), server.URL); err == nil {
		t.Fatal("SamplePrysmBlockTiming accepted an unqualified latest-value gauge")
	}
}
