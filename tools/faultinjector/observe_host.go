package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SampleIOPressure reads a container's cgroup v2 io.pressure and returns the
// "some avg10" figure: the percentage of the last 10 seconds during which at
// least one task in the cgroup was stalled waiting on I/O.
//
// This is the same PSI (Pressure Stall Information) mechanism the real
// hostmetrics adapter (internal/source/hostmetrics, Phase 2) will read from
// /proc/pressure/io at the whole-host level; reading it scoped to one
// container's cgroup instead is what lets this tool attribute pressure to the
// specific service a scenario is faulting, rather than the whole devnet host.
// Read directly off the host filesystem — see [CgroupIOFault]'s doc comment
// for why this process runs with the privilege to do that unmediated.
func SampleIOPressure(ctx context.Context, containerID string) (avg10 float64, err error) {
	return samplePressure(containerID, "io.pressure")
}

// SampleMemoryPressure reads a container's cgroup v2 memory.pressure "some
// avg10" figure, the memory-reclaim analogue of [SampleIOPressure]. No fault in
// this tool induces memory pressure yet — this exists so the day one does, the
// observation side is already in place rather than another thing to add then.
func SampleMemoryPressure(ctx context.Context, containerID string) (avg10 float64, err error) {
	return samplePressure(containerID, "memory.pressure")
}

// samplePressure locates containerID's cgroup (see containerCgroupDir) and
// parses the "some" line of the named PSI file:
//
//	some avg10=1.23 avg60=0.45 avg300=0.01 total=123456
func samplePressure(containerID, psiFile string) (float64, error) {
	dir, err := containerCgroupDir(containerID)
	if err != nil {
		return 0, err
	}

	path := filepath.Join(dir, psiFile)
	content, err := os.ReadFile(path) //nolint:gosec // G304: dir comes from a glob under the fixed /sys/fs/cgroup root, not an operator- or attacker-supplied path
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", path, err)
	}

	avg10, err := parsePSISomeAvg10(string(content))
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", path, err)
	}
	return avg10, nil
}

// parsePSISomeAvg10 extracts avg10 from a PSI file's "some" line.
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
			return avg10, nil
		}
		return 0, fmt.Errorf("%q line has no avg10 field", "some")
	}
	return 0, fmt.Errorf("no %q line in PSI output %q", "some", psiOutput)
}
