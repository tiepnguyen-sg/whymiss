package app

import (
	"context"
	"log/slog"
	"math"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/clock"
	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// stampClock attaches a fresh clock reading to obs. A stale last-known-good
// reading is deliberately not reused: it remains useful for diagnostics, but it
// cannot establish clock trust at this observation's timestamp (I-9).
func stampClock(tracker *clock.Tracker, maxAge time.Duration, obs domain.Observation) domain.Observation {
	return stampClockProvenance(tracker, maxAge, obs, obs.Kind != domain.ObsSlotStart)
}

func stampClockProvenance(tracker *clock.Tracker, maxAge time.Duration, obs domain.Observation, correctAt bool) domain.Observation {
	// An observation may pass through more than one composition path (for
	// example, REST head fallback is persisted and queued for metrics scraping).
	// Clock correction is not additive: once provenance is attached, preserve the
	// exact timestamp that was persisted.
	if obs.ClockMeasured || obs.ClockOffset != 0 || !obs.ClockSampleAt.IsZero() {
		return obs
	}
	if tracker == nil {
		return obs
	}
	reading, _, ok := tracker.LastKnownGood()
	if !ok {
		return obs
	}
	age := absoluteDuration(obs.At.Sub(reading.At))
	if maxAge <= 0 || age > maxAge {
		return obs
	}

	obs.ClockOffset = reading.Offset
	obs.ClockMeasured = true
	obs.ClockSampleAt = reading.At.Add(reading.Offset).UTC()
	// The chain's slot start is already canonical. Other timestamps, including
	// the locally generated completion marker, are wall-clock reads and need the
	// measured offset applied to satisfy Observation.At's corrected contract.
	if correctAt {
		obs.At = obs.At.Add(reading.Offset).UTC()
	}
	return obs
}

func stampSampleClock(tracker *clock.Tracker, maxAge time.Duration, sample domain.MetricSample) domain.MetricSample {
	if sample.ClockMeasured || sample.ClockOffset != 0 || !sample.ClockSampleAt.IsZero() {
		return sample
	}
	if tracker == nil || sample.Source == domain.SourceDerived {
		return sample
	}
	reading, _, ok := tracker.LastKnownGood()
	if !ok {
		return sample
	}
	age := absoluteDuration(sample.At.Sub(reading.At))
	if maxAge <= 0 || age > maxAge {
		return sample
	}
	sample.At = sample.At.Add(reading.Offset).UTC()
	sample.ClockOffset = reading.Offset
	sample.ClockMeasured = true
	sample.ClockSampleAt = reading.At.Add(reading.Offset).UTC()
	return sample
}

func absoluteDuration(value time.Duration) time.Duration {
	if value == time.Duration(math.MinInt64) {
		return time.Duration(math.MaxInt64)
	}
	if value < 0 {
		return -value
	}
	return value
}

// runClockSampler drives tracker on interval until ctx ends. A failed sample
// just logs. Tracker preserves the previous reading for diagnostics, while
// stampClock independently rejects it once it exceeds maxAge.
func runClockSampler(ctx context.Context, tracker *clock.Tracker, interval time.Duration, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	if tracker == nil || interval <= 0 {
		logger.Error("clock sampler disabled by invalid runtime configuration", "interval", interval)
		return
	}
	sample := func() {
		reading, err := tracker.Sample(ctx)
		if err != nil {
			logger.Warn("clock sample failed", "error", err)
			return
		}
		logger.Debug("clock sampled", "offset", reading.Offset, "server", reading.Server)
	}

	sample()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sample()
		}
	}
}
