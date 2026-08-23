package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

const (
	maxObservationsPerSlot = 1024
	maxSamplesPerRange     = 2048
	maxReorgsPerWindow     = 128
)

// ObservationsForSlot returns every observation recorded for slot, sorted
// by At ascending — the direct input internal/timeline.Assembler.Build
// expects.
func (s *Store) ObservationsForSlot(ctx context.Context, slot domain.Slot) ([]domain.Observation, error) {
	if slot > domain.Slot(math.MaxInt64) {
		return nil, fmt.Errorf("query observations: slot %d exceeds SQLite INTEGER range", slot)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT slot, kind, at, clock_offset_ns, clock_measured, clock_sample_at, source, attrs FROM observations WHERE slot = ? ORDER BY at ASC LIMIT ?`,
		int64(slot), //nolint:gosec // G115: slot numbers never approach int64's range
		maxObservationsPerSlot+1,
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
		if len(out) > maxObservationsPerSlot {
			return nil, fmt.Errorf("slot %d has more than %d observations; refusing an unbounded timeline", slot, maxObservationsPerSlot)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read observations for slot %d: %w", slot, err)
	}
	return out, nil
}

// ReorgsBetweenSlots returns reorg events after start through end, preserving
// each event's own slot for inclusion-window context.
func (s *Store) ReorgsBetweenSlots(ctx context.Context, start, end domain.Slot) ([]domain.Observation, error) {
	if end <= start {
		return nil, nil
	}
	if start > domain.Slot(math.MaxInt64) {
		return nil, fmt.Errorf("query reorgs: start slot %d exceeds SQLite INTEGER range", start)
	}
	if end > domain.Slot(math.MaxInt64) {
		end = domain.Slot(math.MaxInt64)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT slot, kind, at, clock_offset_ns, clock_measured, clock_sample_at, source, attrs FROM observations WHERE kind = ? AND slot > ? AND slot <= ? ORDER BY at ASC LIMIT ?`,
		string(domain.ObsReorg), int64(start), int64(end), maxReorgsPerWindow+1, //nolint:gosec // slots are checked against MaxInt64 above
	)
	if err != nil {
		return nil, fmt.Errorf("query reorgs for slots %d..%d: %w", start, end, err)
	}
	defer rows.Close() //nolint:errcheck // read-only query; nothing to act on if Close fails
	var out []domain.Observation
	for rows.Next() {
		obs, err := scanObservation(rows)
		if err != nil {
			return nil, fmt.Errorf("scan reorg for slots %d..%d: %w", start, end, err)
		}
		out = append(out, obs)
		if len(out) > maxReorgsPerWindow {
			return nil, fmt.Errorf("slots %d..%d have more than %d reorgs; refusing an unbounded timeline", start, end, maxReorgsPerWindow)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read reorgs for slots %d..%d: %w", start, end, err)
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
		clockMeasured bool
		clockSampleAt string
		source        string
		attrsJSON     string
	)
	if err := row.Scan(&slot, &kind, &atStr, &clockOffsetNS, &clockMeasured, &clockSampleAt, &source, &attrsJSON); err != nil {
		return domain.Observation{}, fmt.Errorf("scan row: %w", err)
	}
	if slot < 0 {
		return domain.Observation{}, fmt.Errorf("slot is negative: %d", slot)
	}
	at, err := time.Parse(timeLayout, atStr)
	if err != nil {
		return domain.Observation{}, fmt.Errorf("parse at %q: %w", atStr, err)
	}
	var sampledAt time.Time
	if clockSampleAt != "" {
		sampledAt, err = time.Parse(timeLayout, clockSampleAt)
		if err != nil {
			return domain.Observation{}, fmt.Errorf("parse clock_sample_at %q: %w", clockSampleAt, err)
		}
	}
	var attrs map[domain.AttrKey]string
	if err := json.Unmarshal([]byte(attrsJSON), &attrs); err != nil {
		return domain.Observation{}, fmt.Errorf("decode attrs %q: %w", attrsJSON, err)
	}

	draft := domain.Observation{
		Slot:          domain.Slot(slot), //nolint:gosec // G115: negative values rejected above
		Kind:          domain.ObservationKind(kind),
		At:            at.UTC(),
		ClockOffset:   time.Duration(clockOffsetNS),
		ClockMeasured: clockMeasured,
		ClockSampleAt: sampledAt.UTC(),
		Source:        domain.SourceID(source),
		Attrs:         attrs,
	}
	obs, err := domain.NewObservation(draft)
	if err != nil {
		return domain.Observation{}, fmt.Errorf("reconstruct observation: %w", err)
	}
	return obs, nil
}

// SamplesBetween returns samples inside the closed time range, oldest first.
func (s *Store) SamplesBetween(ctx context.Context, start, end time.Time) ([]domain.MetricSample, error) {
	if end.Before(start) {
		return nil, fmt.Errorf("sample range ends before it starts: %s < %s", end, start)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT at, component, name, value, clock_offset_ns, clock_measured, clock_sample_at, source
FROM samples WHERE at >= ? AND at <= ? ORDER BY at ASC LIMIT ?`,
		start.UTC().Format(timeLayout), end.UTC().Format(timeLayout),
		maxSamplesPerRange+1,
	)
	if err != nil {
		return nil, fmt.Errorf("query samples from %s to %s: %w", start, end, err)
	}
	defer rows.Close() //nolint:errcheck // read-only query; nothing to act on if Close fails

	var out []domain.MetricSample
	for rows.Next() {
		sample, err := scanSample(rows)
		if err != nil {
			return nil, fmt.Errorf("scan sample: %w", err)
		}
		out = append(out, sample)
		if len(out) > maxSamplesPerRange {
			return nil, fmt.Errorf("sample range has more than %d rows; refusing an unbounded timeline", maxSamplesPerRange)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read samples from %s to %s: %w", start, end, err)
	}
	return out, nil
}

func scanSample(row rowScanner) (domain.MetricSample, error) {
	var (
		atStr         string
		component     string
		name          string
		value         float64
		clockOffsetNS int64
		clockMeasured bool
		clockSampleAt string
		source        string
	)
	if err := row.Scan(&atStr, &component, &name, &value, &clockOffsetNS, &clockMeasured, &clockSampleAt, &source); err != nil {
		return domain.MetricSample{}, fmt.Errorf("scan row: %w", err)
	}
	at, err := time.Parse(timeLayout, atStr)
	if err != nil {
		return domain.MetricSample{}, fmt.Errorf("parse at %q: %w", atStr, err)
	}
	var sampledAt time.Time
	if clockSampleAt != "" {
		sampledAt, err = time.Parse(timeLayout, clockSampleAt)
		if err != nil {
			return domain.MetricSample{}, fmt.Errorf("parse clock_sample_at %q: %w", clockSampleAt, err)
		}
	}

	sample := domain.MetricSample{
		At: at.UTC(), Component: domain.Component(component), Name: domain.MetricName(name), Value: value,
		ClockOffset: time.Duration(clockOffsetNS), ClockMeasured: clockMeasured,
		ClockSampleAt: sampledAt.UTC(), Source: domain.SourceID(source),
	}
	if err := sample.Validate(); err != nil {
		return domain.MetricSample{}, fmt.Errorf("reconstruct sample: %w", err)
	}
	return sample, nil
}
