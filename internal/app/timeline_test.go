package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/store"
)

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

func TestGetTimeline(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "whymiss.db")

	st, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}

	slotStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, obs := range []domain.Observation{
		mustObs(t, 100, domain.ObsDutyAssigned, slotStart.Add(-6*time.Second), map[domain.AttrKey]string{domain.AttrValidatorIndex: "24"}),
		mustObs(t, 100, domain.ObsSlotStart, slotStart, nil),
		mustObs(t, 100, domain.ObsAttestationPublished, slotStart.Add(4*time.Second), map[domain.AttrKey]string{domain.AttrValidatorIndex: "24"}),
	} {
		if err := st.WriteObservation(ctx, obs); err != nil {
			t.Fatalf("WriteObservation: %v", err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	tl, err := GetTimeline(ctx, dbPath, 100, domain.MainnetPreEPBS())
	if err != nil {
		t.Fatalf("GetTimeline: %v", err)
	}
	if tl.Slot != 100 {
		t.Errorf("Slot = %d, want 100", tl.Slot)
	}
	if !tl.SlotStart.Equal(slotStart) {
		t.Errorf("SlotStart = %v, want %v", tl.SlotStart, slotStart)
	}
	if tl.Duty == nil || tl.Duty.ValidatorIndex != 24 {
		t.Errorf("Duty = %+v, want validator 24", tl.Duty)
	}
	if len(tl.Observations) != 3 {
		t.Errorf("got %d observations, want 3", len(tl.Observations))
	}
}

func TestGetTimeline_NoDataForSlot(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "whymiss.db")

	st, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := GetTimeline(ctx, dbPath, 999, domain.MainnetPreEPBS()); err == nil {
		t.Error("GetTimeline: want an error for a slot with no recorded observations, got nil")
	}
}
