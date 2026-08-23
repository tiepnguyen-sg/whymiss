package hostmetrics

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// MetricCPUStealPct is the percentage of CPU time the hypervisor withheld
// from this guest, over the window between two consecutive Sample calls.
// docs/causes.md's local.host.cpu_steal rule (R-600) reads it via
// thresholds.cpu_steal_pct.
const MetricCPUStealPct domain.MetricName = "host_cpu_steal_pct"

// statPath is /proc/stat's fixed kernel location. Tests pass fixture paths to
// sample directly instead of mutating the production path.
const statPath = "/proc/stat"

// cpuStatFields is how many space-separated fields follow "cpu" on
// /proc/stat's first line, per man 5 proc: user nice system idle iowait irq
// softirq steal guest guest_nice.
const cpuStatFields = 10

// stealFieldIndex is steal's zero-based position among cpuStatFields.
const stealFieldIndex = 7

// CPUSteal computes %steal over successive sampling windows. /proc/stat's
// counters are cumulative since boot, so a single read cannot report a
// percentage — CPUSteal keeps the previous read and reports the delta the
// next time Sample is called, which is why it is a stateful type rather
// than a free function like [SampleIOPressure].
type CPUSteal struct {
	prev    cpuTicks
	hasPrev bool
}

type cpuTicks struct {
	total uint64
	steal uint64
}

// Sample reads /proc/stat and returns %steal since the previous call. ok is
// false on the first call for a given CPUSteal value, since there is no
// prior reading yet to compute a delta against — not an error, just not
// yet meaningful.
func (c *CPUSteal) Sample() (sample domain.MetricSample, ok bool, err error) {
	return c.sample(statPath)
}

func (c *CPUSteal) sample(path string) (sample domain.MetricSample, ok bool, err error) {
	ticks, err := readCPUTicks(path)
	if err != nil {
		return domain.MetricSample{}, false, fmt.Errorf("read %s: %w", path, err)
	}

	prev := c.prev
	hadPrev := c.hasPrev
	c.prev, c.hasPrev = ticks, true
	if !hadPrev {
		return domain.MetricSample{}, false, nil
	}
	if ticks.total < prev.total || ticks.steal < prev.steal {
		return domain.MetricSample{}, false, errors.New("CPU counters decreased between samples")
	}

	totalDelta := ticks.total - prev.total
	stealDelta := ticks.steal - prev.steal
	if totalDelta == 0 {
		return domain.MetricSample{}, false, errors.New("no CPU ticks elapsed between samples")
	}
	if stealDelta > totalDelta {
		return domain.MetricSample{}, false, errors.New("CPU steal delta exceeds total CPU delta")
	}

	pct := float64(stealDelta) / float64(totalDelta) * 100
	out := domain.MetricSample{
		At:        time.Now().UTC(),
		Component: domain.ComponentHost,
		Name:      MetricCPUStealPct,
		Value:     pct,
		Source:    domain.SourceHostMetrics,
	}
	if err := out.Validate(); err != nil {
		return domain.MetricSample{}, false, fmt.Errorf("build sample %s: %w", MetricCPUStealPct, err)
	}
	return out, true, nil
}

// readCPUTicks parses /proc/stat's first line ("cpu  <fields...>", the
// aggregate across all cores).
func readCPUTicks(path string) (cpuTicks, error) {
	content, err := os.ReadFile(path) //nolint:gosec // G304: production passes a fixed path; tests pass private fixture paths
	if err != nil {
		return cpuTicks{}, err
	}

	firstLine, _, _ := strings.Cut(string(content), "\n")
	fields := strings.Fields(firstLine)
	if len(fields) == 0 || fields[0] != "cpu" {
		return cpuTicks{}, fmt.Errorf("first line %q does not start with \"cpu\"", firstLine)
	}
	fields = fields[1:]
	if len(fields) < cpuStatFields {
		return cpuTicks{}, fmt.Errorf("cpu line has %d fields, want at least %d: %q", len(fields), cpuStatFields, firstLine)
	}

	values := make([]uint64, cpuStatFields)
	for i := range cpuStatFields {
		v, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return cpuTicks{}, fmt.Errorf("parse field %d (%q): %w", i, fields[i], err)
		}
		values[i] = v
	}

	// guest and guest_nice are already included in user and nice respectively;
	// summing them again would dilute the reported steal percentage.
	var total uint64
	for _, v := range values[:stealFieldIndex+1] {
		if math.MaxUint64-total < v {
			return cpuTicks{}, errors.New("aggregate CPU tick counter overflow")
		}
		total += v
	}
	return cpuTicks{total: total, steal: values[stealFieldIndex]}, nil
}
