package rules

import (
	"strconv"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/rca"
)

// postPropagationDominant reports whether the "post-propagation" span
// (validation, possibly combined with signing — see
// rca.ComputeStages's doc comment on the head_updated data gap) is the
// dominant stage. R-300 and R-310 both gate on this: EL/CL slowness is a
// validation-stage phenomenon, and with head_updated never populated by
// this codebase's collectors today, Validation is the only field carrying
// that span.
func postPropagationDominant(tl domain.Timeline, cfg rca.Config) bool {
	stages := rca.ComputeStages(tl)
	dominant, ok := stages.Dominant(cfg)
	return ok && dominant == domain.StageValidation
}

// engineCall is the parsed form of one engine_call observation.
type engineCall struct {
	at         time.Time
	method     string
	durationMS float64
}

// engineCalls returns every engine_call observation on tl, parsed.
// Unparseable entries are skipped rather than erroring — a rule reading
// this is already past the point where a malformed attribute should abort
// attribution (I-8: degrade, don't refuse).
func engineCalls(tl domain.Timeline) []engineCall {
	var out []engineCall
	for _, obs := range tl.Observations {
		if obs.Kind != domain.ObsEngineCall {
			continue
		}
		method, ok := obs.Attr(domain.AttrEngineMethod)
		if !ok {
			continue
		}
		durationStr, ok := obs.Attr(domain.AttrDurationMS)
		if !ok {
			continue
		}
		duration, err := strconv.ParseFloat(durationStr, 64)
		if err != nil {
			continue
		}
		out = append(out, engineCall{at: obs.At, method: method, durationMS: duration})
	}
	return out
}

// hostSampledValue reads a host_sampled observation's value for the named
// metric — the format tools/faultinjector's corpus scenarios actually
// produce (ObsHostSampled with AttrMetric/AttrValue), distinct from
// internal/source/hostmetrics's MetricSample form that whymiss watch
// produces for live collection. Checks both: Observations first (what the
// corpus has), Samples second (what a live Timeline would have) — see
// hostSampleNames below for the name mapping between the two forms.
func hostSampledValue(tl domain.Timeline, obsMetricName, sampleMetricName string) (float64, bool) {
	for i := len(tl.Observations) - 1; i >= 0; i-- {
		obs := tl.Observations[i]
		if obs.Kind != domain.ObsHostSampled {
			continue
		}
		metric, ok := obs.Attr(domain.AttrMetric)
		if !ok || metric != obsMetricName {
			continue
		}
		valueStr, ok := obs.Attr(domain.AttrValue)
		if !ok {
			continue
		}
		value, err := strconv.ParseFloat(valueStr, 64)
		if err != nil {
			continue
		}
		return value, true
	}
	return tl.SampleValue(domain.ComponentHost, domain.MetricName(sampleMetricName))
}

// peerCountValue reads a peer-count sample, checking both forms the same
// way hostSampledValue does: Observations first (domain.ObsPeerCountSampled
// with AttrPeerCount — the format tools/faultinjector's corpus scenarios
// produce), falling back to Samples (metricCLPeerCount — the
// live-collection form internal/source/promscrape produces for whymiss
// watch). Without this dual check, R-200's peer-count corroboration
// branch was unreachable by any real corpus scenario: faultinjector never
// wrote a MetricSample, only Observations, and Timeline.SampleValue only
// ever reads Samples.
func peerCountValue(tl domain.Timeline) (float64, bool) {
	for i := len(tl.Observations) - 1; i >= 0; i-- {
		obs := tl.Observations[i]
		if obs.Kind != domain.ObsPeerCountSampled {
			continue
		}
		valueStr, ok := obs.Attr(domain.AttrPeerCount)
		if !ok {
			continue
		}
		value, err := strconv.ParseFloat(valueStr, 64)
		if err != nil {
			continue
		}
		return value, true
	}
	return tl.SampleValue(domain.ComponentCL, metricCLPeerCount)
}

// elSubCause selects local.el_slow's sub-cause per docs/causes.md §7's
// first-match-wins table. syncing/snapshot/pruning need signals this
// codebase's collectors don't produce yet (no observation or metric kind
// carries EL sync status or snapshot/pruning activity) — only
// disk_saturation is checkable today, via host_sampled's iowait_pct.
func elSubCause(tl domain.Timeline) (domain.CauseID, string) {
	iowait, ok := hostSampledValue(tl, "iowait_pct", "host_iowait_pct")
	if !ok || iowait <= 0 {
		return "", ""
	}
	return domain.CauseELSlowDiskSaturation, ""
}

// vcTimingShapeMatches reports whether tl carries R-410's exact evidence
// shape (block available before the attestation deadline, attestation not
// published until after it) — the direct signal VCSlow.Evaluate itself
// matches on. R-310 checks this to defer to R-410 rather than claiming
// local.cl_slow: "validation-plus-signing span dominant with no engine_call
// evidence" is a coarse elimination fallback, and shouldn't outrank direct
// timing evidence that the delay specifically happened in the signing step
// (a real corpus scenario, vc-slow-cpu, has exactly this shape — R-310's
// fallback branch matched it first purely because it's ordered first,
// before R-410 ever got a chance, even though R-410's evidence is strictly
// more specific).
func vcTimingShapeMatches(tl domain.Timeline) bool {
	blockSeen, hasBlockSeen := tl.First(domain.ObsBlockSeen)
	published, hasPublished := tl.First(domain.ObsAttestationPublished)
	if !hasBlockSeen || !hasPublished {
		return false
	}
	deadline := tl.AttestationDeadline()
	return blockSeen.At.Before(deadline) && published.At.After(deadline)
}

// inclusionDelay returns the parsed inclusion_delay attribute of tl's
// attestation_included observation, if any.
func inclusionDelay(tl domain.Timeline) (uint64, bool) {
	included, ok := tl.Last(domain.ObsAttestationIncluded)
	if !ok {
		return 0, false
	}
	delayStr, ok := included.Attr(domain.AttrInclusionDelay)
	if !ok {
		return 0, false
	}
	delay, err := strconv.ParseUint(delayStr, 10, 64)
	if err != nil {
		return 0, false
	}
	return delay, true
}
