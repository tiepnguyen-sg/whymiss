package main

import (
	"context"
	"fmt"
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
}

// CgroupMemFault applies memory pressure by writing a container's cgroup v2
// memory.high file — the same host-privileged write path as [CgroupIOFault],
// for the same reason (see its doc comment).
type CgroupMemFault struct {
	Params CgroupMemParams
}

func (f *CgroupMemFault) Apply(ctx context.Context, enclave, target string) (func(context.Context) error, error) {
	id, err := dockerContainerID(ctx, target)
	if err != nil {
		return nil, err
	}
	cgroupDir, err := containerCgroupDir(id)
	if err != nil {
		return nil, err
	}

	if err := writeCgroupFile(cgroupDir, "memory.high", fmt.Sprintf("%d", f.Params.LimitBytes)); err != nil {
		return nil, err
	}

	revert := func(context.Context) error {
		return writeCgroupFile(cgroupDir, "memory.high", "max")
	}
	return revert, nil
}
