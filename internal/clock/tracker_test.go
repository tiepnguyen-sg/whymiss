package clock

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestTrackerRecordsSuccess(t *testing.T) {
	t.Parallel()

	srv := newFakeServer(t, time.Second)
	sampler, err := New(Config{Servers: []string{srv.addr}, Timeout: time.Second, MaxAttempts: 1})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	tr := NewTracker(sampler)

	if _, _, ok := tr.LastKnownGood(); ok {
		t.Fatal("LastKnownGood() ok = true before any sample was taken")
	}

	before := time.Now().UTC()
	reading, err := tr.Sample(context.Background())
	if err != nil {
		t.Fatalf("Sample() error = %v, want nil", err)
	}

	last, syncAt, ok := tr.LastKnownGood()
	if !ok {
		t.Fatal("LastKnownGood() ok = false after a successful sample")
	}
	if last != reading {
		t.Errorf("LastKnownGood() reading = %+v, want %+v", last, reading)
	}
	if syncAt.Before(before) {
		t.Errorf("LastKnownGood() syncAt = %v, want it no earlier than %v", syncAt, before)
	}
}

// TestTrackerKeepsLastGoodAfterFailure is the behaviour docs/causes.md relies on:
// local.host.clock_drift's required evidence names "time of last successful sync",
// which only means something if a later failed measurement does not erase it.
func TestTrackerKeepsLastGoodAfterFailure(t *testing.T) {
	t.Parallel()

	good := newFakeServer(t, time.Second)
	sampler, err := New(Config{Servers: []string{good.addr}, Timeout: 200 * time.Millisecond, MaxAttempts: 1})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	tr := NewTracker(sampler)

	firstReading, err := tr.Sample(context.Background())
	if err != nil {
		t.Fatalf("Sample() error = %v, want nil", err)
	}
	_, firstSyncAt, _ := tr.LastKnownGood()

	good.setSilent()
	if _, err := tr.Sample(context.Background()); err == nil {
		t.Fatal("Sample() error = nil, want the second sample to fail now the server is silent")
	}

	last, syncAt, ok := tr.LastKnownGood()
	if !ok {
		t.Fatal("LastKnownGood() ok = false after a failure that followed a success")
	}
	if last != firstReading {
		t.Errorf("LastKnownGood() reading = %+v after a failed sample, want the earlier %+v preserved", last, firstReading)
	}
	if !syncAt.Equal(firstSyncAt) {
		t.Errorf("LastKnownGood() syncAt = %v after a failed sample, want the earlier %v preserved", syncAt, firstSyncAt)
	}
}

func TestTrackerPropagatesFailureWithoutRecording(t *testing.T) {
	t.Parallel()

	srv := newFakeServer(t, 0)
	srv.setSilent()
	sampler, err := New(Config{Servers: []string{srv.addr}, Timeout: 100 * time.Millisecond, MaxAttempts: 1})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	tr := NewTracker(sampler)

	_, err = tr.Sample(context.Background())
	if !errors.Is(err, ErrAllAttemptsFailed) {
		t.Fatalf("Sample() error = %v, want %v", err, ErrAllAttemptsFailed)
	}
	if _, _, ok := tr.LastKnownGood(); ok {
		t.Error("LastKnownGood() ok = true after every sample has failed")
	}
}

// TestTrackerConcurrentAccess is the property the doc comment promises: Sample and
// LastKnownGood may be called from different goroutines at once. Run with -race.
func TestTrackerConcurrentAccess(t *testing.T) {
	t.Parallel()

	srv := newFakeServer(t, 0)
	sampler, err := New(Config{Servers: []string{srv.addr}, Timeout: time.Second, MaxAttempts: 1})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	tr := NewTracker(sampler)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = tr.Sample(context.Background())
		}()
		go func() {
			defer wg.Done()
			tr.LastKnownGood()
		}()
	}
	wg.Wait()
}
