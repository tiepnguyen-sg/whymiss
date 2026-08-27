package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// CgroupMemParams configures a memory-pressure fault via the cgroup v2 memory
// controller.
type CgroupMemParams struct {
	// LimitBytes caps memory.high, the soft limit the kernel enforces by
	// throttling and reclaiming rather than OOM-killing the process the way
	// memory.max's hard limit would on breach — reclaim pressure is exactly
	// what docs/causes.md's local.host.memory_pressure rule measures via PSI,
	// and killing the target client mid-scenario would not produce that.
	LimitBytes uint64 `yaml:"limit_bytes"`
	// PressureBytes starts a devnet-only file-cache allocator of this size in
	// the target cgroup. A passive memory.high cap is not sufficient on an idle
	// execution client: it reclaims once and produces no sustained PSI signal.
	PressureBytes uint64 `yaml:"pressure_bytes,omitempty"`
}

// CgroupMemFault applies memory pressure by writing a container's cgroup v2
// memory.high file — the same host-privileged write path as [CgroupIOFault],
// for the same reason (see its doc comment).
type CgroupMemFault struct {
	Params CgroupMemParams
}

func (f *CgroupMemFault) Apply(ctx context.Context, enclave, target string) (func(context.Context) error, error) {
	id, err := dockerContainerID(ctx, enclave, target)
	if err != nil {
		return nil, err
	}
	original, err := readContainerCgroupFile(ctx, id, "memory.high")
	if err != nil {
		return nil, fmt.Errorf("snapshot memory.high: %w", err)
	}
	originalValue := strings.TrimSpace(string(original))
	if originalValue == "" {
		return nil, fmt.Errorf("snapshot memory.high: empty value")
	}
	if err := writeContainerCgroupFile(ctx, id, "memory.high", fmt.Sprintf("%d", f.Params.LimitBytes)); err != nil {
		return nil, err
	}
	var pressureHelper string
	if f.Params.PressureBytes > 0 {
		pressureHelper, err = startCgroupMemoryPressure(ctx, id, f.Params.PressureBytes)
		if err != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			restoreErr := writeContainerCgroupFile(cleanupCtx, id, "memory.high", originalValue)
			return nil, errors.Join(err, restoreErr)
		}
	}

	revert := func(revertCtx context.Context) error {
		var stopErr error
		if pressureHelper != "" {
			stopErr = stopCgroupMemoryPressure(revertCtx, pressureHelper)
		}
		restoreErr := writeContainerCgroupFile(revertCtx, id, "memory.high", originalValue)
		return errors.Join(stopErr, restoreErr)
	}
	return revert, nil
}

func startCgroupMemoryPressure(ctx context.Context, containerID string, pressureBytes uint64) (string, error) {
	name := fmt.Sprintf("whymiss-memory-pressure-%d-%d", os.Getpid(), time.Now().UnixNano())
	// See moveHelperIntoCgroup: this helper used to place itself, and that write
	// failed with EIO on every run, so its allocation was never charged to the
	// target. The PSI values in host-memory-pressure.yaml's bisection log were
	// therefore geth's own reclaim under memory.high, not this helper's doing.
	script := `
set -eu
pressure_bytes="$1"
sleep 2
workdir="$(mktemp -d /tmp/whymiss-memory-pressure.XXXXXX)"
trap 'rm -rf "$workdir"' EXIT INT TERM
count="$(( (pressure_bytes + 1048575) / 1048576 ))"
while :; do
	dd if=/dev/zero of="$workdir/load" bs=1048576 count="$count" >/dev/null 2>&1
	rm -f "$workdir/load"
done
`
	args := []string{
		"run", "--detach", "--rm", "--name", name, "--privileged", "--pid=host",
		dockerDesktopHelperImage, "sh", "-c", script, "memory-pressure",
		fmt.Sprintf("%d", pressureBytes),
	}
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("start cgroup memory pressure helper: %w\n%s", err, out)
	}
	if err := moveHelperIntoCgroup(ctx, name, containerID); err != nil {
		return "", errors.Join(err, stopCgroupMemoryPressure(ctx, name))
	}
	return name, nil
}

func stopCgroupMemoryPressure(ctx context.Context, name string) error {
	if out, err := exec.CommandContext(ctx, "docker", "stop", "--time", "1", name).CombinedOutput(); err != nil {
		return fmt.Errorf("stop cgroup memory pressure helper %s: %w\n%s", name, err, out)
	}
	return nil
}
