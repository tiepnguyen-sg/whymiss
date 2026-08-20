package clock

import (
	"context"
	"sync"
	"time"
)

// Tracker wraps a [Sampler] and remembers the last successful [Reading].
//
// docs/causes.md requires local.host.clock_drift evidence to name "time of last
// successful sync" — a fact that is only available if something outlives the single
// failed measurement that triggered the verdict. A Tracker is that something: a
// long-lived component in the collector daemon (Phase 2) samples through it on a
// schedule, and whatever assembles evidence later asks it what the last good
// reading was, even after several consecutive failures.
//
// Safe for concurrent use: Sample is expected to be called from one periodic
// goroutine while LastKnownGood is read from others (a report renderer, a
// Prometheus collector).
type Tracker struct {
	sampler *Sampler

	mu     sync.Mutex
	last   Reading
	syncAt time.Time
	have   bool
}

// NewTracker wraps sampler. sampler must not be nil.
func NewTracker(sampler *Sampler) *Tracker {
	return &Tracker{sampler: sampler}
}

// Sample measures once through the wrapped Sampler. On success it records the
// reading as the new last-known-good before returning it. On failure it returns
// the error unchanged and leaves the last-known-good reading exactly as it was —
// a failed attempt must never overwrite the last honest measurement with nothing.
func (t *Tracker) Sample(ctx context.Context) (Reading, error) {
	reading, err := t.sampler.Sample(ctx)
	if err != nil {
		return Reading{}, err
	}

	t.mu.Lock()
	t.last, t.syncAt, t.have = reading, time.Now().UTC(), true
	t.mu.Unlock()

	return reading, nil
}

// LastKnownGood returns the most recent successful reading, when it was recorded,
// and whether one has ever succeeded. syncAt is when the Tracker observed the
// success, not the reading's own At — the two are typically close but syncAt is
// the honest answer to "when did we last know this worked."
func (t *Tracker) LastKnownGood() (reading Reading, syncAt time.Time, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.last, t.syncAt, t.have
}
