package hostmetrics

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// psiFixture is the kernel's PSI file format (man 5 proc, CONFIG_PSI) — a
// stable ABI, not a client response this package guesses at. avg10=29.24 sits
// inside the range this project actually measured live, via the cgroup-scoped
// form of the same file: 20.99% at a 512MB memory.high cap up to 58.32% at
// 16MB, logged per cap in tools/faultinjector/scenarios/host-memory-pressure.yaml.
// That recipe's corpus record was dropped (see the recipe for why), so the log
// there — not a corpus fixture — is the surviving record of those figures. The
// host-wide file this package reads has the identical "some avg10=..." line
// format.
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
	t.Parallel()

	got, err := samplePressure(writeFixture(t, psiFixture), MetricIOWaitPct)
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
	t.Parallel()

	got, err := samplePressure(writeFixture(t, psiFixture), MetricMemPressurePct)
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
	t.Parallel()

	if _, err := samplePressure(filepath.Join(t.TempDir(), "does-not-exist"), MetricIOWaitPct); err == nil {
		t.Error("SampleIOPressure: want an error when the PSI file is absent (I-3: degrade, don't fabricate), got nil")
	}
}

func TestParsePSISomeAvg10RejectsImpossibleValues(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"-0.01", "100.01", "NaN", "+Inf"} {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			if _, err := parsePSISomeAvg10("some avg10=" + value + " avg60=0 avg300=0 total=0\n"); err == nil {
				t.Fatalf("parsePSISomeAvg10(%q): want error", value)
			}
		})
	}
}
