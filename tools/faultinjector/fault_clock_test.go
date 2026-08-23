package main

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFormatFaketimeOffset(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		offset time.Duration
		want   string
	}{
		{"positive", 2*time.Second + 500*time.Millisecond, "+2.500000000"},
		{"negative", -500 * time.Millisecond, "-0.500000000"},
		{"subsecond", time.Nanosecond, "+0.000000001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := formatFaketimeOffset(tc.offset); got != tc.want {
				t.Errorf("formatFaketimeOffset(%s) = %q, want %q", tc.offset, got, tc.want)
			}
		})
	}
}

func TestClockSkewAgainstPreloadedContainer(t *testing.T) {
	target := os.Getenv("WHYMISS_CLOCK_TARGET_CONTAINER")
	if os.Getenv("WHYMISS_CLOCK_INTEGRATION") != "1" || target == "" {
		t.Skip("set WHYMISS_CLOCK_INTEGRATION=1 and WHYMISS_CLOCK_TARGET_CONTAINER")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	id, err := dockerContainerID(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	baseline := containerClockDifference(ctx, t, id)

	fault := &ClockSkewFault{Params: ClockSkewParams{Offset: "+2s"}}
	revert, err := fault.Apply(ctx, "", target)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	reverted := false
	t.Cleanup(func() {
		if reverted {
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := revert(cleanupCtx); err != nil {
			t.Errorf("cleanup revert: %v", err)
		}
	})

	skewed := containerClockDifference(ctx, t, id)
	if delta := skewed - baseline; delta < 1500*time.Millisecond || delta > 2500*time.Millisecond {
		t.Fatalf("applied clock delta = %s, want approximately +2s", delta)
	}
	if err := revert(ctx); err != nil {
		t.Fatalf("revert() error = %v", err)
	}
	reverted = true
	restored := containerClockDifference(ctx, t, id)
	if delta := restored - baseline; delta < -500*time.Millisecond || delta > 500*time.Millisecond {
		t.Fatalf("restored clock delta = %s, want approximately 0s", delta)
	}
}

func containerClockDifference(ctx context.Context, t *testing.T, containerID string) time.Duration {
	t.Helper()

	before := time.Now()
	out, err := exec.CommandContext(ctx, "docker", "exec", containerID, "date", "+%s%N").Output()
	after := time.Now()
	if err != nil {
		t.Fatalf("read container clock: %v", err)
	}
	nanos, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		t.Fatalf("parse container clock %q: %v", out, err)
	}
	midpoint := before.Add(after.Sub(before) / 2)
	return time.Unix(0, nanos).Sub(midpoint)
}
