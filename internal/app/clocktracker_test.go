package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/clock"
	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

type fixedClockSampler struct {
	reading clock.Reading
	err     error
}

func (s fixedClockSampler) Sample(context.Context) (clock.Reading, error) {
	return s.reading, s.err
}

func trackerWithReading(t *testing.T, reading clock.Reading) *clock.Tracker {
	t.Helper()
	tracker, err := clock.NewTracker(fixedClockSampler{reading: reading})
	if err != nil {
		t.Fatalf("new tracker: %v", err)
	}
	if _, err := tracker.Sample(context.Background()); err != nil {
		t.Fatalf("seed tracker: %v", err)
	}
	return tracker
}

func TestStampClock(t *testing.T) {
	t.Parallel()

	sampledAt := time.Date(2026, 8, 23, 1, 0, 0, 0, time.UTC)
	reading := clock.Reading{At: sampledAt, Offset: 40 * time.Millisecond, Server: "ntp.example:123"}

	t.Run("corrects adapter timestamp and records freshness metadata", func(t *testing.T) {
		t.Parallel()
		at := sampledAt.Add(10 * time.Second)
		obs := domain.Observation{At: at, Source: domain.SourceBeaconAPI}
		got := stampClock(trackerWithReading(t, reading), time.Minute, obs)

		if !got.At.Equal(at.Add(reading.Offset)) {
			t.Errorf("At = %s, want %s", got.At, at.Add(reading.Offset))
		}
		if !got.ClockMeasured || got.ClockOffset != reading.Offset {
			t.Errorf("clock metadata = measured %v offset %s", got.ClockMeasured, got.ClockOffset)
		}
		if want := sampledAt.Add(reading.Offset); !got.ClockSampleAt.Equal(want) {
			t.Errorf("ClockSampleAt = %s, want %s", got.ClockSampleAt, want)
		}
	})

	t.Run("does not shift canonical slot start", func(t *testing.T) {
		t.Parallel()
		obs := domain.Observation{At: sampledAt, Kind: domain.ObsSlotStart, Source: domain.SourceDerived}
		got := stampClock(trackerWithReading(t, reading), time.Minute, obs)
		if !got.At.Equal(obs.At) {
			t.Errorf("At = %s, want unchanged %s", got.At, obs.At)
		}
		if !got.ClockMeasured {
			t.Error("derived observation should still carry the trust measurement")
		}
	})

	t.Run("corrects locally timed derived completion", func(t *testing.T) {
		t.Parallel()
		obs := domain.Observation{At: sampledAt, Kind: domain.ObsCollectionCompleted, Source: domain.SourceDerived}
		got := stampClock(trackerWithReading(t, reading), time.Minute, obs)
		if !got.At.Equal(obs.At.Add(reading.Offset)) {
			t.Errorf("At = %s, want corrected %s", got.At, obs.At.Add(reading.Offset))
		}
	})

	t.Run("rejects stale last known good", func(t *testing.T) {
		t.Parallel()
		obs := domain.Observation{At: sampledAt.Add(3 * time.Minute), Source: domain.SourceBeaconAPI}
		got := stampClock(trackerWithReading(t, reading), 2*time.Minute, obs)
		if got.ClockMeasured || got.ClockOffset != 0 || !got.ClockSampleAt.IsZero() {
			t.Errorf("stale reading was stamped: %+v", got)
		}
		if !got.At.Equal(obs.At) {
			t.Errorf("stale reading changed At to %s", got.At)
		}
	})

	t.Run("rejects freshness duration at minimum integer", func(t *testing.T) {
		t.Parallel()
		farPast := time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)
		farFuture := farPast.Add(time.Duration(math.MaxInt64)).Add(time.Nanosecond)
		obs := domain.Observation{At: farPast, Source: domain.SourceBeaconAPI}
		reading := clock.Reading{At: farFuture, Server: "ntp.example:123"}
		got := stampClock(trackerWithReading(t, reading), time.Minute, obs)
		if got.ClockMeasured {
			t.Fatalf("overflowed stale reading was trusted: %+v", got)
		}
	})

	t.Run("passes through when sampling has never succeeded", func(t *testing.T) {
		t.Parallel()
		tracker, err := clock.NewTracker(fixedClockSampler{err: errors.New("offline")})
		if err != nil {
			t.Fatal(err)
		}
		obs := domain.Observation{At: sampledAt, Source: domain.SourceBeaconAPI}
		if got := stampClock(tracker, time.Minute, obs); !reflect.DeepEqual(got, obs) {
			t.Errorf("stampClock() = %+v, want unchanged %+v", got, obs)
		}
	})

	t.Run("does not apply clock correction twice", func(t *testing.T) {
		t.Parallel()
		at := sampledAt.Add(10 * time.Second)
		tracker := trackerWithReading(t, reading)
		once := stampClock(tracker, time.Minute, domain.Observation{At: at, Source: domain.SourceBeaconAPI})
		twice := stampClock(tracker, time.Minute, once)
		if !reflect.DeepEqual(twice, once) {
			t.Errorf("second stamp changed observation: once=%+v twice=%+v", once, twice)
		}
	})
}

func TestStampSampleClock(t *testing.T) {
	t.Parallel()
	sampledAt := time.Date(2026, 8, 23, 1, 0, 0, 0, time.UTC)
	reading := clock.Reading{At: sampledAt, Offset: -25 * time.Millisecond, Server: "ntp.example:123"}
	sample := domain.MetricSample{
		At: sampledAt.Add(10 * time.Second), Component: domain.ComponentHost,
		Name: "host_mem_pressure_pct", Source: domain.SourceHostMetrics,
	}
	got := stampSampleClock(trackerWithReading(t, reading), time.Minute, sample)
	if !got.At.Equal(sample.At.Add(reading.Offset)) || !got.ClockMeasured || got.ClockOffset != reading.Offset {
		t.Fatalf("stampSampleClock() = %+v", got)
	}
	if want := sampledAt.Add(reading.Offset); !got.ClockSampleAt.Equal(want) {
		t.Errorf("ClockSampleAt = %s, want %s", got.ClockSampleAt, want)
	}
	if twice := stampSampleClock(trackerWithReading(t, reading), time.Minute, got); !reflect.DeepEqual(twice, got) {
		t.Errorf("second sample stamp changed value: once=%+v twice=%+v", got, twice)
	}

	derived := sample
	derived.Source = domain.SourceDerived
	if got := stampSampleClock(trackerWithReading(t, reading), time.Minute, derived); !reflect.DeepEqual(got, derived) {
		t.Errorf("derived sample changed: %+v", got)
	}
}

func TestRunClockSamplerRejectsInvalidInterval(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	done := make(chan struct{})
	go func() {
		runClockSampler(context.Background(), nil, 0, logger)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runClockSampler did not return for invalid configuration")
	}
}
