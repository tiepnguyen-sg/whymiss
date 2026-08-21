package hostmetrics

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/CHANGEME/whymiss/internal/domain"
)

// psiFixture is the kernel's PSI file format (man 5 proc, CONFIG_PSI) — a
// stable ABI, not a client response this package guesses at. avg10=29.24
// is a real figure this project measured before, via the cgroup-scoped
// form of the same file (test/corpus/host-memory-pressure/observations.jsonl,
// generated against a live devnet with a 128MB memory.high cap); the
// host-wide file this package reads has the identical "some avg10=..."
// line format.
const psiFixture = "some avg10=29.24 avg60=12.05 avg300=3.10 total=987654321\nfull avg10=10.00 avg60=4.02 avg300=1.05 total=123456789\n"

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pressure")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestSampleIOPressure(t *testing.T) {
	restore := ioPressurePath
	ioPressurePath = writeFixture(t, psiFixture)
	defer func() { ioPressurePath = restore }()

	got, err := SampleIOPressure()
	if err != nil {
		t.Fatalf("SampleIOPressure: %v", err)
	}
	if got.Name != MetricIOWaitPct {
		t.Errorf("Name = %q, want %q", got.Name, MetricIOWaitPct)
	}
	if got.Component != domain.ComponentHost {
		t.Errorf("Component = %q, want %q", got.Component, domain.ComponentHost)
	}
	if got.Value != 29.24 {
		t.Errorf("Value = %v, want 29.24", got.Value)
	}
}

func TestSampleMemoryPressure(t *testing.T) {
	restore := memPressurePath
	memPressurePath = writeFixture(t, psiFixture)
	defer func() { memPressurePath = restore }()

	got, err := SampleMemoryPressure()
	if err != nil {
		t.Fatalf("SampleMemoryPressure: %v", err)
	}
	if got.Name != MetricMemPressurePct {
		t.Errorf("Name = %q, want %q", got.Name, MetricMemPressurePct)
	}
	if got.Value != 29.24 {
		t.Errorf("Value = %v, want 29.24", got.Value)
	}
}

func TestSampleIOPressure_Unavailable(t *testing.T) {
	restore := ioPressurePath
	ioPressurePath = filepath.Join(t.TempDir(), "does-not-exist")
	defer func() { ioPressurePath = restore }()

	if _, err := SampleIOPressure(); err == nil {
		t.Error("SampleIOPressure: want an error when the PSI file is absent (I-3: degrade, don't fabricate), got nil")
	}
}
