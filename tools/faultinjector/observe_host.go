package main

import (
	"context"
	"fmt"
	"math"
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
// Native Linux reads the host filesystem directly; Docker Desktop uses the
// injector's short-lived privileged VM helper. The production binary contains
// neither path.
func SampleIOPressure(ctx context.Context, containerID string) (avg10 float64, err error) {
	return samplePressure(ctx, containerID, "io.pressure")
}

// SampleMemoryPressure reads a container's cgroup v2 memory.pressure "some
// avg10" figure, the memory-reclaim analogue of [SampleIOPressure].
func SampleMemoryPressure(ctx context.Context, containerID string) (avg10 float64, err error) {
	return samplePressure(ctx, containerID, "memory.pressure")
}

// samplePressure locates containerID's cgroup (see containerCgroupDir) and
// parses the "some" line of the named PSI file:
//
//	some avg10=1.23 avg60=0.45 avg300=0.01 total=123456
func samplePressure(ctx context.Context, containerID, psiFile string) (float64, error) {
	content, err := readContainerCgroupFile(ctx, containerID, psiFile)
	if err != nil {
		return 0, err
	}

	avg10, err := parsePSISomeAvg10(string(content))
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", psiFile, err)
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
			if math.IsNaN(avg10) || math.IsInf(avg10, 0) || avg10 < 0 || avg10 > 100 {
				return 0, fmt.Errorf("avg10 value %q must be finite and between 0 and 100", value)
			}
			return avg10, nil
		}
		return 0, fmt.Errorf("%q line has no avg10 field", "some")
	}
	return 0, fmt.Errorf("no %q line in PSI output %q", "some", psiOutput)
}
