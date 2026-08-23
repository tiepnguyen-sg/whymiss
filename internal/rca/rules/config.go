package rules

import (
	"fmt"
	"math"
	"time"
)

// Config carries every documented RCA threshold.
type Config struct {
	Dominance             float64
	ClockOffsetMax        time.Duration
	ClockSampleMaxAge     time.Duration
	NetworkDeviation      time.Duration
	EngineSpikeMultiplier float64
	PeerCountMin          float64
	IOWaitPct             float64
	CPUStealPct           float64
	PSIMemAvg10           float64
}

// DefaultConfig returns the documented safe defaults from docs/causes.md.
func DefaultConfig() Config {
	return Config{
		Dominance:             0.5,
		ClockOffsetMax:        100 * time.Millisecond,
		ClockSampleMaxAge:     2 * time.Minute,
		NetworkDeviation:      750 * time.Millisecond,
		EngineSpikeMultiplier: 3.0,
		PeerCountMin:          40,
		IOWaitPct:             20.0,
		CPUStealPct:           5.0,
		PSIMemAvg10:           10.0,
	}
}

// Validate rejects thresholds outside documented operator-safe ranges.
func (c Config) Validate() error {
	for _, threshold := range []struct {
		name  string
		value float64
	}{
		{"dominance", c.Dominance},
		{"engine spike multiplier", c.EngineSpikeMultiplier},
		{"peer count min", c.PeerCountMin},
		{"iowait pct", c.IOWaitPct},
		{"cpu steal pct", c.CPUStealPct},
		{"psi memory avg10", c.PSIMemAvg10},
	} {
		if math.IsNaN(threshold.value) || math.IsInf(threshold.value, 0) {
			return fmt.Errorf("%s must be finite, got %g", threshold.name, threshold.value)
		}
	}
	switch {
	case c.Dominance < 0.5 || c.Dominance > 0.9:
		return fmt.Errorf("dominance must be between 0.5 and 0.9, got %g", c.Dominance)
	case c.ClockOffsetMax < 10*time.Millisecond || c.ClockOffsetMax > time.Second:
		return fmt.Errorf("clock offset max must be between 10ms and 1s, got %s", c.ClockOffsetMax)
	case c.ClockSampleMaxAge < 30*time.Second || c.ClockSampleMaxAge > 10*time.Minute:
		return fmt.Errorf("clock sample max age must be between 30s and 10m, got %s", c.ClockSampleMaxAge)
	case c.NetworkDeviation < 50*time.Millisecond || c.NetworkDeviation > 5*time.Second:
		return fmt.Errorf("network deviation must be between 50ms and 5s, got %s", c.NetworkDeviation)
	case c.EngineSpikeMultiplier < 1.1 || c.EngineSpikeMultiplier > 20:
		return fmt.Errorf("engine spike multiplier must be between 1.1 and 20, got %g", c.EngineSpikeMultiplier)
	case c.PeerCountMin < 1 || c.PeerCountMin > 500:
		return fmt.Errorf("peer count min must be between 1 and 500, got %g", c.PeerCountMin)
	case c.IOWaitPct < 0 || c.IOWaitPct > 100:
		return fmt.Errorf("iowait pct must be between 0 and 100, got %g", c.IOWaitPct)
	case c.CPUStealPct < 0 || c.CPUStealPct > 100:
		return fmt.Errorf("cpu steal pct must be between 0 and 100, got %g", c.CPUStealPct)
	case c.PSIMemAvg10 < 0 || c.PSIMemAvg10 > 100:
		return fmt.Errorf("psi memory avg10 must be between 0 and 100, got %g", c.PSIMemAvg10)
	default:
		return nil
	}
}
