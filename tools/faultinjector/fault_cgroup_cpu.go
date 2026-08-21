package main

import (
	"context"
	"fmt"
)

// cgroupCPUPeriodUS is the period half of cgroup v2 cpu.max's "<quota> <period>"
// pair, fixed at the kernel's own default (100ms) rather than exposed as a
// scenario knob — one fewer degree of freedom to reason about when comparing
// scenarios by QuotaPercent alone.
const cgroupCPUPeriodUS = 100000

// CgroupCPUParams configures a CPU-throttling fault via the cgroup v2 cpu
// controller.
type CgroupCPUParams struct {
	// QuotaPercent caps CPU time to this percentage of one core.
	QuotaPercent uint64 `yaml:"quota_percent"`
}

// CgroupCPUFault throttles a container's CPU time by writing its cgroup v2
// cpu.max file — the same host-privileged write path as [CgroupIOFault], for
// the same reason (see its doc comment). Unlike io.max against this project's
// devnet workload (verified: zero effect on duty timing across four throttle
// severities, since geth's engine-call hot path has no synchronous disk write
// in it here), CPU throttling gates the state-transition and block-validation
// work directly, which is unavoidably CPU-bound — the mechanism this taxonomy
// actually needs for local.el_slow and local.cl_slow.
type CgroupCPUFault struct {
	Params CgroupCPUParams
}

func (f *CgroupCPUFault) Apply(ctx context.Context, enclave, target string) (func(context.Context) error, error) {
	id, err := dockerContainerID(ctx, target)
	if err != nil {
		return nil, err
	}
	cgroupDir, err := containerCgroupDir(id)
	if err != nil {
		return nil, err
	}

	quota := cgroupCPUPeriodUS * f.Params.QuotaPercent / 100
	if quota == 0 {
		quota = 1000 // cpu.max rejects 0 as invalid, same as io.max's rbps/wbps=0
	}
	if err := writeCgroupFile(cgroupDir, "cpu.max", fmt.Sprintf("%d %d", quota, cgroupCPUPeriodUS)); err != nil {
		return nil, err
	}

	revert := func(context.Context) error {
		return writeCgroupFile(cgroupDir, "cpu.max", fmt.Sprintf("max %d", cgroupCPUPeriodUS))
	}
	return revert, nil
}
