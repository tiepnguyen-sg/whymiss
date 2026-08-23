package rca

import (
	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/rca/rules"
)

// Stages is the latency-budget decomposition consumed by RCA rules.
type Stages = rules.Stages

// ComputeStages derives the latency-budget decomposition from tl.
func ComputeStages(tl domain.Timeline) Stages { return rules.ComputeStages(tl) }
