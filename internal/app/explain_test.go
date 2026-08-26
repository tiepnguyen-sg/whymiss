package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/rca"
	"github.com/tiepnguyen-sg/whymiss/internal/store"
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
		mustObs(t, 100, domain.ObsBlockSkipped, slotStart.Add(36*time.Second), nil),
		// The attestation reaching the chain is what lets R-100 exonerate:
		// without it a proven skip is ambiguous between an upstream failure and
		// a concurrent local one, and the verdict is unknown (ADR-0021).
		mustObs(t, 100, domain.ObsAttestationIncluded, slotStart.Add(24*time.Second), map[domain.AttrKey]string{
			domain.AttrValidatorIndex: "24", domain.AttrInclusionDelay: "1",
			domain.AttrHeadCorrect: "true", domain.AttrTargetCorrect: "true",
		}),
		mustObs(t, 100, domain.ObsCollectionCompleted, slotStart.Add(15*time.Minute), nil),
	} {
		if err := st.WriteObservation(ctx, obs); err != nil {
			t.Fatalf("WriteObservation: %v", err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	v, err := Explain(ctx, dbPath, 100, domain.MainnetPreEPBS(), rca.DefaultConfig())
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if v.Slot != 100 {
		t.Errorf("Slot = %d, want 100", v.Slot)
	}
	if v.Cause != domain.CauseProposerMissed {
		t.Errorf("Cause = %q, want %q (canonical skipped-slot evidence exists)", v.Cause, domain.CauseProposerMissed)
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

	if _, err := Explain(ctx, dbPath, 999, domain.MainnetPreEPBS(), rca.DefaultConfig()); err == nil {
		t.Error("Explain: want an error for a slot with no recorded observations, got nil")
	}
}
