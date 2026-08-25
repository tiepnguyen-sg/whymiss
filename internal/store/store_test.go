package store

import (
	"context"
	"database/sql"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
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
	source := domain.SourceBeaconAPI
	if kind == domain.ObsSlotStart || kind == domain.ObsCollectionCompleted {
		source = domain.SourceDerived
	}
	obs, err := domain.NewObservation(domain.Observation{
		Slot: slot, Kind: kind, At: at, Source: source, Attrs: attrs,
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
	if version != len(schemaMigrations()) {
		t.Errorf("user_version = %d, want %d (all migrations applied)", version, len(schemaMigrations()))
	}
}

func TestOpen_BoundsAndConfiguresEveryConnection(t *testing.T) {
	s := openTestStore(t)
	if got := s.db.Stats().MaxOpenConnections; got != maxStoreConnections {
		t.Fatalf("MaxOpenConnections = %d, want %d", got, maxStoreConnections)
	}

	ctx := context.Background()
	first, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close() //nolint:errcheck // test cleanup
	second, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close() //nolint:errcheck // test cleanup

	for i, conn := range []*sql.Conn{first, second} {
		for pragma, want := range map[string]int{
			"busy_timeout":   busyTimeoutMS,
			"foreign_keys":   1,
			"synchronous":    1, // NORMAL
			"trusted_schema": 0,
		} {
			var got int
			if err := conn.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&got); err != nil {
				t.Fatalf("connection %d PRAGMA %s: %v", i, pragma, err)
			}
			if got != want {
				t.Errorf("connection %d PRAGMA %s = %d, want %d", i, pragma, got, want)
			}
		}
	}
	var autoVacuum int
	if err := s.db.QueryRowContext(ctx, "PRAGMA auto_vacuum").Scan(&autoVacuum); err != nil {
		t.Fatal(err)
	}
	if autoVacuum != autoVacuumIncremental {
		t.Errorf("auto_vacuum = %d, want incremental (%d)", autoVacuum, autoVacuumIncremental)
	}
}

func TestOpen_RejectsFutureSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	dsn, err := sqliteDSN(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), "PRAGMA user_version = 999"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), path); err == nil || !strings.Contains(err.Error(), "newer than this binary supports") {
		t.Fatalf("Open error = %v, want future-schema rejection", err)
	}
}

func TestOpen_RejectsIncompleteCurrentSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "incomplete.db")
	dsn, err := sqliteDSN(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), "PRAGMA user_version = 4"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), path); err == nil || !strings.Contains(err.Error(), "schema mismatch") {
		t.Fatalf("Open error = %v, want incomplete-schema rejection", err)
	}
}

func TestOpenRejectsIndexWithWrongColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wrong-index.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	dsn, err := sqliteDSN(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `DROP INDEX idx_observations_slot; CREATE INDEX idx_observations_slot ON observations(kind)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), path); err == nil || !strings.Contains(err.Error(), "has columns") {
		t.Fatalf("Open error = %v, want wrong-index rejection", err)
	}
}

// TestOpen_AcceptsRelativePathWithSubdirectory guards against a real
// regression: url.URL{Path: p} with a relative p and no Host serializes as
// "file://p", so the RFC 3986 generic syntax reads everything up to the
// first "/" after "//" as the URI's authority, not the path. A flat filename
// like "whymiss.db" happened to still open, which hid this for a long time;
// any relative path with a directory component broke outright ("invalid uri
// authority: <dir>") — found running the Phase 2 soak test with
// --db results/<timestamp>/whymiss.db from a relative working directory.
func TestOpen_AcceptsRelativePathWithSubdirectory(t *testing.T) {
	dir := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	if err := os.Mkdir("subdir", 0o755); err != nil {
		t.Fatal(err)
	}

	s, err := Open(context.Background(), filepath.Join("subdir", "whymiss.db"))
	if err != nil {
		t.Fatalf("Open with a relative path containing a subdirectory: %v", err)
	}
	s.Close() //nolint:errcheck // test cleanup
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

func TestStore_RejectsSlotOutsideSQLiteIntegerRange(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	slot := domain.Slot(math.MaxInt64) + 1
	obs := mustObs(t, slot, domain.ObsBlockSeen, time.Now().UTC(), nil)
	if err := s.WriteObservation(ctx, obs); err == nil {
		t.Fatal("WriteObservation accepted an unrepresentable slot")
	}
	if _, err := s.ObservationsForSlot(ctx, slot); err == nil {
		t.Fatal("ObservationsForSlot accepted an unrepresentable slot")
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

func TestReorgsBetweenSlots_FiltersAndPreservesEventSlot(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, observation := range []domain.Observation{
		mustObs(t, 100, domain.ObsReorg, base, nil),
		mustObs(t, 101, domain.ObsBlockSeen, base.Add(12*time.Second), nil),
		mustObs(t, 101, domain.ObsReorg, base.Add(13*time.Second), nil),
		mustObs(t, 103, domain.ObsReorg, base.Add(36*time.Second), nil),
	} {
		if err := s.WriteObservation(ctx, observation); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.ReorgsBetweenSlots(ctx, 100, 102)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != domain.ObsReorg || got[0].Slot != 101 {
		t.Fatalf("ReorgsBetweenSlots() = %+v, want slot 101 reorg only", got)
	}
}

func TestReorgsBetweenSlots_RejectsUnboundedWindow(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Format(timeLayout)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= maxReorgsPerWindow; i++ {
		if _, err := tx.ExecContext(ctx, `INSERT INTO observations (slot, kind, at, clock_offset_ns, clock_measured, clock_sample_at, source, attrs) VALUES (?, 'reorg', ?, 0, 0, '', 'beaconapi', '{}')`, 101+i, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReorgsBetweenSlots(ctx, 100, 100+maxReorgsPerWindow+1); err == nil || !strings.Contains(err.Error(), "unbounded timeline") {
		t.Fatalf("ReorgsBetweenSlots error = %v, want bounded-read rejection", err)
	}
}

func TestReadPathsRejectUnboundedTimelines(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Format(timeLayout)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= maxObservationsPerSlot; i++ {
		if _, err := tx.ExecContext(ctx, `INSERT INTO observations (slot, kind, at, clock_offset_ns, clock_measured, clock_sample_at, source, attrs) VALUES (100, 'slot_start', ?, 0, 0, '', 'derived', '{}')`, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ObservationsForSlot(ctx, 100); err == nil || !strings.Contains(err.Error(), "unbounded timeline") {
		t.Fatalf("ObservationsForSlot error = %v, want bounded-read rejection", err)
	}

	tx, err = s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= maxSamplesPerRange; i++ {
		if _, err := tx.ExecContext(ctx, `INSERT INTO samples (at, component, name, value, clock_offset_ns, clock_measured, clock_sample_at, source) VALUES (?, 'el', 'x', 1, 0, 0, '', 'promscrape')`, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := s.SamplesBetween(ctx, start, start); err == nil || !strings.Contains(err.Error(), "unbounded timeline") {
		t.Fatalf("SamplesBetween error = %v, want bounded-read rejection", err)
	}
}

func TestWriteSample_RoundTrips(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	want := domain.MetricSample{
		At: at, Component: domain.ComponentEL, Name: "el_engine_newpayload_ms", Value: 2.5,
		ClockOffset: 2 * time.Millisecond, ClockMeasured: true, ClockSampleAt: at.Add(-time.Second),
		Source: domain.SourcePromScrape,
	}
	if err := s.WriteSample(ctx, want); err != nil {
		t.Fatalf("WriteSample: %v", err)
	}

	got, err := s.SamplesBetween(ctx, at.Add(-time.Second), at.Add(time.Second))
	if err != nil {
		t.Fatalf("SamplesBetween: %v", err)
	}
	if len(got) != 1 || got[0].Value != 2.5 || got[0].Name != want.Name ||
		!got[0].ClockMeasured || got[0].ClockOffset != want.ClockOffset || !got[0].ClockSampleAt.Equal(want.ClockSampleAt) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestSamplesBetween_RestrictsBothBounds(t *testing.T) {
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

	got, err := s.SamplesBetween(ctx, old.Add(time.Minute), recent.Add(time.Minute))
	if err != nil {
		t.Fatalf("SamplesBetween: %v", err)
	}
	if len(got) != 1 || got[0].Value != 2 {
		t.Errorf("got %+v, want only the recent sample", got)
	}
	got, err = s.SamplesBetween(ctx, old.Add(-time.Minute), old.Add(time.Minute))
	if err != nil {
		t.Fatalf("SamplesBetween: %v", err)
	}
	if len(got) != 1 || got[0].Value != 1 {
		t.Errorf("got %+v, want only the old sample", got)
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
	for i := range domain.Slot(6000) {
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
