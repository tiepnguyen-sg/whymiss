package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/CHANGEME/whymiss/internal/domain"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "whymiss.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() }) //nolint:errcheck // test cleanup
	return s
}

func mustObs(t *testing.T, slot domain.Slot, kind domain.ObservationKind, at time.Time, attrs map[domain.AttrKey]string) domain.Observation {
	t.Helper()
	obs, err := domain.NewObservation(domain.Observation{
		Slot: slot, Kind: kind, At: at, Source: domain.SourceBeaconAPI, Attrs: attrs,
	})
	if err != nil {
		t.Fatalf("NewObservation: %v", err)
	}
	return obs
}

func TestOpen_AppliesMigrations(t *testing.T) {
	s := openTestStore(t)
	var version int
	if err := s.db.QueryRowContext(context.Background(), "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != len(migrations) {
		t.Errorf("user_version = %d, want %d (all migrations applied)", version, len(migrations))
	}
}

func TestOpen_IsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "whymiss.db")
	s1, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open (first): %v", err)
	}
	s1.Close() //nolint:errcheck // test cleanup

	// Reopening an already-migrated database must not fail or re-run
	// already-applied migrations (which would error: CREATE TABLE on an
	// existing table).
	s2, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open (second, already migrated): %v", err)
	}
	s2.Close() //nolint:errcheck // test cleanup
}

func TestWriteObservation_RoundTrips(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	at := time.Date(2026, 1, 1, 0, 0, 4, 0, time.UTC)
	want := mustObs(t, 100, domain.ObsAttestationPublished, at, map[domain.AttrKey]string{domain.AttrValidatorIndex: "24"})
	if err := s.WriteObservation(ctx, want); err != nil {
		t.Fatalf("WriteObservation: %v", err)
	}

	got, err := s.ObservationsForSlot(ctx, 100)
	if err != nil {
		t.Fatalf("ObservationsForSlot: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d observations, want 1", len(got))
	}
	if got[0].Kind != want.Kind || !got[0].At.Equal(want.At) || got[0].Attrs[domain.AttrValidatorIndex] != "24" {
		t.Errorf("got %+v, want %+v", got[0], want)
	}
}

func TestObservationsForSlot_OnlyReturnsThatSlot(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := s.WriteObservation(ctx, mustObs(t, 100, domain.ObsSlotStart, base, nil)); err != nil {
		t.Fatalf("WriteObservation: %v", err)
	}
	if err := s.WriteObservation(ctx, mustObs(t, 101, domain.ObsSlotStart, base.Add(12*time.Second), nil)); err != nil {
		t.Fatalf("WriteObservation: %v", err)
	}

	got, err := s.ObservationsForSlot(ctx, 100)
	if err != nil {
		t.Fatalf("ObservationsForSlot: %v", err)
	}
	if len(got) != 1 || got[0].Slot != 100 {
		t.Errorf("got %+v, want exactly slot 100's one observation", got)
	}
}

func TestObservationsForSlot_SortedByTime(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Write out of order; the read path must sort.
	if err := s.WriteObservation(ctx, mustObs(t, 100, domain.ObsBlockSeen, base.Add(600*time.Millisecond), nil)); err != nil {
		t.Fatalf("WriteObservation: %v", err)
	}
	if err := s.WriteObservation(ctx, mustObs(t, 100, domain.ObsSlotStart, base, nil)); err != nil {
		t.Fatalf("WriteObservation: %v", err)
	}

	got, err := s.ObservationsForSlot(ctx, 100)
	if err != nil {
		t.Fatalf("ObservationsForSlot: %v", err)
	}
	if len(got) != 2 || got[0].Kind != domain.ObsSlotStart || got[1].Kind != domain.ObsBlockSeen {
		t.Errorf("got %+v, want slot_start before block_seen", got)
	}
}

func TestWriteSample_RoundTrips(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	want := domain.MetricSample{At: at, Component: domain.ComponentEL, Name: "el_engine_newpayload_ms", Value: 2.5, Source: domain.SourcePromScrape}
	if err := s.WriteSample(ctx, want); err != nil {
		t.Fatalf("WriteSample: %v", err)
	}

	got, err := s.SamplesSince(ctx, at.Add(-time.Second))
	if err != nil {
		t.Fatalf("SamplesSince: %v", err)
	}
	if len(got) != 1 || got[0].Value != 2.5 || got[0].Name != want.Name {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestSamplesSince_ExcludesOlder(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := old.Add(time.Hour)
	if err := s.WriteSample(ctx, domain.MetricSample{At: old, Component: domain.ComponentEL, Name: "x", Value: 1, Source: domain.SourcePromScrape}); err != nil {
		t.Fatalf("WriteSample: %v", err)
	}
	if err := s.WriteSample(ctx, domain.MetricSample{At: recent, Component: domain.ComponentEL, Name: "x", Value: 2, Source: domain.SourcePromScrape}); err != nil {
		t.Fatalf("WriteSample: %v", err)
	}

	got, err := s.SamplesSince(ctx, old.Add(time.Minute))
	if err != nil {
		t.Fatalf("SamplesSince: %v", err)
	}
	if len(got) != 1 || got[0].Value != 2 {
		t.Errorf("got %+v, want only the recent sample", got)
	}
}

func TestPrune_ByAge(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	old := time.Now().UTC().Add(-48 * time.Hour)
	recent := time.Now().UTC()
	if err := s.WriteObservation(ctx, mustObs(t, 1, domain.ObsSlotStart, old, nil)); err != nil {
		t.Fatalf("WriteObservation: %v", err)
	}
	if err := s.WriteObservation(ctx, mustObs(t, 2, domain.ObsSlotStart, recent, nil)); err != nil {
		t.Fatalf("WriteObservation: %v", err)
	}

	if err := s.Prune(ctx, 24*time.Hour, 1<<30); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	if got, err := s.ObservationsForSlot(ctx, 1); err != nil || len(got) != 0 {
		t.Errorf("slot 1 (48h old): got %+v (err %v), want pruned", got, err)
	}
	if got, err := s.ObservationsForSlot(ctx, 2); err != nil || len(got) != 1 {
		t.Errorf("slot 2 (recent): got %+v (err %v), want kept", got, err)
	}
}

func TestPrune_ByBytes(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	for i := range domain.Slot(50) {
		obs := mustObs(t, i, domain.ObsAttestationPublished, now.Add(time.Duration(i)*time.Second),
			map[domain.AttrKey]string{domain.AttrValidatorIndex: "24"})
		if err := s.WriteObservation(ctx, obs); err != nil {
			t.Fatalf("WriteObservation: %v", err)
		}
	}

	sizeBefore, err := s.sizeBytes(ctx)
	if err != nil {
		t.Fatalf("sizeBytes: %v", err)
	}

	// A byte budget small enough that not everything fits, but large
	// enough that pruning to fit is possible (not "prune to nothing").
	budget := sizeBefore / 2
	if err := s.Prune(ctx, 365*24*time.Hour, budget); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	remaining, err := s.ObservationsForSlot(ctx, 0)
	if err != nil {
		t.Fatalf("ObservationsForSlot: %v", err)
	}
	if len(remaining) != 0 {
		t.Error("want slot 0 (oldest) pruned once the byte budget forced trimming")
	}

	sizeAfter, err := s.sizeBytes(ctx)
	if err != nil {
		t.Fatalf("sizeBytes: %v", err)
	}
	if sizeAfter > sizeBefore {
		t.Errorf("sizeAfter (%d) > sizeBefore (%d), want pruning to shrink the database", sizeAfter, sizeBefore)
	}
}
