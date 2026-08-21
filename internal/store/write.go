package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/CHANGEME/whymiss/internal/domain"
)

// WriteObservation persists obs. Attrs is stored JSON-encoded — this
// package's own on-disk detail, not a format any other package depends on;
// every read path decodes it back into the same map[domain.AttrKey]string.
func (s *Store) WriteObservation(ctx context.Context, obs domain.Observation) error {
	attrsJSON, err := json.Marshal(obs.Attrs)
	if err != nil {
		return fmt.Errorf("encode attrs: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO observations (slot, kind, at, clock_offset_ns, source, attrs) VALUES (?, ?, ?, ?, ?, ?)`,
		int64(obs.Slot), string(obs.Kind), obs.At.Format(timeLayout), int64(obs.ClockOffset), string(obs.Source), string(attrsJSON), //nolint:gosec // G115: slot numbers and clock offsets never approach int64's range
	)
	if err != nil {
		return fmt.Errorf("insert observation: %w", err)
	}
	return nil
}

// WriteSample persists s.
func (s *Store) WriteSample(ctx context.Context, sample domain.MetricSample) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO samples (at, component, name, value, source) VALUES (?, ?, ?, ?, ?)`,
		sample.At.Format(timeLayout), string(sample.Component), string(sample.Name), sample.Value, string(sample.Source),
	)
	if err != nil {
		return fmt.Errorf("insert sample: %w", err)
	}
	return nil
}
