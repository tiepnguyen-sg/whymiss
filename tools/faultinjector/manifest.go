package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/CHANGEME/whymiss/internal/domain"

	"gopkg.in/yaml.v3"
)

// Manifest is test/corpus/<id>/manifest.yaml: the label on a corpus scenario.
// docs/BUILD_PROMPT.md §4 names the three fields this file must have; the rest
// here are provenance — which fault produced the scenario and against what,
// so a scenario can be regenerated or audited without reading its README.
type Manifest struct {
	ID          string      `yaml:"id"`
	Description string      `yaml:"description"`
	Expect      Expectation `yaml:"expect"`

	Slot           uint64 `yaml:"slot"`
	ValidatorIndex uint64 `yaml:"validator_index"`

	FaultKind   string        `yaml:"fault_kind"`
	FaultTarget string        `yaml:"fault_target"`
	Duration    time.Duration `yaml:"duration"`

	GeneratedAt time.Time `yaml:"generated_at"`
}

// WriteCorpusScenario writes manifest.yaml, observations.jsonl, and README.md
// under dir, which must already exist.
//
// observations.jsonl is one JSON object per line, each the exact wire form of a
// [domain.Observation] — the same shape Phase 2's replay mode (BUILD_PROMPT task
// 2.8) will read back in. Writing through domain.NewObservation's JSON encoding
// rather than a bespoke format means the corpus and the collector agree on the
// format by construction, not by two implementations staying in sync by hand.
func WriteCorpusScenario(dir string, m Manifest, observations []domain.Observation, readme string) error {
	manifestBytes, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), manifestBytes, 0o600); err != nil {
		return fmt.Errorf("write manifest.yaml: %w", err)
	}

	if err := writeObservationsJSONL(filepath.Join(dir, "observations.jsonl"), observations); err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0o600); err != nil {
		return fmt.Errorf("write README.md: %w", err)
	}
	return nil
}

func writeObservationsJSONL(path string, observations []domain.Observation) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create observations.jsonl: %w", err)
	}
	defer func() {
		// This is the file the corpus scenario's facts live in: a Close error
		// (a late-surfacing write failure, e.g. on a full disk) means the file
		// on disk cannot be trusted, so it must not be swallowed the way a
		// read-only Close typically can be.
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close observations.jsonl: %w", cerr)
		}
	}()

	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for i, obs := range observations {
		if err := enc.Encode(obs); err != nil {
			return fmt.Errorf("encode observation %d: %w", i, err)
		}
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush observations.jsonl: %w", err)
	}
	return nil
}
