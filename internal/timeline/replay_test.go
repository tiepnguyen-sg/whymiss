package timeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func marshalTimeline(tl domain.Timeline) ([]byte, error) {
	return json.Marshal(tl)
}

func timeMustParse(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return tm.UTC()
}

// corpusPath resolves a test/corpus fixture from this package's directory.
// The corpus scenarios are real, devnet-generated data
// (docs/BUILD_PROMPT.md §8) — replaying them is exactly task 2.8's job, and
// what better proves LoadObservations/Replay agree with
// tools/faultinjector's WriteCorpusScenario than the corpus it actually
// writes.
func corpusPath(elems ...string) string {
	return filepath.Join(append([]string{"..", "..", "test", "corpus"}, elems...)...)
}

func TestLoadObservations_RealCorpusScenario(t *testing.T) {
	obs, err := LoadObservations(corpusPath("vc-slow-cpu", "observations.jsonl"))
	if err != nil {
		t.Fatalf("LoadObservations: %v", err)
	}
	if len(obs) != 5 {
		t.Fatalf("got %d observations, want 5", len(obs))
	}
	if obs[0].Kind != domain.ObsDutyAssigned || obs[1].Kind != domain.ObsSlotStart {
		t.Errorf("first two observations = %q, %q, want duty_assigned, slot_start", obs[0].Kind, obs[1].Kind)
	}
}

func TestReplay_RealCorpusScenario(t *testing.T) {
	obs, err := LoadObservations(corpusPath("vc-slow-cpu", "observations.jsonl"))
	if err != nil {
		t.Fatalf("LoadObservations: %v", err)
	}

	tl, err := Replay(obs, domain.MainnetPreEPBS())
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}

	if tl.Slot != 1959 {
		t.Errorf("Slot = %d, want 1959", tl.Slot)
	}
	if tl.Duty == nil || tl.Duty.ValidatorIndex != 24 {
		t.Errorf("Duty = %+v, want validator 24 recovered from duty_assigned", tl.Duty)
	}
	if len(tl.Observations) != 5 {
		t.Errorf("got %d observations on the timeline, want 5", len(tl.Observations))
	}

	included, ok := tl.Last(domain.ObsAttestationIncluded)
	if !ok {
		t.Fatal("want an attestation_included observation")
	}
	if delay, _ := included.Attr(domain.AttrInclusionDelay); delay != "1" {
		t.Errorf("inclusion_delay = %q, want %q", delay, "1")
	}
}

// allCorpusScenarioIDs lists every subdirectory of test/corpus — one per
// scenario — by reading the directory rather than hardcoding names, so a
// newly added scenario is automatically covered by
// TestReplay_ByteIdenticalAcrossRuns without editing this file.
func allCorpusScenarioIDs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(corpusPath())
	if err != nil {
		t.Fatalf("read test/corpus: %v", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	if len(ids) == 0 {
		t.Fatal("test/corpus has no scenario directories — did the path resolve correctly?")
	}
	return ids
}

// TestReplay_ByteIdenticalAcrossRuns is BUILD_PROMPT.md §10.3's explicit
// DoD: "Replaying every corpus scenario produces byte-identical timelines
// across runs." — every, not a sample, so this walks test/corpus rather
// than naming one scenario.
func TestReplay_ByteIdenticalAcrossRuns(t *testing.T) {
	for _, id := range allCorpusScenarioIDs(t) {
		t.Run(id, func(t *testing.T) {
			obs, err := LoadObservations(corpusPath(id, "observations.jsonl"))
			if err != nil {
				t.Fatalf("LoadObservations: %v", err)
			}

			tl1, err := Replay(obs, domain.MainnetPreEPBS())
			if err != nil {
				t.Fatalf("Replay (run 1): %v", err)
			}
			tl2, err := Replay(obs, domain.MainnetPreEPBS())
			if err != nil {
				t.Fatalf("Replay (run 2): %v", err)
			}

			b1, err := marshalTimeline(tl1)
			if err != nil {
				t.Fatalf("marshal run 1: %v", err)
			}
			b2, err := marshalTimeline(tl2)
			if err != nil {
				t.Fatalf("marshal run 2: %v", err)
			}
			if string(b1) != string(b2) {
				t.Errorf("replaying %s twice produced different output:\nrun 1: %s\nrun 2: %s", id, b1, b2)
			}
		})
	}
}

func TestReplay_MixedSlotsRejected(t *testing.T) {
	a := mustObs(t, 100, domain.ObsSlotStart, timeMustParse(t, "2026-01-01T00:00:00Z"), nil)
	b := mustObs(t, 101, domain.ObsSlotStart, timeMustParse(t, "2026-01-01T00:00:12Z"), nil)
	if _, err := Replay([]domain.Observation{a, b}, domain.MainnetPreEPBS()); err == nil {
		t.Error("Replay: want an error when observations span more than one slot, got nil")
	}
}

func TestReplay_NoSlotStartRejected(t *testing.T) {
	a := mustObs(t, 100, domain.ObsDutyAssigned, timeMustParse(t, "2026-01-01T00:00:00Z"), map[domain.AttrKey]string{domain.AttrValidatorIndex: "1"})
	if _, err := Replay([]domain.Observation{a}, domain.MainnetPreEPBS()); err == nil {
		t.Error("Replay: want an error when there is no slot_start observation, got nil")
	}
}
