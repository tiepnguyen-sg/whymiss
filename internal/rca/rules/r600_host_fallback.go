package rules

import (
	"fmt"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// HostFallback is R-600: host resource pressure was the dominant
// explanation and no higher-layer rule matched — terminal only, by virtue
// of running last before the R-999 catch-all. One file for all three host
// causes (disk_io, cpu_steal, memory_pressure) since they share the same
// "sampled pressure above a threshold" shape and the same rationale for
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
func (HostFallback) Evaluate(tl domain.Timeline, cfg Config) (*domain.Verdict, bool) {
	if !dutyHasObservableLoss(tl) {
		return nil, false
	}
	if iowait, at, source, ok := hostSampleFact(tl, "iowait_pct", "host_iowait_pct"); ok && iowait > cfg.IOWaitPct {
		return hostVerdict(domain.CauseHostDiskIO,
			fmt.Sprintf("host I/O pressure (PSI some avg10 %.2f%%) exceeded the %.2f%% threshold", iowait, cfg.IOWaitPct),
			iowait, cfg.IOWaitPct, at, source,
			[]string{"identify the process generating the I/O; if it is the execution client, this is really local.el_slow.disk_saturation and the taxonomy should be reviewed", "if it is something else, move that workload off the staking box"},
		), true
	}

	if steal, at, source, ok := hostSampleFact(tl, "cpu_steal_pct", "host_cpu_steal_pct"); ok && steal > cfg.CPUStealPct {
		return hostVerdict(domain.CauseHostCPUSteal,
			fmt.Sprintf("CPU steal time (%.2f%% over the latest sampling interval) exceeded the %.2f%% threshold", steal, cfg.CPUStealPct),
			steal, cfg.CPUStealPct, at, source,
			[]string{"this is a noisy-neighbour problem and is not fixable in software — move to dedicated hardware or a provider with committed CPU"},
		), true
	}

	if mem, at, source, ok := hostSampleFact(tl, "mem_pressure_pct", "host_mem_pressure_pct"); ok && mem > cfg.PSIMemAvg10 {
		return hostVerdict(domain.CauseHostMemoryPressure,
			fmt.Sprintf("memory pressure (PSI some avg10 %.2f%%) exceeded the %.2f%% threshold", mem, cfg.PSIMemAvg10),
			mem, cfg.PSIMemAvg10, at, source,
			[]string{"add RAM or reduce the client cache settings", "never run a validator on a box that swaps"},
		), true
	}

	return nil, false
}

func hostVerdict(cause domain.CauseID, statement string, observed, expected float64, at time.Time, source domain.SourceID, remediation []string) *domain.Verdict {
	return &domain.Verdict{
		Cause:      cause,
		Confidence: domain.ConfidenceMedium, // host pressure is correlational; the causal chain to the missed duty is inferred, not observed (docs/causes.md §7).
		Evidence: []domain.Evidence{{
			At:        at,
			Statement: statement,
			Source:    source,
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
