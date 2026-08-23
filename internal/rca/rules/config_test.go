package rules

import (
	"math"
	"testing"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()
	if err := DefaultConfig().Validate(); err != nil {
		t.Fatalf("default config: %v", err)
	}
	cfg := DefaultConfig()
	cfg.Dominance = 1
	if err := cfg.Validate(); err == nil {
		t.Fatal("invalid dominance: want error")
	}
}

func TestConfigValidateRejectsNonFiniteThresholds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"nan dominance", func(cfg *Config) { cfg.Dominance = math.NaN() }},
		{"infinite engine multiplier", func(cfg *Config) { cfg.EngineSpikeMultiplier = math.Inf(1) }},
		{"nan host threshold", func(cfg *Config) { cfg.IOWaitPct = math.NaN() }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := DefaultConfig()
			tc.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want non-finite threshold rejection")
			}
		})
	}
}
