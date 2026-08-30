package app

import (
	"log/slog"
	"sync"
	"time"
)

// streamReminderInterval is how often a still-failing event stream is repeated
// in the log. Chosen against the measurement that prompted it: at one reconnect
// roughly every 15 seconds, a 72-hour outage produces 17,275 lines unthrottled
// and 288 at this interval — often enough that a reader scrolling the log cannot
// believe the stream is healthy, rare enough that it does not bury anything.
const streamReminderInterval = 15 * time.Minute

// streamHealth turns a repeating event-stream failure into one line, a periodic
// reminder, and a line when it clears.
//
// The 72-hour release soak produced 18,006 log lines of which 17,275 were the
// same warning: the gateway it ran against answers /eth/v1/events with 501, and
// internal/source/beaconapi retries for the life of the process, deliberately,
// so a node that gains the endpoint later is picked up with no operator action.
// The retrying is correct and is not what changed. Burying 55 real errors under
// an identical line repeated every fifteen seconds is what changed.
//
// A different error is always reported immediately: two failure modes in
// succession are two events, and collapsing them would hide the second.
type streamHealth struct {
	now      func() time.Time
	interval time.Duration

	mu       sync.Mutex
	last     string
	since    time.Time
	reported time.Time
	count    int
}

func newStreamHealth(now func() time.Time) *streamHealth {
	return &streamHealth{now: now, interval: streamReminderInterval}
}

// failed reports a stream error, suppressing consecutive repeats of the same one
// until the reminder interval has passed.
func (h *streamHealth) failed(logger *slog.Logger, err error) {
	if err == nil {
		return
	}
	message := err.Error()

	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()

	if message != h.last {
		h.last, h.since, h.reported, h.count = message, now, now, 1
		logger.Warn("event stream error, reconnecting", "error", err)
		return
	}

	h.count++
	if now.Sub(h.reported) < h.interval {
		return
	}
	h.reported = now
	logger.Warn("event stream still failing, still reconnecting",
		"error", err, "attempts", h.count, "failing_for", now.Sub(h.since).Round(time.Second).String())
}

// Durations are logged as strings ("2m0s"), not as slog's default nanosecond
// integers: this log is read by a person during an incident.
//
// recovered reports that the stream is delivering again, and is a no-op if it
// never stopped. The caller reports it when an observation actually arrives,
// because that is the only evidence the stream works — the reconnect loop itself
// cannot tell a connection that succeeded from one about to fail.
func (h *streamHealth) recovered(logger *slog.Logger) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.count == 0 {
		return
	}
	attempts, downtime := h.count, h.now().Sub(h.since).Round(time.Second).String()
	h.last, h.count, h.since, h.reported = "", 0, time.Time{}, time.Time{}
	logger.Info("event stream recovered", "failed_attempts", attempts, "was_failing_for", downtime)
}
