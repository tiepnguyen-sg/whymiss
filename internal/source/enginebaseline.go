package source

import (
	"math"
	"sort"
)

const (
	// EngineBaselineMinSamples is how many per-slot Engine totals must be
	// collected before a p99 means anything. Shared by the collector and by
	// tools/faultinjector so a corpus record's baseline is computed exactly the
	// way a running deployment would compute it — a record whose baseline was
	// derived from fewer samples would not represent what the daemon records.
	EngineBaselineMinSamples = 32
	// EngineBaselineMaxSamples bounds the rolling window (I-12).
	EngineBaselineMaxSamples = 256
)

// EngineBaseline is a bounded rolling window of per-slot Engine API totals, in
// milliseconds, and the p99 over them. R-300 compares a slot's Engine cost
// against this to tell a spike from a node that is simply always this slow.
type EngineBaseline struct{ totals []float64 }

// Add records one slot's total, ignoring values that are not finite and
// non-negative.
func (b *EngineBaseline) Add(totalMS float64) {
	if totalMS < 0 || math.IsNaN(totalMS) || math.IsInf(totalMS, 0) {
		return
	}
	if len(b.totals) == EngineBaselineMaxSamples {
		copy(b.totals, b.totals[1:])
		b.totals[len(b.totals)-1] = totalMS
		return
	}
	b.totals = append(b.totals, totalMS)
}

// Len reports how many samples the window holds.
func (b *EngineBaseline) Len() int { return len(b.totals) }

// P99 returns the 99th percentile and whether enough samples exist for it to
// mean anything.
func (b *EngineBaseline) P99() (float64, bool) {
	if len(b.totals) < EngineBaselineMinSamples {
		return 0, false
	}
	values := append([]float64(nil), b.totals...)
	sort.Float64s(values)
	index := int(math.Ceil(0.99*float64(len(values)))) - 1
	return values[index], true
}
