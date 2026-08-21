package rules

import (
	"fmt"

	"github.com/CHANGEME/whymiss/internal/domain"
	"github.com/CHANGEME/whymiss/internal/rca"
)

// HostFallback is R-600: host resource pressure was the dominant
// explanation and no higher-layer rule matched — terminal only, by virtue
// of running last before the R-999 catch-all. One file for all three host
// causes (disk_io, cpu_steal, memory_pressure) since they share the same
// "sustained pressure above a threshold" shape and the same rationale for
// living together (docs/causes.md §6: host signals are corroboration
// inside R-300/R-310 first, and only become a terminal cause here).
//
// Checked in this order — disk, then CPU, then memory — matching
// docs/causes.md §7's listing order; not a claim that one is more likely
// than another.
type HostFallback struct{}

// ID returns R-600.
func (HostFallback) ID() string { return "R-600" }

// Evaluate implements rca.Rule.
func (HostFallback) Evaluate(tl domain.Timeline, cfg rca.Config) (*domain.Verdict, bool) {
	if iowait, ok := hostSampledValue(tl, "iowait_pct", "host_iowait_pct"); ok && iowait > cfg.IOWaitPct {
		return hostVerdict(tl, domain.CauseHostDiskIO,
			fmt.Sprintf("host disk I/O pressure (iowait %.2f%%) exceeded the %.2f%% threshold, sustained across the collection window", iowait, cfg.IOWaitPct),
			iowait, cfg.IOWaitPct,
			[]string{"identify the process generating the I/O; if it is the execution client, this is really local.el_slow.disk_saturation and the taxonomy should be reviewed", "if it is something else, move that workload off the staking box"},
		), true
	}

	if steal, ok := hostSampledValue(tl, "cpu_steal_pct", "host_cpu_steal_pct"); ok && steal > cfg.CPUStealPct {
		return hostVerdict(tl, domain.CauseHostCPUSteal,
			fmt.Sprintf("CPU steal time (%.2f%%) exceeded the %.2f%% threshold, sustained across the collection window", steal, cfg.CPUStealPct),
			steal, cfg.CPUStealPct,
			[]string{"this is a noisy-neighbour problem and is not fixable in software — move to dedicated hardware or a provider with committed CPU"},
		), true
	}

	if mem, ok := hostSampledValue(tl, "mem_pressure_pct", "host_mem_pressure_pct"); ok && mem > cfg.PSIMemAvg10 {
		return hostVerdict(tl, domain.CauseHostMemoryPressure,
			fmt.Sprintf("memory pressure (PSI avg10 %.2f%%) exceeded the %.2f%% threshold, sustained across the collection window", mem, cfg.PSIMemAvg10),
			mem, cfg.PSIMemAvg10,
			[]string{"add RAM or reduce the client cache settings", "never run a validator on a box that swaps"},
		), true
	}

	return nil, false
}

func hostVerdict(tl domain.Timeline, cause domain.CauseID, statement string, observed, expected float64, remediation []string) *domain.Verdict {
	return &domain.Verdict{
		Cause:      cause,
		Confidence: domain.ConfidenceMedium, // host pressure is correlational; the causal chain to the missed duty is inferred, not observed (docs/causes.md §7).
		Evidence: []domain.Evidence{{
			At:        tl.SlotStart,
			Statement: statement,
			Source:    domain.SourceHostMetrics,
			Comparison: &domain.Comparison{
				Label:    string(cause),
				Observed: observed,
				Expected: expected,
				Unit:     domain.UnitPercent,
			},
		}},
		Remediation: remediation,
	}
}
