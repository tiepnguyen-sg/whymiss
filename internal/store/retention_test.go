package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// Byte-cap prune must trim to the cap, not empty the store.
func TestPrune_ByteCapTrimsNotWipes(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	now := time.Now().UTC()

	const total = 6000
	for i := range total {
		if err := s.WriteObservation(ctx, mustObs(t, 1000, domain.ObsBlockSeen, now.Add(-time.Duration(i)*time.Second), nil)); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	before, err := s.sizeBytes(ctx)
	if err != nil {
		t.Fatalf("sizeBytes: %v", err)
	}

	// Age limit far in the past so only the byte cap can act.
	if err := s.Prune(ctx, 365*24*time.Hour, before/2); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	var remaining int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM observations").Scan(&remaining); err != nil {
		t.Fatalf("count: %v", err)
	}
	if remaining == 0 {
		t.Fatalf("byte-cap prune emptied the store: %d rows -> 0", total)
	}
	if remaining == total {
		t.Errorf("byte-cap prune deleted nothing: still %d rows", remaining)
	}
	after, err := s.sizeBytes(ctx)
	if err != nil {
		t.Fatalf("sizeBytes after prune: %v", err)
	}
	if cap := before / 2; after > cap {
		t.Errorf("live bytes after prune = %d, want <= cap %d", after, cap)
	}
	physical, err := s.physicalSizeBytes()
	if err != nil {
		t.Fatal(err)
	}
	if cap := before / 2; physical > cap {
		t.Errorf("physical bytes after prune = %d, want <= cap %d", physical, cap)
	}
}

func TestDeleteByAge_DeletesGloballyInBoundedBatches(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	base := time.Now().UTC().Add(-time.Hour)

	for i := 0; i < pruneBatchSize+17; i++ {
		at := base.Add(time.Duration(i) * time.Millisecond)
		if i%2 == 0 {
			if err := s.WriteObservation(ctx, mustObs(t, domain.Slot(i), domain.ObsBlockSeen, at, nil)); err != nil {
				t.Fatal(err)
			}
		} else if err := s.WriteSample(ctx, domain.MetricSample{
			At: at, Component: domain.ComponentCL, Name: "peers", Value: float64(i), Source: domain.SourcePromScrape,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.WriteObservation(ctx, mustObs(t, 9999, domain.ObsBlockSeen, base.Add(2*time.Hour), nil)); err != nil {
		t.Fatal(err)
	}

	deleted, err := s.deleteByAge(ctx, base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if deleted != pruneBatchSize+17 {
		t.Fatalf("deleted = %d, want %d", deleted, pruneBatchSize+17)
	}
	if got, err := s.ObservationsForSlot(ctx, 9999); err != nil || len(got) != 1 {
		t.Fatalf("new observation = %+v, err = %v", got, err)
	}
	if info, err := os.Stat(s.path + "-wal"); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	} else if err == nil && info.Size() != 0 {
		t.Errorf("WAL size = %d after batched checkpoints, want 0", info.Size())
	}
}

func TestDeleteOldestBatch_UsesTimestampAcrossTables(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	base := time.Now().UTC()

	// Insert out of chronological order so row IDs cannot stand in for time.
	if err := s.WriteObservation(ctx, mustObs(t, 10, domain.ObsBlockSeen, base.Add(4*time.Second), nil)); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteObservation(ctx, mustObs(t, 11, domain.ObsBlockSeen, base.Add(time.Second), nil)); err != nil {
		t.Fatal(err)
	}
	for _, sample := range []domain.MetricSample{
		{At: base.Add(3 * time.Second), Component: domain.ComponentCL, Name: "peers", Value: 3, Source: domain.SourcePromScrape},
		{At: base.Add(2 * time.Second), Component: domain.ComponentCL, Name: "peers", Value: 2, Source: domain.SourcePromScrape},
	} {
		if err := s.WriteSample(ctx, sample); err != nil {
			t.Fatal(err)
		}
	}

	deleted, err := s.deleteOldestBatch(ctx, 2)
	if err != nil {
		t.Fatalf("deleteOldestBatch: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted = %d, want 2", deleted)
	}
	if got, err := s.ObservationsForSlot(ctx, 11); err != nil || len(got) != 0 {
		t.Errorf("oldest observation remains: %+v (err %v)", got, err)
	}
	if got, err := s.SamplesBetween(ctx, base, base.Add(5*time.Second)); err != nil || len(got) != 1 || got[0].Value != 3 {
		t.Errorf("samples = %+v (err %v), want only timestamp +3s", got, err)
	}
	if got, err := s.ObservationsForSlot(ctx, 10); err != nil || len(got) != 1 {
		t.Errorf("newest observation was removed: %+v (err %v)", got, err)
	}
}

func TestPrune_RejectsImpossibleInputs(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if err := s.Prune(ctx, 0, 1); err == nil {
		t.Error("zero max age: want error")
	}
	if err := s.Prune(ctx, time.Hour, 0); err == nil {
		t.Error("zero max bytes: want error")
	}
}
