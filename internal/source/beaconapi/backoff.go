package beaconapi

import (
	"context"
	"math/rand/v2"
	"time"
)

// reconnectBase and reconnectCap bound the exponential backoff between SSE
// reconnect attempts. Same "full jitter" shape as internal/clock's backoff
// (I-5: exponential with jitter, not a tunable knob), sized larger than
// clock's since a lost SSE connection is a much less urgent recovery than a
// clock sample.
const (
	reconnectBase = 1 * time.Second
	reconnectCap  = 30 * time.Second
)

// reconnectCeiling is the deterministic upper bound sleepReconnect jitters
// within for a given attempt (0-indexed). Split out so growth and cap
// behaviour are testable without actually sleeping.
func reconnectCeiling(attempt int) time.Duration {
	d := reconnectBase << attempt
	if d <= 0 || d > reconnectCap {
		// d <= 0 catches the left-shift overflowing into the sign bit for a
		// large attempt count; reconnectCap is the intended ceiling either way.
		return reconnectCap
	}
	return d
}

// sleepReconnect waits before a reconnect attempt, uniformly between zero
// and reconnectCeiling(attempt). Returns ctx.Err() if ctx is done first, so
// a stream shutting down doesn't wait out a long backoff first.
func sleepReconnect(ctx context.Context, attempt int) error {
	jittered := time.Duration(rand.Int64N(int64(reconnectCeiling(attempt)))) //nolint:gosec // G404: reconnect jitter, not a security-sensitive use of randomness

	timer := time.NewTimer(jittered)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
