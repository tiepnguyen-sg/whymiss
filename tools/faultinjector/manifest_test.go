package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"

	"gopkg.in/yaml.v3"
)

func TestWriteCorpusScenarioPublishesHashedFiles(t *testing.T) {
	dir := t.TempDir()
	sampledAt := time.Date(2026, 8, 23, 1, 0, 0, 0, time.UTC)
	obs, err := domain.NewObservation(domain.Observation{
		Slot: 1, Kind: domain.ObsSlotStart, At: sampledAt, Source: domain.SourceDerived,
		ClockMeasured: true, ClockSampleAt: sampledAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := Manifest{
		CorpusFormatVersion: 2, GeneratorEngineVersion: "0.2.0",
		ID: "scenario", Description: "real run",
		Expect:    Expectation{Cause: "local.vc_disconnected", Confidence: "high"},
		FaultKind: "pause", FaultTarget: "vc-1", Duration: 20 * time.Second,
		GeneratedAt: sampledAt,
		ClockSamples: []ClockProvenance{{
			Server: "127.0.0.1:123", SampleAt: sampledAt, RoundTrip: time.Millisecond,
		}},
	}
	if err := WriteCorpusScenario(dir, m, []domain.Observation{obs}, "# scenario\n"); err != nil {
		t.Fatalf("WriteCorpusScenario: %v", err)
	}

	observations, err := os.ReadFile(filepath.Join(dir, "observations.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(dir, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var got Manifest
	if err := yaml.Unmarshal(manifestBytes, &got); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(observations)
	if want := hex.EncodeToString(hash[:]); got.ObservationsSHA256 != want {
		t.Errorf("observations_sha256 = %q, want %q", got.ObservationsSHA256, want)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, ".*.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Errorf("temporary files left behind: %v", leftovers)
	}
}
