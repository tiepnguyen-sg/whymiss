package main

import (
	"context"
	"testing"
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
