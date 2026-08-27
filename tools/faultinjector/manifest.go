package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/clock"
	"github.com/tiepnguyen-sg/whymiss/internal/domain"

	"gopkg.in/yaml.v3"
)

// ClockProvenance identifies the real NTP exchange used to correct every
// observation timestamp in one generated scenario.
type ClockProvenance struct {
	Server    string        `yaml:"server"`
	SampleAt  time.Time     `yaml:"sampled_at"`
	Offset    time.Duration `yaml:"offset"`
	RoundTrip time.Duration `yaml:"round_trip"`
}

// Manifest is test/corpus/<id>/manifest.yaml: the label and reproducibility
// metadata for one real fault-injection run.
type Manifest struct {
	CorpusFormatVersion    int         `yaml:"corpus_format_version"`
	GeneratorEngineVersion string      `yaml:"generator_engine_version"`
	ID                     string      `yaml:"id"`
	RecipeID               string      `yaml:"recipe_id,omitempty"`
	Description            string      `yaml:"description"`
	Expect                 Expectation `yaml:"expect"`

	Slot           uint64 `yaml:"slot"`
	ValidatorIndex uint64 `yaml:"validator_index"`

	FaultKind   string        `yaml:"fault_kind"`
	FaultTarget string        `yaml:"fault_target"`
	Duration    time.Duration `yaml:"duration"`

	GeneratedAt        time.Time         `yaml:"generated_at"`
	ClockSamples       []ClockProvenance `yaml:"clock_samples"`
	ObservationsSHA256 string            `yaml:"observations_sha256"`
	// SamplesSHA256 pins samples.jsonl the same way ObservationsSHA256 pins the
	// observations, and is written only when the record carries samples. Without
	// it a record's metric samples would be the one part of the evidence nobody
	// could prove had not been hand-edited — and an engine baseline is exactly
	// the value that decides whether R-300 sees a spike.
	SamplesSHA256 string `yaml:"samples_sha256,omitempty"`
}

// WriteCorpusScenario replaces a scenario's three files atomically one file at
// a time, publishing manifest.yaml last. The manifest hashes observations.jsonl,
// so an interrupted multi-file update is detected rather than silently accepted.
func WriteCorpusScenario(dir string, m Manifest, observations []domain.Observation, samples []domain.MetricSample, readme string) error {
	if err := validateCorpusWrite(m, observations, readme); err != nil {
		return err
	}
	for i, sample := range samples {
		if err := sample.Validate(); err != nil {
			return fmt.Errorf("sample %d: %w", i, err)
		}
	}
	observationsBytes, err := encodeObservationsJSONL(observations)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(observationsBytes)
	m.ObservationsSHA256 = hex.EncodeToString(hash[:])

	// samples.jsonl is written only when the scenario actually recorded metric
	// samples, so a record without them stays byte-identical to what earlier
	// generations produced and timeline.LoadSamples treats its absence as "none".
	// Both checksums have to be set before the manifest is marshalled, or the
	// manifest on disk would pin observations and say nothing about samples.
	var sampleBytes []byte
	if len(samples) > 0 {
		sampleBytes, err = encodeSamplesJSONL(samples)
		if err != nil {
			return err
		}
		sampleHash := sha256.Sum256(sampleBytes)
		m.SamplesSHA256 = hex.EncodeToString(sampleHash[:])
	}

	manifestBytes, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := atomicWriteFile(filepath.Join(dir, "observations.jsonl"), observationsBytes); err != nil {
		return fmt.Errorf("write observations.jsonl: %w", err)
	}
	if len(sampleBytes) > 0 {
		if err := atomicWriteFile(filepath.Join(dir, "samples.jsonl"), sampleBytes); err != nil {
			return fmt.Errorf("write samples.jsonl: %w", err)
		}
	}
	if err := atomicWriteFile(filepath.Join(dir, "README.md"), []byte(readme)); err != nil {
		return fmt.Errorf("write README.md: %w", err)
	}
	if err := atomicWriteFile(filepath.Join(dir, "manifest.yaml"), manifestBytes); err != nil {
		return fmt.Errorf("write manifest.yaml: %w", err)
	}
	return nil
}

func validateCorpusWrite(m Manifest, observations []domain.Observation, readme string) error {
	if m.CorpusFormatVersion != 2 || m.GeneratorEngineVersion == "" || m.ID == "" || m.Description == "" {
		return fmt.Errorf("manifest identity and corpus format provenance are required")
	}
	if !validScenarioID(m.ID) || (m.RecipeID != "" && !validScenarioID(m.RecipeID)) {
		return fmt.Errorf("manifest id and recipe_id must be valid scenario ids")
	}
	if m.FaultKind == "" || m.FaultTarget == "" || m.Duration <= 0 || m.GeneratedAt.IsZero() {
		return fmt.Errorf("manifest fault provenance is incomplete")
	}
	if len(m.ClockSamples) == 0 {
		return fmt.Errorf("manifest clock provenance is incomplete")
	}
	knownClockSamples := make(map[string]struct{}, len(m.ClockSamples))
	for i, sample := range m.ClockSamples {
		if sample.Server == "" || sample.SampleAt.IsZero() || sample.RoundTrip <= 0 {
			return fmt.Errorf("manifest clock sample %d is incomplete", i)
		}
		knownClockSamples[clockSampleKey(sample.SampleAt, sample.Offset)] = struct{}{}
	}
	if m.Expect.Cause == "" || m.Expect.Confidence == "" {
		return fmt.Errorf("manifest expectation is incomplete")
	}
	if len(observations) == 0 || readme == "" {
		return fmt.Errorf("scenario observations and README must not be empty")
	}
	for i, obs := range observations {
		if err := obs.Validate(); err != nil {
			return fmt.Errorf("validate observation %d: %w", i, err)
		}
		_, known := knownClockSamples[clockSampleKey(obs.ClockSampleAt, obs.ClockOffset)]
		if !obs.ClockMeasured || !known {
			return fmt.Errorf("observation %d clock provenance does not match manifest", i)
		}
	}
	return nil
}

func clockProvenance(readings []clock.Reading) []ClockProvenance {
	out := make([]ClockProvenance, len(readings))
	for i, reading := range readings {
		out[i] = ClockProvenance{
			Server: reading.Server, SampleAt: reading.At.Add(reading.Offset).UTC(),
			Offset: reading.Offset, RoundTrip: reading.RoundTrip,
		}
	}
	return out
}

func clockSampleKey(sampleAt time.Time, offset time.Duration) string {
	return sampleAt.UTC().Format(time.RFC3339Nano) + "\x00" + offset.String()
}

func encodeObservationsJSONL(observations []domain.Observation) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	for i, obs := range observations {
		if err := enc.Encode(obs); err != nil {
			return nil, fmt.Errorf("encode observation %d: %w", i, err)
		}
	}
	return b.Bytes(), nil
}

func atomicWriteFile(path string, content []byte) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			if closeErr := tmp.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
				err = errors.Join(err, fmt.Errorf("close failed temporary file: %w", closeErr))
			}
			if removeErr := os.Remove(tmpPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				err = errors.Join(err, fmt.Errorf("remove failed temporary file: %w", removeErr))
			}
		}
	}()
	if _, err = tmp.Write(content); err != nil {
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err = os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("publish temporary file: %w", err)
	}
	return nil
}

// encodeSamplesJSONL writes one JSON-encoded domain.MetricSample per line, the
// wire form timeline.LoadSamples reads.
func encodeSamplesJSONL(samples []domain.MetricSample) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	for i, sample := range samples {
		if err := encoder.Encode(sample); err != nil {
			return nil, fmt.Errorf("encode sample %d: %w", i, err)
		}
	}
	return buf.Bytes(), nil
}
