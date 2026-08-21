package beaconapi

import (
	"context"
	"testing"
	"time"
)

// TestRateLimiter_EnforcesCeiling is task 2.9's required proof: a burst of
// requests against a Client configured with minInterval never exceeds one
// request per minInterval, regardless of how many callers ask concurrently
// (I-5).
func TestRateLimiter_EnforcesCeiling(t *testing.T) {
	const (
		minInterval = 20 * time.Millisecond
		requests    = 10
	)
	limiter := newRateLimiter(minInterval)

	start := time.Now()
	for range requests {
		if err := limiter.wait(context.Background()); err != nil {
			t.Fatalf("wait: %v", err)
		}
	}
	elapsed := time.Since(start)

	// requests grants happen at t=0, minInterval, 2*minInterval, ...,
	// (requests-1)*minInterval — so the ceiling is (requests-1)*minInterval,
	// not requests*minInterval.
	floor := time.Duration(requests-1) * minInterval
	if elapsed < floor {
		t.Errorf("elapsed = %v, want at least %v (%d requests at minInterval %v apart)", elapsed, floor, requests, minInterval)
	}
}

// TestRateLimiter_ConcurrentCallersStillSerialize proves the ceiling holds
// even when every caller asks at once, not just when a single goroutine
// asks in a loop — the realistic shape of this package's actual use, where
// SSE streaming and REST polling run from separate goroutines against the
// same Client.
func TestRateLimiter_ConcurrentCallersStillSerialize(t *testing.T) {
	const (
		minInterval = 10 * time.Millisecond
		requests    = 8
	)
	limiter := newRateLimiter(minInterval)

	start := time.Now()
	done := make(chan struct{}, requests)
	for range requests {
		go func() {
			if err := limiter.wait(context.Background()); err != nil {
				t.Error(err)
			}
			done <- struct{}{}
		}()
	}
	for range requests {
		<-done
	}
	elapsed := time.Since(start)

	floor := time.Duration(requests-1) * minInterval
	if elapsed < floor {
		t.Errorf("elapsed = %v, want at least %v even when all %d callers ask concurrently", elapsed, floor, requests)
	}
}

func TestRateLimiter_CancelledContext(t *testing.T) {
	limiter := newRateLimiter(time.Hour) // long enough that the second wait would block for the test's duration if not cancelled
	if err := limiter.wait(context.Background()); err != nil {
		t.Fatalf("first wait: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := limiter.wait(ctx); err == nil {
		t.Error("wait: want an error for an already-cancelled context, got nil")
	}
}
