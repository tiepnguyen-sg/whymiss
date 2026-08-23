package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// WriteObservation persists obs. Attrs is stored JSON-encoded — this
// package's own on-disk detail, not a format any other package depends on;
// every read path decodes it back into the same map[domain.AttrKey]string.
func (s *Store) WriteObservation(ctx context.Context, obs domain.Observation) error {
	if err := obs.Validate(); err != nil {
		return fmt.Errorf("validate observation: %w", err)
	}
	if obs.Slot > domain.Slot(math.MaxInt64) {
		return fmt.Errorf("validate observation: slot %d exceeds SQLite INTEGER range", obs.Slot)
	}
	attrsJSON, err := json.Marshal(obs.Attrs)
	if err != nil {
		return fmt.Errorf("encode attrs: %w", err)
	}
	clockSampleAt := ""
	if !obs.ClockSampleAt.IsZero() {
		clockSampleAt = obs.ClockSampleAt.Format(timeLayout)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO observations (slot, kind, at, clock_offset_ns, clock_measured, clock_sample_at, source, attrs) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		int64(obs.Slot), string(obs.Kind), obs.At.Format(timeLayout), int64(obs.ClockOffset), obs.ClockMeasured, clockSampleAt, string(obs.Source), string(attrsJSON), //nolint:gosec // G115: slot range checked above; time.Duration is int64
	)
	if err != nil {
		return fmt.Errorf("insert observation: %w", err)
	}
	return nil
}

// WriteSample persists s.
func (s *Store) WriteSample(ctx context.Context, sample domain.MetricSample) error {
	if err := sample.Validate(); err != nil {
		return fmt.Errorf("validate sample: %w", err)
	}
	clockSampleAt := ""
	if !sample.ClockSampleAt.IsZero() {
		clockSampleAt = sample.ClockSampleAt.Format(timeLayout)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO samples (at, component, name, value, clock_offset_ns, clock_measured, clock_sample_at, source) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		sample.At.Format(timeLayout), string(sample.Component), string(sample.Name), sample.Value,
		int64(sample.ClockOffset), sample.ClockMeasured, clockSampleAt, string(sample.Source),
	)
	if err != nil {
		return fmt.Errorf("insert sample: %w", err)
	}
	return nil
}
