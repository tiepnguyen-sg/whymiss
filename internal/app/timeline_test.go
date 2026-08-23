package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/store"
)

func mustObs(t *testing.T, slot domain.Slot, kind domain.ObservationKind, at time.Time, attrs map[domain.AttrKey]string) domain.Observation {
	t.Helper()
	source := domain.SourceBeaconAPI
	if kind == domain.ObsSlotStart || kind == domain.ObsCollectionCompleted {
		source = domain.SourceDerived
	}
	obs, err := domain.NewObservation(domain.Observation{
		Slot: slot, Kind: kind, At: at, Source: source, Attrs: attrs,
		ClockMeasured: true, ClockSampleAt: at,
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
	for _, sample := range []domain.MetricSample{
		{At: slotStart.Add(-time.Second), Component: domain.ComponentHost, Name: "host_mem_pressure_pct", Value: 1, Source: domain.SourceHostMetrics},
		{At: slotStart.Add(13 * time.Second), Component: domain.ComponentHost, Name: "host_mem_pressure_pct", Value: 99, Source: domain.SourceHostMetrics},
	} {
		if err := st.WriteSample(ctx, sample); err != nil {
			t.Fatalf("WriteSample: %v", err)
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
	if len(tl.Samples) != 1 || tl.Samples[0].Value != 1 {
		t.Errorf("Samples = %+v, want only the sample inside the slot evidence window", tl.Samples)
	}
}

func TestGetTimeline_LoadsFutureReorgsForAttester(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "whymiss.db")
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	slotStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, observation := range []domain.Observation{
		mustObs(t, 100, domain.ObsDutyAssigned, slotStart.Add(-time.Second), map[domain.AttrKey]string{domain.AttrValidatorIndex: "24"}),
		mustObs(t, 100, domain.ObsSlotStart, slotStart, nil),
		mustObs(t, 101, domain.ObsReorg, slotStart.Add(12*time.Second), nil),
	} {
		if err := st.WriteObservation(ctx, observation); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	tl, err := GetTimeline(ctx, dbPath, 100, domain.MainnetPreEPBS())
	if err != nil {
		t.Fatal(err)
	}
	if len(tl.Reorgs) != 1 || tl.Reorgs[0].Slot != 101 {
		t.Fatalf("Reorgs = %+v, want slot 101", tl.Reorgs)
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

func TestGetTimeline_IsolatesValidatorsSharingSlot(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "whymiss.db")
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	slotStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	validatorAttrs := func(index string) map[domain.AttrKey]string {
		return map[domain.AttrKey]string{domain.AttrValidatorIndex: index}
	}
	for _, obs := range []domain.Observation{
		mustObs(t, 100, domain.ObsDutyAssigned, slotStart.Add(-6*time.Second), validatorAttrs("24")),
		mustObs(t, 100, domain.ObsDutyAssigned, slotStart.Add(-5*time.Second), validatorAttrs("40")),
		mustObs(t, 100, domain.ObsSlotStart, slotStart, nil),
		mustObs(t, 100, domain.ObsAttestationPublished, slotStart.Add(2*time.Second), validatorAttrs("24")),
		mustObs(t, 100, domain.ObsAttestationPublished, slotStart.Add(3*time.Second), validatorAttrs("40")),
		mustObs(t, 100, domain.ObsCollectionCompleted, slotStart.Add(15*time.Minute), validatorAttrs("24")),
		mustObs(t, 100, domain.ObsCollectionCompleted, slotStart.Add(15*time.Minute+time.Nanosecond), validatorAttrs("40")),
	} {
		if err := st.WriteObservation(ctx, obs); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := GetTimeline(ctx, dbPath, 100, domain.MainnetPreEPBS()); err == nil || !strings.Contains(err.Error(), "select exactly one validator_index") {
		t.Fatalf("ambiguous GetTimeline error = %v", err)
	}
	tl, err := GetTimelineForValidator(ctx, dbPath, 100, 24, domain.MainnetPreEPBS())
	if err != nil {
		t.Fatal(err)
	}
	if tl.Duty == nil || tl.Duty.ValidatorIndex != 24 || tl.Count(domain.ObsCollectionCompleted) != 1 {
		t.Fatalf("selected timeline = %+v", tl)
	}
	for _, obs := range tl.Observations {
		if value, ok := obs.Attr(domain.AttrValidatorIndex); ok && value != "24" {
			t.Fatalf("validator 40 observation leaked into selected timeline: %+v", obs)
		}
	}
	if _, err := GetTimelineForValidator(ctx, dbPath, 100, 99, domain.MainnetPreEPBS()); err == nil {
		t.Fatal("unassigned validator selector was accepted")
	}
}
