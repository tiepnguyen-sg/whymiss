package rca

import "github.com/tiepnguyen-sg/whymiss/internal/rca/rules"

// Config is the threshold set consumed by the ordered RCA rules.
type Config = rules.Config

// DefaultConfig returns the documented safe rule thresholds.
func DefaultConfig() Config { return rules.DefaultConfig() }
