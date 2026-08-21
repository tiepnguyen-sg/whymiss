package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/CHANGEME/whymiss/internal/domain"
	"github.com/CHANGEME/whymiss/internal/rca"
	_ "github.com/CHANGEME/whymiss/internal/rca/rules" // registers rca.Order via init
	"github.com/CHANGEME/whymiss/internal/store"
)

func TestExplain(t *testing.T) {
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
	} {
		if err := st.WriteObservation(ctx, obs); err != nil {
			t.Fatalf("WriteObservation: %v", err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	v, err := Explain(ctx, dbPath, 100, domain.MainnetPreEPBS())
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if v.Slot != 100 {
		t.Errorf("Slot = %d, want 100", v.Slot)
	}
	if v.Cause != domain.CauseProposerMissed {
		t.Errorf("Cause = %q, want %q (no block_seen, no attestation activity at all)", v.Cause, domain.CauseProposerMissed)
	}
	if v.EngineVersion != rca.EngineVersion {
		t.Errorf("EngineVersion = %q, want %q", v.EngineVersion, rca.EngineVersion)
	}
}

func TestExplain_NoDataForSlot(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "whymiss.db")

	st, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := Explain(ctx, dbPath, 999, domain.MainnetPreEPBS()); err == nil {
		t.Error("Explain: want an error for a slot with no recorded observations, got nil")
	}
}
