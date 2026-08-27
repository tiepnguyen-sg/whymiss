package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"

	"gopkg.in/yaml.v3"
)

func validManifest() Manifest {
	at := time.Date(2026, 8, 23, 1, 0, 0, 0, time.UTC)
	return Manifest{
		CorpusFormatVersion: 2, GeneratorEngineVersion: "0.2.0",
		ID: "scenario", Description: "real run",
		Expect:    Expectation{Cause: "local.vc_disconnected", Confidence: "high"},
		FaultKind: "pause", FaultTarget: "vc-1", Duration: 20 * time.Second,
		GeneratedAt: at,
		ClockSamples: []ClockProvenance{{
			Server: "127.0.0.1:123", SampleAt: at, RoundTrip: time.Millisecond,
		}},
	}
}

func validObservation(t *testing.T) domain.Observation {
	t.Helper()
	at := time.Date(2026, 8, 23, 1, 0, 0, 0, time.UTC)
	obs, err := domain.NewObservation(domain.Observation{
		Slot: 1, Kind: domain.ObsSlotStart, At: at, Source: domain.SourceDerived,
		ClockMeasured: true, ClockSampleAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	return obs
}

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
	if err := WriteCorpusScenario(dir, m, []domain.Observation{obs}, nil, "# scenario\n"); err != nil {
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

// TestWriteCorpusScenarioPinsSamples covers the integrity gap samples.jsonl had
// when it was introduced: observations were checksummed into the manifest and
// samples were not, so the one part of a record that decides whether R-300 sees
// an Engine spike was the one part nobody could prove had not been hand-edited.
func TestWriteCorpusScenarioPinsSamples(t *testing.T) {
	dir := t.TempDir()
	m := validManifest()
	obs := validObservation(t)
	sample := domain.MetricSample{
		At: obs.At, Component: domain.ComponentEL,
		Name: "el_engine_calls_p99_ms", Value: 361, Source: domain.SourceDerived,
	}

	if err := WriteCorpusScenario(dir, m, []domain.Observation{obs}, []domain.MetricSample{sample}, "# scenario\n"); err != nil {
		t.Fatalf("WriteCorpusScenario: %v", err)
	}

	written, err := os.ReadFile(filepath.Join(dir, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "samples_sha256:") {
		t.Fatalf("manifest does not pin samples.jsonl:\n%s", written)
	}

	sampleBytes, err := os.ReadFile(filepath.Join(dir, "samples.jsonl"))
	if err != nil {
		t.Fatalf("samples.jsonl: %v", err)
	}
	want := sha256.Sum256(sampleBytes)
	if !strings.Contains(string(written), hex.EncodeToString(want[:])) {
		t.Error("samples_sha256 in the manifest is not the hash of the bytes written")
	}
}

// A record with no samples must stay exactly as records were before the file
// existed: no samples.jsonl, and no checksum field claiming one.
func TestWriteCorpusScenarioOmitsSamplesWhenThereAreNone(t *testing.T) {
	dir := t.TempDir()
	if err := WriteCorpusScenario(dir, validManifest(), []domain.Observation{validObservation(t)}, nil, "# scenario\n"); err != nil {
		t.Fatalf("WriteCorpusScenario: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "samples.jsonl")); !os.IsNotExist(err) {
		t.Errorf("samples.jsonl was written for a record with no samples (err = %v)", err)
	}
	written, err := os.ReadFile(filepath.Join(dir, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(written), "samples_sha256") {
		t.Errorf("manifest declares samples_sha256 with no samples:\n%s", written)
	}
}
