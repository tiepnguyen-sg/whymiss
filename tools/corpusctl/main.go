package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/timeline"

	"gopkg.in/yaml.v3"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "corpusctl:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: corpusctl validate <corpus-dir> | corpusctl export --db <path> --slot <n> --out <dir>")
	}
	switch args[0] {
	case "validate":
		if len(args) != 2 {
			return fmt.Errorf("usage: corpusctl validate <corpus-dir>")
		}
		return validateCorpus(args[1])
	case "export":
		return exportObserved(args[1:])
	default:
		return fmt.Errorf("usage: corpusctl validate <corpus-dir> | corpusctl export --db <path> --slot <n> --out <dir>")
	}
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
	if err := validateObservationProvenance(m, observations); err != nil {
		return err
	}
	observationsBytes, err := os.ReadFile(filepath.Join(dir, "observations.jsonl"))
	if err != nil {
		return fmt.Errorf("read observations.jsonl for checksum: %w", err)
	}
	hash := sha256.Sum256(observationsBytes)
	if got := hex.EncodeToString(hash[:]); got != m.ObservationsSHA256 {
		return fmt.Errorf("observations.jsonl sha256 %s does not match manifest %s", got, m.ObservationsSHA256)
	}

	// samples.jsonl is optional — most scenarios record none — but when present
	// every line must be a MetricSample the engine would accept, so a malformed
	// one is caught here rather than at replay time.
	samplesPath := filepath.Join(dir, "samples.jsonl")
	if _, err := timeline.LoadSamples(samplesPath); err != nil {
		return fmt.Errorf("samples.jsonl: %w", err)
	}
	// And pinned, for the same reason observations are. A record's metric
	// samples are evidence: an engine baseline is the value that decides whether
	// R-300 sees a spike, so "it parses" is not enough — it has to be provably
	// the bytes the generator wrote.
	sampleBytes, readErr := os.ReadFile(samplesPath)
	switch {
	case readErr == nil:
		if m.SamplesSHA256 == "" {
			return fmt.Errorf("samples.jsonl exists but manifest has no samples_sha256 to pin it")
		}
		sampleHash := sha256.Sum256(sampleBytes)
		if got := hex.EncodeToString(sampleHash[:]); got != m.SamplesSHA256 {
			return fmt.Errorf("samples.jsonl sha256 %s does not match manifest %s", got, m.SamplesSHA256)
		}
	case errors.Is(readErr, os.ErrNotExist):
		if m.SamplesSHA256 != "" {
			return fmt.Errorf("manifest declares samples_sha256 but samples.jsonl is missing")
		}
	default:
		return fmt.Errorf("read samples.jsonl for checksum: %w", readErr)
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
	if m.CorpusFormatVersion != 2 {
		return fmt.Errorf("manifest.yaml: corpus_format_version = %d, want 2; regenerate this scenario", m.CorpusFormatVersion)
	}
	if m.GeneratorEngineVersion == "" {
		return fmt.Errorf("manifest.yaml: generator_engine_version is required")
	}
	if m.ID == "" {
		return fmt.Errorf("manifest.yaml: id is required")
	}
	if !validCorpusID(m.ID) {
		return fmt.Errorf("manifest.yaml: id %q is invalid", m.ID)
	}
	if m.ID != wantID {
		return fmt.Errorf("manifest.yaml: id %q does not match directory name %q", m.ID, wantID)
	}
	if m.RecipeID != "" && !validCorpusID(m.RecipeID) {
		return fmt.Errorf("manifest.yaml: recipe_id %q is invalid", m.RecipeID)
	}
	if m.Description == "" {
		return fmt.Errorf("manifest.yaml: description is required")
	}
	switch m.Origin {
	case "", "injected":
		if m.FaultKind == "" || m.FaultTarget == "" || m.Duration <= 0 {
			return fmt.Errorf("manifest.yaml: an injected record needs fault_kind, fault_target and a positive duration")
		}
	case "observed":
		// No fault to name. Requiring one would mean inventing it, and an
		// invented fault_kind is worse than an absent one.
		if m.FaultKind != "" || m.FaultTarget != "" || m.Duration > 0 {
			return fmt.Errorf("manifest.yaml: an observed record must not claim a fault it did not inject")
		}
	default:
		return fmt.Errorf("manifest.yaml: origin %q must be \"injected\" or \"observed\"", m.Origin)
	}
	if m.GeneratedAt.IsZero() {
		return fmt.Errorf("manifest.yaml: generated_at is required")
	}
	if len(m.ClockSamples) == 0 {
		return fmt.Errorf("manifest.yaml: complete clock provenance with a positive round_trip is required")
	}
	for i, sample := range m.ClockSamples {
		if sample.Server == "" || sample.SampleAt.IsZero() || sample.RoundTrip <= 0 {
			return fmt.Errorf("manifest.yaml: clock_samples[%d] is incomplete", i)
		}
	}
	decodedHash, err := hex.DecodeString(m.ObservationsSHA256)
	if err != nil || len(decodedHash) != sha256.Size {
		return fmt.Errorf("manifest.yaml: observations_sha256 must be a 64-character SHA-256 digest")
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

func validCorpusID(id string) bool {
	if id == "" || id[0] == '-' || id[len(id)-1] == '-' {
		return false
	}
	for i := range len(id) {
		c := id[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
		if c == '-' && i > 0 && id[i-1] == '-' {
			return false
		}
	}
	return true
}

func validateObservationProvenance(m manifest, observations []domain.Observation) error {
	knownClockSamples := make(map[string]struct{}, len(m.ClockSamples))
	for _, sample := range m.ClockSamples {
		knownClockSamples[clockSampleKey(sample.SampleAt, sample.Offset)] = struct{}{}
	}
	for i, obs := range observations {
		if !obs.ClockMeasured {
			return fmt.Errorf("observations.jsonl line %d: clock was not measured; regenerate this scenario", i+1)
		}
		if _, ok := knownClockSamples[clockSampleKey(obs.ClockSampleAt, obs.ClockOffset)]; !ok {
			return fmt.Errorf("observations.jsonl line %d: clock provenance does not match manifest", i+1)
		}
	}
	return nil
}

func clockSampleKey(sampleAt time.Time, offset time.Duration) string {
	return sampleAt.UTC().Format(time.RFC3339Nano) + "\x00" + offset.String()
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
		if obs.Slot != slot && (obs.Kind != domain.ObsReorg || obs.Slot <= slot || obs.Slot > slot.LastAttestationInclusionSlot()) {
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
