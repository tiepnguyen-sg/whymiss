package rca

import "time"

// Config carries every `thresholds.*` value docs/causes.md §5 defines. A
// rule must never contain a magic number — every comparison a rule makes
// reads its threshold from here.
type Config struct {
	// Dominance is the stage share required for a stage to be considered
	// dominant (docs/causes.md §3.3: `dominant = argmax stage_share`,
	// gated by `stage_share(dominant) >= Dominance`).
	Dominance float64

	// ClockOffsetMax is the NTP offset above which timing rules are
	// suppressed (I-9, R-011).
	ClockOffsetMax time.Duration

	// NetworkDeviation is the maximum |local - network_p50| block-arrival
	// gap still counted as "the same lateness" for network.late_block
	// (R-110).
	NetworkDeviation time.Duration

	// EngineSpikeMultiplier is how many times a node's own rolling p99
	// Engine API duration must be exceeded to count as a spike (R-300).
	EngineSpikeMultiplier float64

	// PeerCountMin is the peer count below which p2p is considered
	// degraded (R-200).
	PeerCountMin float64

	// SubnetPeerMin is the minimum peer count on the relevant attestation
	// subnet (R-200).
	SubnetPeerMin float64

	// IOWaitPct is the host disk-pressure PSI `avg10` percentage above
	// which disk I/O is implicated (R-300's disk_saturation sub-cause,
	// R-600).
	IOWaitPct float64

	// CPUStealPct is the hypervisor CPU-steal percentage above which the
	// host is implicated (R-600).
	CPUStealPct float64

	// PSIMemAvg10 is the host memory-pressure PSI `avg10` percentage above
	// which memory pressure is implicated (R-600).
	PSIMemAvg10 float64
}

// DefaultConfig returns the documented safe defaults from docs/causes.md
// §5, verbatim.
func DefaultConfig() Config {
	return Config{
		Dominance:             0.5,
		ClockOffsetMax:        100 * time.Millisecond,
		NetworkDeviation:      750 * time.Millisecond,
		EngineSpikeMultiplier: 3.0,
		PeerCountMin:          40,
		SubnetPeerMin:         2,
		IOWaitPct:             20.0,
		CPUStealPct:           5.0,
		PSIMemAvg10:           10.0,
	}
}
