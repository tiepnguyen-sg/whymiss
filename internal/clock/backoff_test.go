package clock

import (
	"context"
	"testing"
	"time"
)

// TestBackoffCeilingGrows and TestBackoffCeilingCapped exercise the deterministic
// shape of the backoff schedule instantly, without sleeping — the randomness and
// timing live only in sleepBackoff, tested separately below with real but short
// waits.
func TestBackoffCeilingGrows(t *testing.T) {
	t.Parallel()

	prev := time.Duration(0)
	for attempt := 1; attempt <= 5; attempt++ {
		got := backoffCeiling(attempt)
		if got <= prev {
			t.Errorf("backoffCeiling(%d) = %s, want it to grow past attempt %d's %s", attempt, got, attempt-1, prev)
		}
		if got > backoffCap {
			t.Errorf("backoffCeiling(%d) = %s, exceeds backoffCap %s", attempt, got, backoffCap)
		}
		prev = got
	}
}

func TestBackoffCeilingCapped(t *testing.T) {
	t.Parallel()

	// Large enough that backoffBase<<attempt has long since overflowed past
	// backoffCap, and large enough to hit the int overflow guard directly.
	for _, attempt := range []int{20, 63} {
		if got := backoffCeiling(attempt); got != backoffCap {
			t.Errorf("backoffCeiling(%d) = %s, want the cap %s", attempt, got, backoffCap)
		}
	}
}

func TestSleepBackoffCompletesWithoutCancellation(t *testing.T) {
	t.Parallel()

	start := time.Now()
	if err := sleepBackoff(context.Background(), 1); err != nil {
		t.Fatalf("sleepBackoff(1) error = %v, want nil", err)
	}
	if elapsed := time.Since(start); elapsed > backoffCeiling(1)+time.Second {
		t.Errorf("sleepBackoff(1) took %s, want at most %s (its ceiling, plus scheduling slack)",
			elapsed, backoffCeiling(1))
	}
}

func TestSleepBackoffRespectsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := sleepBackoff(ctx, 10) // a large attempt would otherwise wait up to backoffCap
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("sleepBackoff() error = nil, want the cancellation error")
	}
	if elapsed > time.Second {
		t.Errorf("sleepBackoff() took %s to notice an already-cancelled context", elapsed)
	}
}
