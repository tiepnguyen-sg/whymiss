package clock

import (
	"context"
	"math/rand/v2"
	"time"
)

// backoffBase and backoffCap bound the exponential backoff between measurement
// attempts. They are package constants rather than configuration: I-5 requires
// backoff to be exponential with jitter, not that its shape be tunable, and a
// caller wanting different timing composes it into the per-attempt Config.Timeout
// instead.
const (
	backoffBase = 200 * time.Millisecond
	backoffCap  = 10 * time.Second
)

// backoffCeiling is the deterministic upper bound sleepBackoff jitters within for
// a given attempt (1-indexed: the retry following the first failure). Exponential
// in attempt, capped at backoffCap. Split out from sleepBackoff so the growth and
// cap behaviour are testable without actually sleeping.
func backoffCeiling(attempt int) time.Duration {
	d := backoffBase << attempt
	if d <= 0 || d > backoffCap {
		// d <= 0 catches the left-shift overflowing into the sign bit for a large
		// attempt count; backoffCap is the intended ceiling either way.
		return backoffCap
	}
	return d
}

// sleepBackoff waits before a retry. The wait is chosen uniformly between zero and
// backoffCeiling(attempt) — "full jitter" — which is the shape that avoids
// synchronized retry storms across many whymiss instances failing at once (I-5).
//
// It returns ctx.Err() if the context is cancelled before the wait elapses, so a
// caller backing off is still responsive to shutdown.
func sleepBackoff(ctx context.Context, attempt int) error {
	jittered := time.Duration(rand.Int64N(int64(backoffCeiling(attempt)))) //nolint:gosec // G404: retry jitter, not a security-sensitive use of randomness

	timer := time.NewTimer(jittered)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
