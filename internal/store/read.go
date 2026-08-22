package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// ObservationsForSlot returns every observation recorded for slot, sorted
// by At ascending — the direct input internal/timeline.Assembler.Build
// expects.
func (s *Store) ObservationsForSlot(ctx context.Context, slot domain.Slot) ([]domain.Observation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT slot, kind, at, clock_offset_ns, source, attrs FROM observations WHERE slot = ? ORDER BY at ASC`,
		int64(slot), //nolint:gosec // G115: slot numbers never approach int64's range
	)
	if err != nil {
		return nil, fmt.Errorf("query observations for slot %d: %w", slot, err)
	}
	defer rows.Close() //nolint:errcheck // read-only query; nothing to act on if Close fails

	var out []domain.Observation
	for rows.Next() {
		obs, err := scanObservation(rows)
		if err != nil {
			return nil, fmt.Errorf("scan observation for slot %d: %w", slot, err)
		}
		out = append(out, obs)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read observations for slot %d: %w", slot, err)
	}
	return out, nil
}

// rowScanner is the subset of *sql.Rows this package's scan helpers need —
// small enough that a caller could pass *sql.Row too, though nothing here
// currently does.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanObservation(row rowScanner) (domain.Observation, error) {
	var (
		slot          int64
		kind          string
		atStr         string
		clockOffsetNS int64
		source        string
		attrsJSON     string
	)
	if err := row.Scan(&slot, &kind, &atStr, &clockOffsetNS, &source, &attrsJSON); err != nil {
		return domain.Observation{}, fmt.Errorf("scan row: %w", err)
	}
	at, err := time.Parse(timeLayout, atStr)
	if err != nil {
		return domain.Observation{}, fmt.Errorf("parse at %q: %w", atStr, err)
	}
	var attrs map[domain.AttrKey]string
	if err := json.Unmarshal([]byte(attrsJSON), &attrs); err != nil {
		return domain.Observation{}, fmt.Errorf("decode attrs %q: %w", attrsJSON, err)
	}

	draft := domain.Observation{
		Slot:        domain.Slot(slot), //nolint:gosec // G115: slot values round-trip through this package's own writes, never negative
		Kind:        domain.ObservationKind(kind),
		At:          at.UTC(),
		ClockOffset: time.Duration(clockOffsetNS),
		Source:      domain.SourceID(source),
		Attrs:       attrs,
	}
	obs, err := domain.NewObservation(draft)
	if err != nil {
		return domain.Observation{}, fmt.Errorf("reconstruct observation: %w", err)
	}
	return obs, nil
}

// SamplesSince returns every metric sample recorded at or after since,
// sorted by At ascending — the window a rolling-baseline computation
// (docs/causes.md's engine-call p99, for one) reads from.
func (s *Store) SamplesSince(ctx context.Context, since time.Time) ([]domain.MetricSample, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT at, component, name, value, source FROM samples WHERE at >= ? ORDER BY at ASC`,
		since.UTC().Format(timeLayout),
	)
	if err != nil {
		return nil, fmt.Errorf("query samples since %s: %w", since, err)
	}
	defer rows.Close() //nolint:errcheck // read-only query; nothing to act on if Close fails

	var out []domain.MetricSample
	for rows.Next() {
		sample, err := scanSample(rows)
		if err != nil {
			return nil, fmt.Errorf("scan sample: %w", err)
		}
		out = append(out, sample)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read samples since %s: %w", since, err)
	}
	return out, nil
}

func scanSample(row rowScanner) (domain.MetricSample, error) {
	var (
		atStr     string
		component string
		name      string
		value     float64
		source    string
	)
	if err := row.Scan(&atStr, &component, &name, &value, &source); err != nil {
		return domain.MetricSample{}, fmt.Errorf("scan row: %w", err)
	}
	at, err := time.Parse(timeLayout, atStr)
	if err != nil {
		return domain.MetricSample{}, fmt.Errorf("parse at %q: %w", atStr, err)
	}

	sample := domain.MetricSample{
		At:        at.UTC(),
		Component: domain.Component(component),
		Name:      domain.MetricName(name),
		Value:     value,
		Source:    domain.SourceID(source),
	}
	if err := sample.Validate(); err != nil {
		return domain.MetricSample{}, fmt.Errorf("reconstruct sample: %w", err)
	}
	return sample, nil
}
