package rca

import (
	"time"

	"github.com/CHANGEME/whymiss/internal/domain"
)

// Stages is the latency-budget decomposition docs/causes.md §3.2 defines:
// propagation (slot start to block seen), validation (block seen to head
// updated), signing (head updated to attestation published). A stage's
// duration is only meaningful once the observation marking its end exists;
// see the doc comments on the exported fields for how a missing endpoint is
// represented.
type Stages struct {
	Propagation time.Duration
	Validation  time.Duration
	Signing     time.Duration

	// HasPropagation is false when block_seen was never observed — the
	// timeline has no evidence a block for this slot ever arrived.
	HasPropagation bool
	// HasValidation is false when head_updated was never observed, even
	// though block_seen was — validation never completed, or the timeline
	// doesn't carry evidence that it did.
	HasValidation bool
	// HasSigning is false when attestation_published was never observed,
	// even though head_updated was.
	HasSigning bool
}

// Total is the sum of every stage this Stages value has an endpoint for.
func (s Stages) Total() time.Duration {
	var total time.Duration
	if s.HasPropagation {
		total += s.Propagation
	}
	if s.HasValidation {
		total += s.Validation
	}
	if s.HasSigning {
		total += s.Signing
	}
	return total
}

// Share returns stage's fraction of Total, and whether that stage's
// duration is known at all.
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

// Dominant reports the stage with the greatest share, and whether that
// share meets cfg.Dominance (docs/causes.md §3.3: "a stage is dominant when
// stage_share(dominant) >= dominance_threshold").
//
// Requires at least two stages to have known durations. With only one
// stage known, its "share" is 100% by construction — not because it's
// actually where the time went, but because there's nothing to compare it
// against. Verified by the golden test: several real scenarios (a frozen
// validator client, never publishing at all) have a known block_seen and
// nothing else, which made Propagation "dominant" for a reason that had
// nothing to do with propagation being slow — it pre-empted R-400
// (local.vc_disconnected) every time, incorrectly.
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

// ComputeStages derives the stage decomposition from tl's observations.
// slot_start is required (domain.Timeline.Validate already guarantees a
// well-formed timeline carries one when it carries any observation) and is
// always present on any Timeline this codebase constructs (internal/timeline's
// Assembler and Replay both require it) — a Timeline missing it entirely is
// not representable by this build, so ComputeStages does not separately
// handle that case.
//
// head_updated degradation: docs/causes.md §3.2 defines validation as
// block_seen to head_updated, and signing as head_updated to
// attestation_published. tools/faultinjector — the only real source of
// observations today — never emits head_updated (only internal/source/beaconapi's
// SSE stream does, for whymiss watch's live collection; task 2.7's watch loop
// doesn't yet track per-duty state to know when to look for it either). Rather
// than let every corpus scenario fall to unknown.insufficient_data for want of
// one observation kind that happens not to be populated yet, ComputeStages
// falls back to treating block_seen-to-attestation_published as one combined
// "post-propagation" span, attributed to Validation (Signing is left unknown
// in that case) — R-300/R-310 lean on engine_call evidence rather than
// sub-splitting this span by time to tell EL slowness from CL slowness, and
// R-410 (vc_slow) doesn't fire without a real head_updated to compare against,
// which is the honest position given this data gap.
func ComputeStages(tl domain.Timeline) Stages {
	var s Stages

	blockSeen, hasBlockSeen := tl.First(domain.ObsBlockSeen)
	if hasBlockSeen {
		s.Propagation = blockSeen.At.Sub(tl.SlotStart)
		s.HasPropagation = true
	}

	published, hasPublished := tl.First(domain.ObsAttestationPublished)

	headUpdated, hasHead := tl.First(domain.ObsHeadUpdated)
	switch {
	case hasBlockSeen && hasHead:
		s.Validation = headUpdated.At.Sub(blockSeen.At)
		s.HasValidation = true
		if hasPublished {
			s.Signing = published.At.Sub(headUpdated.At)
			s.HasSigning = true
		}
	case hasBlockSeen && hasPublished:
		// Degraded case: no head_updated, so validation and signing can't
		// be told apart by time. See the doc comment above.
		s.Validation = published.At.Sub(blockSeen.At)
		s.HasValidation = true
	}

	return s
}
