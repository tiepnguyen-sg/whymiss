package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/CHANGEME/whymiss/internal/domain"

	"gopkg.in/yaml.v3"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "corpusctl:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 2 || args[0] != "validate" {
		return fmt.Errorf("usage: corpusctl validate <corpus-dir>")
	}
	return validateCorpus(args[1])
}

// validateCorpus checks every scenario directory under root and reports every
// failure found, rather than stopping at the first — a corpus run covers dozens
// of scenarios, and fixing them one failed run at a time does not scale.
func validateCorpus(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("read corpus dir %s: %w", root, err)
	}

	var failures []string
	checked := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		checked++
		dir := filepath.Join(root, entry.Name())
		if err := validateScenario(dir, entry.Name()); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", entry.Name(), err))
		}
	}

	if checked == 0 {
		return fmt.Errorf("no scenario directories found under %s", root)
	}
	if len(failures) > 0 {
		return fmt.Errorf("%d of %d scenarios failed validation:\n  %s",
			len(failures), checked, strings.Join(failures, "\n  "))
	}
	fmt.Printf("corpusctl: %d scenarios OK\n", checked)
	return nil
}

func validateScenario(dir, wantID string) error {
	m, err := loadManifest(filepath.Join(dir, "manifest.yaml"))
	if err != nil {
		return err
	}
	if err := validateManifest(m, wantID); err != nil {
		return err
	}

	observations, err := loadObservations(filepath.Join(dir, "observations.jsonl"), domain.Slot(m.Slot))
	if err != nil {
		return err
	}
	if len(observations) == 0 {
		return fmt.Errorf("observations.jsonl has no observations")
	}

	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		return fmt.Errorf("README.md: %w", err)
	}
	return nil
}

func loadManifest(path string) (manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return manifest{}, fmt.Errorf("read manifest.yaml: %w", err)
	}
	var m manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return manifest{}, fmt.Errorf("parse manifest.yaml: %w", err)
	}
	return m, nil
}

func validateManifest(m manifest, wantID string) error {
	if m.ID == "" {
		return fmt.Errorf("manifest.yaml: id is required")
	}
	if m.ID != wantID {
		return fmt.Errorf("manifest.yaml: id %q does not match directory name %q", m.ID, wantID)
	}
	if m.Description == "" {
		return fmt.Errorf("manifest.yaml: description is required")
	}

	cause := domain.CauseID(m.Expect.Cause)
	if m.Expect.Cause == "" {
		return fmt.Errorf("manifest.yaml: expect.cause is required")
	}
	if !cause.Valid() {
		return fmt.Errorf("manifest.yaml: expect.cause %q is not in the taxonomy (docs/causes.md)", m.Expect.Cause)
	}
	if m.Expect.SubCause != "" {
		sub := domain.CauseID(m.Expect.SubCause)
		if !sub.Valid() {
			return fmt.Errorf("manifest.yaml: expect.sub_cause %q is not in the taxonomy", m.Expect.SubCause)
		}
		if !sub.IsSubCauseOf(cause) {
			return fmt.Errorf("manifest.yaml: expect.sub_cause %q is not a sub-cause of expect.cause %q",
				m.Expect.SubCause, m.Expect.Cause)
		}
	}

	switch domain.Confidence(m.Expect.Confidence) {
	case domain.ConfidenceHigh, domain.ConfidenceMedium, domain.ConfidenceLow:
	default:
		return fmt.Errorf("manifest.yaml: expect.confidence %q is not high/medium/low", m.Expect.Confidence)
	}
	return nil
}

// loadObservations decodes observations.jsonl and enforces the same invariants
// domain.Timeline will (ascending timestamps, every observation belongs to
// slot) — catching a malformed corpus fixture here is cheaper than watching
// Phase 2's replay mode (BUILD_PROMPT task 2.8) fail on it later.
func loadObservations(path string, slot domain.Slot) ([]domain.Observation, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open observations.jsonl: %w", err)
	}
	defer f.Close() //nolint:errcheck // read/write already completed by the time this runs; nothing actionable on Close failure

	var out []domain.Observation
	scanner := bufio.NewScanner(f)
	for lineNum := 1; scanner.Scan(); lineNum++ {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var draft domain.Observation
		if err := json.Unmarshal([]byte(line), &draft); err != nil {
			return nil, fmt.Errorf("observations.jsonl line %d: decode: %w", lineNum, err)
		}
		obs, err := domain.NewObservation(draft)
		if err != nil {
			return nil, fmt.Errorf("observations.jsonl line %d: %w", lineNum, err)
		}
		if obs.Slot != slot {
			return nil, fmt.Errorf("observations.jsonl line %d: slot %d does not match manifest slot %d",
				lineNum, obs.Slot, slot)
		}
		out = append(out, obs)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan observations.jsonl: %w", err)
	}

	for i := 1; i < len(out); i++ {
		if out[i].At.Before(out[i-1].At) {
			return nil, fmt.Errorf("observations.jsonl: line %d (%s) precedes line %d (%s) — must be sorted ascending",
				i+1, out[i].At, i, out[i-1].At)
		}
	}
	return out, nil
}
