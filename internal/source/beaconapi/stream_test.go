package beaconapi

import (
	"bufio"
	"os"
	"strings"
	"testing"

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
