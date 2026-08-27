package timeline

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// TestLoadSamplesMissingFileIsNotAnError pins the property every already-recorded
// corpus scenario depends on: samples.jsonl is optional, and a record without one
// replays exactly as it did before the file existed.
func TestLoadSamplesMissingFileIsNotAnError(t *testing.T) {
	t.Parallel()
	got, err := LoadSamples(filepath.Join(t.TempDir(), "samples.jsonl"))
	if err != nil {
		t.Fatalf("LoadSamples on a missing file: %v", err)
	}
	if got != nil {
		t.Errorf("LoadSamples = %v, want nil for a missing file", got)
	}
}

func TestLoadSamplesRoundTrips(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "samples.jsonl")
	line := `{"at":"2026-08-26T00:00:00Z","component":"el","name":"el_engine_calls_p99_ms","value":361,"source":"derived"}`
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSamples(path)
	if err != nil {
		t.Fatalf("LoadSamples: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d samples, want 1", len(got))
	}
	if got[0].Name != "el_engine_calls_p99_ms" || got[0].Value != 361 || got[0].Component != domain.ComponentEL {
		t.Errorf("sample = %+v", got[0])
	}
}

func TestLoadSamplesRejectsAnInvalidSample(t *testing.T) {
	t.Parallel()
	// Caught here rather than at replay: a sample the engine would refuse must
	// never reach a rule as though it were evidence.
	path := filepath.Join(t.TempDir(), "samples.jsonl")
	line := `{"at":"2026-08-26T00:00:00Z","component":"el","name":"el_engine_calls_p99_ms","value":361,"source":"beaconapi"}`
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSamples(path); err == nil {
		t.Fatal("LoadSamples accepted a sample whose source is not permitted for its component")
	}
}

// TestReplayWithSamplesReachesTheTimeline is the point of the whole file: a rule
// that reads tl.Samples rather than an observation could not fire on a corpus
// record before, because a record had no way to carry one. R-300's
// el_engine_calls_p99_ms baseline is exactly that case.
func TestReplayWithSamplesReachesTheTimeline(t *testing.T) {
	t.Parallel()
	slotStart := timeMustParse(t, "2026-08-26T00:00:00Z")
	obs := []domain.Observation{
		mustReplayObs(t, 100, domain.ObsSlotStart, slotStart, domain.SourceDerived),
		mustReplayObs(t, 100, domain.ObsCollectionCompleted, slotStart.Add(15*time.Minute), domain.SourceDerived),
	}
	sample := domain.MetricSample{
		At: slotStart, Component: domain.ComponentEL,
		Name: "el_engine_calls_p99_ms", Value: 361, Source: domain.SourceDerived,
	}

	tl, err := ReplayWithSamples(obs, []domain.MetricSample{sample}, domain.MainnetPreEPBS())
	if err != nil {
		t.Fatalf("ReplayWithSamples: %v", err)
	}
	got, ok := tl.SampleValue(domain.ComponentEL, "el_engine_calls_p99_ms")
	if !ok {
		t.Fatal("the sample did not reach the timeline")
	}
	if got != 361 {
		t.Errorf("SampleValue = %v, want 361", got)
	}

	// Replay is the same call with no samples, and must stay that way.
	plain, err := Replay(obs, domain.MainnetPreEPBS())
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if len(plain.Samples) != 0 {
		t.Errorf("Replay carried %d samples, want none", len(plain.Samples))
	}
}

func mustReplayObs(t *testing.T, slot domain.Slot, kind domain.ObservationKind, at time.Time, source domain.SourceID) domain.Observation {
	t.Helper()
	o, err := domain.NewObservation(domain.Observation{Slot: slot, Kind: kind, At: at, Source: source})
	if err != nil {
		t.Fatalf("NewObservation(%s): %v", kind, err)
	}
	return o
}
