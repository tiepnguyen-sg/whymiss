package main

import (
	"context"
	"fmt"
	"strings"
)

// cgroupCPUPeriodUS is the period half of cgroup v2 cpu.max's "<quota> <period>"
// pair, fixed at the kernel's own default (100ms) whenever the requested
// QuotaPercent keeps the quota at or above cgroupCPUMinQuotaUS — one fewer
// degree of freedom to reason about when comparing scenarios by QuotaPercent
// alone. Below that floor the period is widened instead (see quotaAndPeriod).
const cgroupCPUPeriodUS = 100000

// cgroupCPUMinQuotaUS is the smallest quota this host's kernel accepted in
// practice: 100us (QuotaPercent: 0.1 at the 100ms default period) was
// rejected outright — "write cpu.max: invalid argument" — while values at or
// above 1000us worked. This isn't a made-up constant; it's the floor observed
// against a real devnet, not documented as a fixed kernel minimum, so treat
// it as this host's empirical floor rather than a portable guarantee.
const cgroupCPUMinQuotaUS = 1000

// quotaAndPeriod turns a requested percentage into a valid cpu.max "<quota>
// <period>" pair. For QuotaPercent large enough that the default 100ms period
// already yields at least cgroupCPUMinQuotaUS, it uses that period unchanged
// (matches every existing scenario's expectations). For smaller percentages
// — needed because a validator client's BLS signing is cheap enough in
// absolute CPU-time (a few ms) that even a 1%/100ms quota (1ms/period) only
// adds a few hundred ms of wall-clock delay, nowhere near the ~4s attestation
// deadline local.vc_slow needs to cross, verified against a real devnet where
// integer 1% left the duty healthy twice in a row — it holds the quota at the
// floor and widens the period instead, so the ratio still matches what was
// requested without ever asking the kernel for a quota it rejects.
func quotaAndPeriod(percent float64) (quota, period int64) {
	period = cgroupCPUPeriodUS
	quota = int64(float64(period) * percent / 100)
	if quota >= cgroupCPUMinQuotaUS {
		return quota, period
	}
	quota = cgroupCPUMinQuotaUS
	period = int64(float64(quota) * 100 / percent)
	return quota, period
}

// CgroupCPUParams configures a CPU-throttling fault via the cgroup v2 cpu
// controller.
type CgroupCPUParams struct {
	// QuotaPercent caps CPU time to this percentage of one core. Fractional
	// values below 1 are meaningful and sometimes necessary: a validator
	// client's BLS signing is cheap enough in absolute CPU-time (a few ms)
	// that even a 1%-of-100ms-period quota (1ms/period) only stretches it to
	// a few hundred ms of wall-clock delay, nowhere near the ~4s attestation
	// deadline local.vc_slow needs to cross — verified against a real devnet,
	// where integer 1% left the duty healthy twice in a row. Sub-1% values
	// (e.g. 0.1) are required to push that delay into range.
	QuotaPercent float64 `yaml:"quota_percent"`
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
	id, err := dockerContainerID(ctx, enclave, target)
	if err != nil {
		return nil, err
	}
	original, err := readContainerCgroupFile(ctx, id, "cpu.max")
	if err != nil {
		return nil, fmt.Errorf("snapshot cpu.max: %w", err)
	}
	originalValue := strings.TrimSpace(string(original))
	if originalValue == "" {
		return nil, fmt.Errorf("snapshot cpu.max: empty value")
	}
	quota, period := quotaAndPeriod(f.Params.QuotaPercent)
	if err := writeContainerCgroupFile(ctx, id, "cpu.max", fmt.Sprintf("%d %d", quota, period)); err != nil {
		return nil, err
	}

	revert := func(revertCtx context.Context) error {
		return writeContainerCgroupFile(revertCtx, id, "cpu.max", originalValue)
	}
	return revert, nil
}
