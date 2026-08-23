package beaconapi

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// TestParseEvent_Head is captured against a real devnet's SSE event stream
// (curl -N .../eth/v1/events?topics=head,chain_reorg) — see
// testdata/sse_stream.txt. The real stream uses no space after the colon
// ("event:head", not "event: head"), which is why parseEvent's caller in
// streamOnce trims the line rather than assuming a fixed prefix width.
func TestParseEvent_Head(t *testing.T) {
	f, err := os.Open("testdata/sse_stream.txt")
	if err != nil {
		t.Fatalf("open testdata/sse_stream.txt: %v", err)
	}
	defer f.Close() //nolint:errcheck // read-only test fixture

	var eventType, data string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
	if eventType == "" || data == "" {
		t.Fatalf("testdata/sse_stream.txt did not contain a parseable event (got eventType=%q)", eventType)
	}
	if eventType != "head" {
		t.Fatalf("testdata/sse_stream.txt's captured event is %q, want \"head\" — update this test if the fixture changed", eventType)
	}

	obs, ok, err := parseEvent(eventType, data)
	if err != nil {
		t.Fatalf("parseEvent(%q, ...): %v", eventType, err)
	}
	if !ok {
		t.Fatal("parseEvent: want ok=true for a head event, got false")
	}
	if obs.Kind != domain.ObsHeadUpdated {
		t.Errorf("Kind = %q, want %q", obs.Kind, domain.ObsHeadUpdated)
	}
	if obs.Slot != 3641 {
		t.Errorf("Slot = %d, want 3641", obs.Slot)
	}
	if got := obs.Attrs[domain.AttrBlockRoot]; got != "0xcfdff47b5b03cfe74d933b1e352c300b1e4c0aeabb98373d7d113070b1609214" {
		t.Errorf("block_root = %q, want the real captured root", got)
	}
}

// chain_reorg parsing has no test here: reorgs did not occur during this
// pass's capture session against a healthy two-node devnet, and
// BUILD_PROMPT.md §8 forbids hand-writing a substitute payload. Add
// TestParseEvent_ChainReorg once a real reorg event has actually been
// captured — deliberately inducing one (e.g. two competing proposers) is a
// real task, not a gap to paper over with an invented fixture.

func TestParseEvent_UnsubscribedTopic(t *testing.T) {
	_, ok, err := parseEvent("finalized_checkpoint", `{}`)
	if err != nil {
		t.Fatalf("parseEvent: %v", err)
	}
	if ok {
		t.Fatal("parseEvent: want ok=false for a topic this package didn't subscribe to")
	}
}

func TestParseEventRejectsMalformedHeadRoot(t *testing.T) {
	_, ok, err := parseEvent("head", `{"slot":"3641","block":"not-a-root","execution_optimistic":false}`)
	if err == nil {
		t.Fatal("parseEvent: want malformed root error")
	}
	if ok {
		t.Fatal("parseEvent: malformed head must not produce an observation")
	}
}

func TestParseEventRejectsHeadWithoutOptimisticStatus(t *testing.T) {
	_, ok, err := parseEvent("head", `{"slot":"3641","block":"0xcfdff47b5b03cfe74d933b1e352c300b1e4c0aeabb98373d7d113070b1609214"}`)
	if err == nil {
		t.Fatal("parseEvent: want missing execution_optimistic error")
	}
	if ok {
		t.Fatal("parseEvent: unverifiable head must not produce an observation")
	}
}

func TestParseEventSuppressesOptimisticHead(t *testing.T) {
	_, ok, err := parseEvent("head", `{"slot":"3641","block":"0xcfdff47b5b03cfe74d933b1e352c300b1e4c0aeabb98373d7d113070b1609214","execution_optimistic":true}`)
	if err != nil {
		t.Fatalf("parseEvent: %v", err)
	}
	if ok {
		t.Fatal("parseEvent: optimistic head must not produce a validated-head observation")
	}
}

func TestStreamOnceTimesOutStalledConnection(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, 0)
	client.streamIdleTimeout = 25 * time.Millisecond
	started := time.Now()
	delivered, err := client.streamOnce(context.Background(), make(chan domain.Observation))
	if err == nil {
		t.Fatal("streamOnce: want timeout error")
	}
	if delivered {
		t.Fatal("streamOnce: reported an event on a stalled stream")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("streamOnce took %s, want a bounded request", elapsed)
	}
}
