package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/source"
	"github.com/tiepnguyen-sg/whymiss/internal/source/beaconapi"
)

func TestWaitHeadTiming(t *testing.T) {
	t.Parallel()

	t.Run("returns a buffered result after observer exit", func(t *testing.T) {
		t.Parallel()
		results := make(chan headTimingResult, 1)
		done := make(chan struct{})
		results <- headTimingResult{}
		close(done)
		if _, err := waitHeadTiming(context.Background(), results, done); err != nil {
			t.Fatalf("waitHeadTiming: %v", err)
		}
	})

	t.Run("rejects observer exit without a result", func(t *testing.T) {
		t.Parallel()
		results := make(chan headTimingResult, 1)
		done := make(chan struct{})
		close(done)
		if _, err := waitHeadTiming(context.Background(), results, done); err == nil {
			t.Fatal("waitHeadTiming error = nil, want missing-result error")
		}
	})

	t.Run("honors cancellation", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := waitHeadTiming(ctx, make(chan headTimingResult), make(chan struct{})); err == nil {
			t.Fatal("waitHeadTiming error = nil, want context error")
		}
	})
}

// headSource serves the two endpoints observeHeadForSlot reads, so a test can
// make them disagree the way a faulted node makes them disagree.
type headSource struct {
	// restHeadSlot is the slot /eth/v1/beacon/headers/head reports, or 0 for a
	// node answering 404.
	restHeadSlot uint64
	// streamSlots are announced as head events on /eth/v1/events, in order.
	streamSlots []uint64
	// gate, when non-nil, holds the head events back until it is closed, so a
	// test can make the head arrive after some other channel has moved on.
	gate <-chan struct{}
}

func (h headSource) serve(t *testing.T) string {
	t.Helper()
	root := "0x" + strings.Repeat("ab", 32)
	mux := http.NewServeMux()
	mux.HandleFunc("/eth/v1/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("event stream response is not flushable")
			return
		}
		if h.gate != nil {
			select {
			case <-h.gate:
			case <-r.Context().Done():
				return
			}
		}
		for _, slot := range h.streamSlots {
			fmt.Fprintf(w, "event: head\ndata: {\"slot\":\"%d\",\"block\":%q,\"execution_optimistic\":false}\n\n", slot, root)
			flusher.Flush()
		}
		// A real node holds the connection open after announcing; closing it
		// here would make Stream reconnect and replay, which no node does.
		<-r.Context().Done()
	})
	mux.HandleFunc("/eth/v1/beacon/headers/head", func(w http.ResponseWriter, _ *http.Request) {
		if h.restHeadSlot == 0 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"execution_optimistic":false,"data":{"root":%q,"canonical":true,`+
			`"header":{"message":{"slot":"%d","proposer_index":"7"}}}}`, root, h.restHeadSlot)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server.URL
}

// TestObserveHeadForSlotUsesTheStreamThePollCannotSee is the regression test for
// the race that lost every run of cl-slow-cpu-lighthouse and
// p2p-ambiguous-no-baseline-prysm: the watched node's head advances past the
// duty slot between two 200ms header samples, so the poll can never report it
// and the record came out with no stage timed and no cause. The REST endpoint
// here is permanently past the slot, exactly as it is for a poll that arrives
// late, while the stream still announces it.
func TestObserveHeadForSlotUsesTheStreamThePollCannotSee(t *testing.T) {
	t.Parallel()
	const slot = 2464
	url := headSource{restHeadSlot: slot + 1, streamSlots: []uint64{slot}}.serve(t)
	client := beaconapi.NewClient(url, time.Millisecond)

	head, err := observeHeadForSlot(context.Background(), client, slot, time.Now().Add(5*time.Second))
	if err != nil {
		t.Fatalf("observeHeadForSlot: %v", err)
	}
	if head.Slot != slot {
		t.Errorf("head slot = %d, want %d", head.Slot, slot)
	}
	if head.Kind != domain.ObsHeadUpdated {
		t.Errorf("head kind = %q, want %q", head.Kind, domain.ObsHeadUpdated)
	}
}

// A slot the stream skips over is a proven skip rather than a missed
// measurement, and the error has to say which — the whole reason the stream is
// worth reading is that it can tell them apart.
func TestObserveHeadForSlotReportsAProvenSkip(t *testing.T) {
	t.Parallel()
	const slot = 2464
	url := headSource{restHeadSlot: slot + 1, streamSlots: []uint64{slot + 1}}.serve(t)
	client := beaconapi.NewClient(url, time.Millisecond)

	_, err := observeHeadForSlot(context.Background(), client, slot, time.Now().Add(5*time.Second))
	if err == nil {
		t.Fatal("observeHeadForSlot error = nil, want a skip report")
	}
	if !strings.Contains(err.Error(), "skipped it") {
		t.Errorf("error does not report a proven skip: %v", err)
	}
}

// With neither channel delivering, the deadline ends the call and the message
// must not claim a skip it cannot prove.
func TestObserveHeadForSlotReportsSilenceWithoutClaimingASkip(t *testing.T) {
	t.Parallel()
	const slot = 2464
	url := headSource{}.serve(t)
	client := beaconapi.NewClient(url, time.Millisecond)

	_, err := observeHeadForSlot(context.Background(), client, slot, time.Now().Add(300*time.Millisecond))
	if err == nil {
		t.Fatal("observeHeadForSlot error = nil, want a not-observed report")
	}
	if strings.Contains(err.Error(), "skipped") {
		t.Errorf("error claims a skip neither channel observed: %v", err)
	}
	if !strings.Contains(err.Error(), "neither the event stream nor the header poll") {
		t.Errorf("unexpected error: %v", err)
	}
}

// climbingGauge serves a Prometheus block-timing gauge whose beacon_head_slot
// advances on a wall clock started at serve(). Advancing by scrape count cannot
// express what these tests are about — a gauge stepping past the slot while
// nobody is watching it — because the observer under test stops scraping the
// moment it gets its answer, which would freeze the schedule it is being judged
// against.
type climbingGauge struct {
	// steps are the slots the gauge reports, each held for stepFor.
	steps         []uint64
	stepFor       time.Duration
	propagationMS int

	start time.Time
}

// slotAt reports the gauge's value at d past serve(), holding the last step
// forever so the gauge never rewinds.
func (g *climbingGauge) slotAt(d time.Duration) uint64 {
	i := int(d / g.stepFor)
	if i >= len(g.steps) {
		i = len(g.steps) - 1
	}
	return g.steps[i]
}

func (g *climbingGauge) serve(t *testing.T) string {
	t.Helper()
	g.start = time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "block_arrival_latency_milliseconds_gauge %d\nbeacon_head_slot %d\n",
			g.propagationMS, g.slotAt(time.Since(g.start)))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// afterGaugeMovedOn returns a gate that opens once the gauge has stepped past
// every slot the test cares about, so the head is announced only after the
// window a scrape-after-head observer could have used has closed.
func (g *climbingGauge) afterGaugeMovedOn(t *testing.T) <-chan struct{} {
	t.Helper()
	gate := make(chan struct{})
	go func() {
		defer close(gate)
		time.Sleep(time.Duration(len(g.steps))*g.stepFor - time.Since(g.start))
	}()
	return gate
}

// TestObserveHeadTimingWatchesTheGaugeThroughItsTransition is the regression
// test for the second half of the observer race, the one that cost
// p2p-degraded-prysm-r04 its arrival while r05 — same recipe, same devnet —
// kept it. The gauge holds slot-1 for a second, the duty slot for a second,
// then slot+1 for good, and the head is not announced until after it has moved
// past. Sampling the gauge once the head has arrived can therefore only ever
// read slot+1 and fail; watching it from before the slot catches the one-second
// window the arrival is visible in.
func TestObserveHeadTimingWatchesTheGaugeThroughItsTransition(t *testing.T) {
	t.Parallel()
	const slot = 2574
	gauge := &climbingGauge{
		steps:         []uint64{slot - 1, slot, slot + 1},
		stepFor:       time.Second,
		propagationMS: 6190,
	}
	metricsURL := gauge.serve(t)
	beaconURL := headSource{
		restHeadSlot: slot + 1,
		streamSlots:  []uint64{slot},
		gate:         gauge.afterGaugeMovedOn(t),
	}.serve(t)

	result := make(chan headTimingResult, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	observeHeadTiming(ctx, source.NewMetricsSampler(), beaconapi.NewClient(beaconURL, time.Millisecond),
		source.ConsensusPrysm, metricsURL, slot, time.Now().Add(25*time.Second), false, result)

	measured := <-result
	if measured.HeadErr != nil {
		t.Fatalf("head observation lost: %v", measured.HeadErr)
	}
	if measured.TimingErr != nil {
		t.Fatalf("block timing lost: %v", measured.TimingErr)
	}
	if measured.Timing.Slot != slot {
		t.Errorf("timing slot = %d, want %d", measured.Timing.Slot, slot)
	}
	if want := 6190 * time.Millisecond; measured.Timing.Propagation != want {
		t.Errorf("propagation = %s, want %s", measured.Timing.Propagation, want)
	}
}

// A slot the gauge steps straight over is still reported as skipped rather than
// measured, so widening the watch does not turn a skip into a false arrival.
func TestObserveHeadTimingStillReportsAGaugeThatSkippedTheSlot(t *testing.T) {
	t.Parallel()
	const slot = 2574
	gauge := &climbingGauge{
		steps:         []uint64{slot - 1, slot + 1},
		stepFor:       time.Second,
		propagationMS: 400,
	}
	metricsURL := gauge.serve(t)
	beaconURL := headSource{
		restHeadSlot: slot + 1,
		streamSlots:  []uint64{slot},
		gate:         gauge.afterGaugeMovedOn(t),
	}.serve(t)

	result := make(chan headTimingResult, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	observeHeadTiming(ctx, source.NewMetricsSampler(), beaconapi.NewClient(beaconURL, time.Millisecond),
		source.ConsensusPrysm, metricsURL, slot, time.Now().Add(25*time.Second), false, result)

	measured := <-result
	if measured.TimingErr == nil {
		t.Fatalf("block timing error = nil, want an advanced-past-slot report (propagation %s)", measured.Timing.Propagation)
	}
	if !strings.Contains(measured.TimingErr.Error(), "advanced to slot") {
		t.Errorf("unexpected timing error: %v", measured.TimingErr)
	}
}

// TestWatchBlockTimingForSlotToleratesAFailedScrape covers the regression the
// wider watch window would otherwise have introduced. Watching the gauge from
// before the slot means many more scrapes than the old sample-after-head did, so
// keeping source.SampleBlockTimingForSlot's abort-on-first-error would have made
// a CPU-starved node's run fail more often rather than less —
// cl-slow-cpu-lighthouse at 5% of a core had already lost r05 that way.
func TestWatchBlockTimingForSlotToleratesAFailedScrape(t *testing.T) {
	t.Parallel()
	const slot = 2574
	var scrapes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// The first two scrapes fail the way a client too starved to serve its
		// own metrics endpoint fails; the third answers with the slot.
		if scrapes.Add(1) <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintf(w, "block_arrival_latency_milliseconds_gauge 4200\nbeacon_head_slot %d\n", slot)
	}))
	defer server.Close()

	timing, err := watchBlockTimingForSlot(context.Background(), source.NewMetricsSampler(),
		source.ConsensusPrysm, server.URL, slot, time.Now().Add(10*time.Second))
	if err != nil {
		t.Fatalf("watchBlockTimingForSlot: %v", err)
	}
	if timing.Slot != slot {
		t.Errorf("timing slot = %d, want %d", timing.Slot, slot)
	}
	if want := 4200 * time.Millisecond; timing.Propagation != want {
		t.Errorf("propagation = %s, want %s", timing.Propagation, want)
	}
	if got := scrapes.Load(); got < 3 {
		t.Errorf("scrapes = %d, want the watch to have retried past the two failures", got)
	}
}

// A gauge that never becomes readable must say so, rather than reporting the
// "remained at slot" message that describes a working endpoint whose node never
// produced a block. The two call for different fixes.
func TestWatchBlockTimingForSlotNamesAnUnreadableGauge(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := watchBlockTimingForSlot(context.Background(), source.NewMetricsSampler(),
		source.ConsensusPrysm, server.URL, 2574, time.Now().Add(600*time.Millisecond))
	if err == nil {
		t.Fatal("watchBlockTimingForSlot error = nil, want an unreadable-gauge report")
	}
	if !strings.Contains(err.Error(), "never readable") {
		t.Errorf("error does not name the unreadable gauge: %v", err)
	}
}
