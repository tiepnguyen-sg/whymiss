package rules

import (
	"math"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// Stages is the latency-budget decomposition docs/causes.md defines.
type Stages struct {
	Propagation time.Duration
	Validation  time.Duration
	Signing     time.Duration

	HasPropagation bool
	HasValidation  bool
	HasSigning     bool
}

// Total is the sum of stages whose endpoints are known.
func (s Stages) Total() time.Duration {
	var total time.Duration
	for _, stage := range []struct {
		value time.Duration
		known bool
	}{
		{value: s.Propagation, known: s.HasPropagation},
		{value: s.Validation, known: s.HasValidation},
		{value: s.Signing, known: s.HasSigning},
	} {
		if !stage.known {
			continue
		}
		if stage.value > 0 && total > time.Duration(math.MaxInt64)-stage.value {
			return time.Duration(math.MaxInt64)
		}
		total += stage.value
	}
	return total
}

// Share returns a stage's fraction of Total and whether it is known.
func (s Stages) Share(stage domain.Stage) (float64, bool) {
	total := s.Total()
	if total <= 0 {
		return 0, false
	}
	switch stage {
	case domain.StagePropagation:
		if !s.HasPropagation {
			return 0, false
		}
		return float64(s.Propagation) / float64(total), true
	case domain.StageValidation:
		if !s.HasValidation {
			return 0, false
		}
		return float64(s.Validation) / float64(total), true
	case domain.StageSigning:
		if !s.HasSigning {
			return 0, false
		}
		return float64(s.Signing) / float64(total), true
	default:
		return 0, false
	}
}

// Dominant reports the greatest known stage when at least two stages are
// comparable and its share meets cfg.Dominance.
func (s Stages) Dominant(cfg Config) (domain.Stage, bool) {
	known := 0
	if s.HasPropagation {
		known++
	}
	if s.HasValidation {
		known++
	}
	if s.HasSigning {
		known++
	}
	if known < 2 {
		return "", false
	}

	best := domain.Stage("")
	var bestShare float64
	found := false
	for _, stage := range domain.Stages() {
		share, ok := s.Share(stage)
		if !ok {
			continue
		}
		if !found || share > bestShare {
			best, bestShare, found = stage, share, true
		}
	}
	if !found || bestShare < cfg.Dominance {
		return "", false
	}
	return best, true
}

// ComputeStages derives the stage decomposition from observations. Without a
// head_updated observation, block_seen-to-attestation_published is retained as
// a combined post-propagation span under Validation; Signing remains unknown.
func ComputeStages(tl domain.Timeline) Stages {
	var s Stages

	blockSeen, hasBlockSeen := timedBlockSeen(tl)
	if hasBlockSeen && !blockSeen.At.Before(tl.SlotStart) {
		s.Propagation = blockSeen.At.Sub(tl.SlotStart)
		s.HasPropagation = true
	} else {
		hasBlockSeen = false
	}

	published, hasPublished := tl.First(domain.ObsAttestationPublished)
	headUpdated, hasHead := tl.First(domain.ObsHeadUpdated)
	if hasBlockSeen && hasHead {
		if !headUpdated.At.Before(blockSeen.At) {
			s.Validation = headUpdated.At.Sub(blockSeen.At)
			s.HasValidation = true
			if hasPublished && !published.At.Before(headUpdated.At) {
				s.Signing = published.At.Sub(headUpdated.At)
				s.HasSigning = true
			}
		}
	} else if hasBlockSeen && hasPublished && !published.At.Before(blockSeen.At) {
		s.Validation = published.At.Sub(blockSeen.At)
		s.HasValidation = true
	}

	return s
}
