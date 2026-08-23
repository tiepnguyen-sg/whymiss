package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeScenarioFixture(t *testing.T, root, id string, manifestYAML, observationsJSONL string) string {
	t.Helper()

	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	hash := sha256.Sum256([]byte(observationsJSONL))
	manifestYAML = strings.ReplaceAll(manifestYAML, "OBS_SHA", hex.EncodeToString(hash[:]))
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatalf("write manifest.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "observations.jsonl"), []byte(observationsJSONL), 0o644); err != nil {
		t.Fatalf("write observations.jsonl: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# "+id+"\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	return dir
}

const validManifest = `
corpus_format_version: 2
generator_engine_version: 0.2.0
id: ok-scenario
description: a fixture that should pass
slot: 100
validator_index: 24
fault_kind: pause
fault_target: vc-1-geth-lighthouse
duration: 20s
generated_at: 2026-08-20T07:00:00Z
clock_samples:
  - server: 127.0.0.1:123
    sampled_at: 2026-08-20T07:00:00Z
    offset: 0s
    round_trip: 1ms
observations_sha256: OBS_SHA
expect:
  cause: local.vc_disconnected
  confidence: high
`

const validObservations = `{"slot":100,"kind":"slot_start","at":"2026-08-20T07:00:00Z","clock_offset":0,"clock_measured":true,"clock_sample_at":"2026-08-20T07:00:00Z","source":"derived"}
{"slot":100,"kind":"duty_assigned","at":"2026-08-20T07:00:01Z","clock_offset":0,"clock_measured":true,"clock_sample_at":"2026-08-20T07:00:00Z","source":"beaconapi","attrs":{"validator_index":"24"}}
`

func TestValidateCorpusAccepts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeScenarioFixture(t, root, "ok-scenario", validManifest, validObservations)

	if err := validateCorpus(root); err != nil {
		t.Fatalf("validateCorpus() = %v, want nil", err)
	}
}

func TestValidateCorpusRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		manifest     string
		observations string
	}{
		{
			name: "id does not match directory name",
			manifest: `id: wrong-id
description: d
slot: 1
expect: {cause: local.vc_disconnected, confidence: high}`,
			observations: `{"slot":1,"kind":"slot_start","at":"2026-08-20T07:00:00Z","clock_offset":0,"source":"derived"}`,
		},
		{
			name: "cause not in taxonomy",
			manifest: `id: bad-scenario
description: d
slot: 1
expect: {cause: local.made_up_cause, confidence: high}`,
			observations: `{"slot":1,"kind":"slot_start","at":"2026-08-20T07:00:00Z","clock_offset":0,"source":"derived"}`,
		},
		{
			name: "sub_cause not a descendant of cause",
			manifest: `id: bad-scenario
description: d
slot: 1
expect: {cause: local.cl_slow, sub_cause: local.el_slow.pruning, confidence: high}`,
			observations: `{"slot":1,"kind":"slot_start","at":"2026-08-20T07:00:00Z","clock_offset":0,"source":"derived"}`,
		},
		{
			name: "invalid confidence",
			manifest: `id: bad-scenario
description: d
slot: 1
expect: {cause: local.vc_disconnected, confidence: extremely_sure}`,
			observations: `{"slot":1,"kind":"slot_start","at":"2026-08-20T07:00:00Z","clock_offset":0,"source":"derived"}`,
		},
		{
			name: "observation slot mismatches manifest slot",
			manifest: `id: bad-scenario
description: d
slot: 1
expect: {cause: local.vc_disconnected, confidence: high}`,
			observations: `{"slot":999,"kind":"slot_start","at":"2026-08-20T07:00:00Z","clock_offset":0,"source":"derived"}`,
		},
		{
			name: "observations out of order",
			manifest: `id: bad-scenario
description: d
slot: 1
expect: {cause: local.vc_disconnected, confidence: high}`,
			observations: `{"slot":1,"kind":"slot_start","at":"2026-08-20T07:00:05Z","clock_offset":0,"source":"derived"}
{"slot":1,"kind":"duty_assigned","at":"2026-08-20T07:00:00Z","clock_offset":0,"source":"beaconapi","attrs":{"validator_index":"1"}}`,
		},
		{
			name: "no observations at all",
			manifest: `id: bad-scenario
description: d
slot: 1
expect: {cause: local.vc_disconnected, confidence: high}`,
			observations: ``,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			writeScenarioFixture(t, root, "bad-scenario", tc.manifest, tc.observations)

			if err := validateCorpus(root); err == nil {
				t.Error("validateCorpus() = nil, want an error")
			}
		})
	}
}

func TestValidateCorpusRequiresAtLeastOneScenario(t *testing.T) {
	t.Parallel()

	if err := validateCorpus(t.TempDir()); err == nil {
		t.Error("validateCorpus() = nil for an empty corpus dir, want an error")
	}
}

func TestValidateCorpusReportsMultipleFailures(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeScenarioFixture(t, root, "ok-scenario", validManifest, validObservations)
	writeScenarioFixture(t, root, "broken-one", `id: broken-one
description: d
slot: 1
expect: {cause: not.a.real.cause, confidence: high}`, `{"slot":1,"kind":"slot_start","at":"2026-08-20T07:00:00Z","clock_offset":0,"source":"derived"}`)
	writeScenarioFixture(t, root, "broken-two", `id: broken-two
description: d
slot: 1
expect: {cause: not.a.real.cause, confidence: high}`, `{"slot":1,"kind":"slot_start","at":"2026-08-20T07:00:00Z","clock_offset":0,"source":"derived"}`)

	err := validateCorpus(root)
	if err == nil {
		t.Fatal("validateCorpus() = nil, want an error naming both broken scenarios")
	}
	msg := err.Error()
	for _, want := range []string{"broken-one", "broken-two"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
}

func TestManifestGeneratedAtRoundTrips(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeScenarioFixture(t, root, "ok-scenario", validManifest, validObservations)

	m, err := loadManifest(filepath.Join(root, "ok-scenario", "manifest.yaml"))
	if err != nil {
		t.Fatalf("loadManifest() error = %v", err)
	}
	want := time.Date(2026, 8, 20, 7, 0, 0, 0, time.UTC)
	if !m.GeneratedAt.Equal(want) {
		t.Errorf("GeneratedAt = %v, want %v", m.GeneratedAt, want)
	}
}
