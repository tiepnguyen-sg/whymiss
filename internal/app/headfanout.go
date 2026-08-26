package app

import (
	"log/slog"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// headFanout hands one head_updated observation to every collector that derives
// per-slot evidence from it.
//
// Both paths that can discover a head must publish through this one type. The
// event stream sees heads for every slot; duty tracking's REST poll sees them
// only for a tracked duty's slot, but it is the only path that exists at all on
// a node whose Beacon API does not serve /eth/v1/events — a gateway answering
// 501 there is a real deployment, not a hypothetical one.
//
// Wiring a collector to only one path is silent: the daemon logs nothing, the
// collector simply never runs, and the rules that need its observation decline
// forever. That is what happened to the network baseline, which was fed from the
// event stream alone while block timing had already gained the REST fallback —
// so on an SSE-less node tl.Network stayed nil, R-110 and R-200 always declined,
// and every "was it the network or me?" question fell through to
// unknown.insufficient_data. Adding a collector here rather than at a call site
// is what keeps the two paths from drifting apart again.
type headFanout struct {
	// timing receives heads for block-timing and Engine-call sampling; nil
	// when --cl-metrics-api is unset.
	timing chan domain.Observation
	// baseline receives heads for independent-node baseline sampling; nil
	// when --baseline-beacon-api is unset.
	baseline chan domain.Observation
}

// send offers head to every wired collector without blocking.
//
// A full channel means that collector's previous scrape has not finished. The
// sample is dropped and logged rather than queued: an unbounded queue would
// violate I-12's bounded memory, and blocking here would let a slow metrics
// endpoint delay the collection loop that feeds it, which I-5 forbids. A
// dropped sample degrades the affected rule to unknown, which I-8 prefers to a
// verdict built from stale evidence.
func (f *headFanout) send(head domain.Observation, logger *slog.Logger) {
	if f == nil || head.Kind != domain.ObsHeadUpdated {
		return
	}
	for _, target := range []struct {
		jobs chan domain.Observation
		what string
	}{
		{f.timing, "block timing"},
		{f.baseline, "network baseline"},
	} {
		if target.jobs == nil {
			continue
		}
		select {
		case target.jobs <- head:
		default:
			logger.Warn("drop sample; previous scrape still pending", "collector", target.what, "slot", head.Slot)
		}
	}
}
