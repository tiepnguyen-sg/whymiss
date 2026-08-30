package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// logLines decodes the JSON lines a slog handler wrote into the buffer.
func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()

	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line is not JSON: %q: %v", line, err)
		}
		out = append(out, entry)
	}
	return out
}

func newTestLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})), buf
}

// The soak's own shape: one error, repeated every 15 seconds for hours. This is
// the test that would have failed before the throttle existed.
func TestStreamHealthCollapsesTheSameFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	health := newStreamHealth(func() time.Time { return now })
	logger, buf := newTestLogger()

	err := errors.New("connect to event stream: unexpected status 501")
	for range 240 { // one hour at the soak's measured cadence
		health.failed(logger, err)
		now = now.Add(15 * time.Second)
	}

	lines := logLines(t, buf)
	// One for the first failure, then one per 15-minute interval across the hour.
	if len(lines) > 5 {
		t.Fatalf("240 identical failures produced %d log lines, want at most 5", len(lines))
	}
	if len(lines) < 2 {
		t.Fatalf("240 failures over an hour produced %d log lines; a silent stream outage is worse than a noisy one", len(lines))
	}
	if got := lines[0]["msg"]; got != "event stream error, reconnecting" {
		t.Errorf("first line msg = %v", got)
	}
	last := lines[len(lines)-1]
	if got := last["msg"]; got != "event stream still failing, still reconnecting" {
		t.Errorf("reminder msg = %v", got)
	}
	if got, ok := last["attempts"].(float64); !ok || got < 2 {
		t.Errorf("reminder attempts = %v, want the accumulated count", last["attempts"])
	}
}

// Two failure modes in a row are two events. Collapsing them would hide the
// second, which is the one that changed.
func TestStreamHealthReportsADifferentFailureImmediately(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	health := newStreamHealth(func() time.Time { return now })
	logger, buf := newTestLogger()

	health.failed(logger, errors.New("unexpected status 501"))
	now = now.Add(time.Second)
	health.failed(logger, errors.New("unexpected status 501"))
	now = now.Add(time.Second)
	health.failed(logger, errors.New("connection refused"))

	lines := logLines(t, buf)
	if len(lines) != 2 {
		t.Fatalf("got %d log lines, want 2 (the repeat suppressed, the new error reported)", len(lines))
	}
	if got := lines[1]["error"]; got != "connection refused" {
		t.Errorf("second line error = %v, want the new failure", got)
	}
}

func TestStreamHealthReportsRecoveryOnceAndOnlyAfterAFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	health := newStreamHealth(func() time.Time { return now })
	logger, buf := newTestLogger()

	// Nothing failed, so nothing recovered.
	health.recovered(logger)
	if lines := logLines(t, buf); len(lines) != 0 {
		t.Fatalf("recovered() logged %d lines with no preceding failure", len(lines))
	}

	health.failed(logger, errors.New("unexpected status 501"))
	now = now.Add(90 * time.Second)
	health.failed(logger, errors.New("unexpected status 501"))
	now = now.Add(30 * time.Second)
	buf.Reset()

	health.recovered(logger)
	health.recovered(logger) // every subsequent observation calls this
	lines := logLines(t, buf)
	if len(lines) != 1 {
		t.Fatalf("got %d recovery lines, want exactly 1", len(lines))
	}
	if got := lines[0]["msg"]; got != "event stream recovered" {
		t.Errorf("msg = %v", got)
	}
	if got, ok := lines[0]["failed_attempts"].(float64); !ok || got != 2 {
		t.Errorf("failed_attempts = %v, want 2", lines[0]["failed_attempts"])
	}
	if got := lines[0]["was_failing_for"]; got != "2m0s" {
		t.Errorf("was_failing_for = %v, want 2m0s", got)
	}

	// After recovery the next failure is a fresh event, not a suppressed repeat.
	buf.Reset()
	health.failed(logger, errors.New("unexpected status 501"))
	if lines := logLines(t, buf); len(lines) != 1 {
		t.Fatalf("the first failure after a recovery produced %d lines, want 1", len(lines))
	}
}
