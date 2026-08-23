package hostmetrics

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// Normalised metric names for the two PSI resources this package reads.
// docs/causes.md's local.host.disk_io and local.host.memory_pressure rules
// (R-600) read these via their thresholds.iowait_pct and
// thresholds.psi_mem_avg10 configuration.
const (
	// MetricIOWaitPct is the legacy wire name for the host-wide io.pressure
	// "some avg10" figure. It is PSI I/O stall pressure, not /proc/stat iowait.
	MetricIOWaitPct domain.MetricName = "host_iowait_pct"

	// MetricMemPressurePct is the host-wide memory.pressure "some avg10"
	// figure.
	MetricMemPressurePct domain.MetricName = "host_mem_pressure_pct"
)

// ioPressurePath and memPressurePath are Linux's host-wide PSI files
// (kernel >= 4.20, CONFIG_PSI). The PSI format is a stable kernel ABI;
// tests pass fixture paths directly to samplePressure without mutating these
// production paths.
const (
	ioPressurePath  = "/proc/pressure/io"
	memPressurePath = "/proc/pressure/memory"
)

// SampleIOPressure reads the host-wide io.pressure "some avg10" figure: the
// percentage of the last 10 seconds during which at least one task on the
// whole machine was stalled waiting on I/O.
func SampleIOPressure() (domain.MetricSample, error) {
	return samplePressure(ioPressurePath, MetricIOWaitPct)
}

// SampleMemoryPressure reads the host-wide memory.pressure "some avg10"
// figure, the memory-reclaim analogue of [SampleIOPressure].
func SampleMemoryPressure() (domain.MetricSample, error) {
	return samplePressure(memPressurePath, MetricMemPressurePct)
}

func samplePressure(path string, name domain.MetricName) (domain.MetricSample, error) {
	content, err := os.ReadFile(path) //nolint:gosec // G304: path is one of two fixed package variables, not operator- or attacker-supplied
	if err != nil {
		return domain.MetricSample{}, fmt.Errorf("read %s: %w", path, err)
	}
	avg10, err := parsePSISomeAvg10(string(content))
	if err != nil {
		return domain.MetricSample{}, fmt.Errorf("parse %s: %w", path, err)
	}

	sample := domain.MetricSample{
		At:        time.Now().UTC(),
		Component: domain.ComponentHost,
		Name:      name,
		Value:     avg10,
		Source:    domain.SourceHostMetrics,
	}
	if err := sample.Validate(); err != nil {
		return domain.MetricSample{}, fmt.Errorf("build sample %s: %w", name, err)
	}
	return sample, nil
}

// parsePSISomeAvg10 extracts avg10 from a PSI file's "some" line:
//
//	some avg10=1.23 avg60=0.45 avg300=0.01 total=123456
func parsePSISomeAvg10(psiOutput string) (float64, error) {
	for _, line := range strings.Split(psiOutput, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "some" {
			continue
		}
		for _, field := range fields[1:] {
			value, ok := strings.CutPrefix(field, "avg10=")
			if !ok {
				continue
			}
			avg10, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return 0, fmt.Errorf("parse avg10 value %q: %w", value, err)
			}
			if math.IsNaN(avg10) || math.IsInf(avg10, 0) || avg10 < 0 || avg10 > 100 {
				return 0, fmt.Errorf("avg10 value %q must be finite and between 0 and 100", value)
			}
			return avg10, nil
		}
		return 0, fmt.Errorf("%q line has no avg10 field", "some")
	}
	return 0, fmt.Errorf("no %q line in PSI output %q", "some", psiOutput)
}
