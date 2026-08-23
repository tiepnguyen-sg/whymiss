package rules

import (
	"math"
	"strconv"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// postPropagationDominant reports whether validation dominates known stages.
func postPropagationDominant(tl domain.Timeline, cfg Config) bool {
	stages := ComputeStages(tl)
	dominant, ok := stages.Dominant(cfg)
	return ok && dominant == domain.StageValidation
}

// propagationOverspent reports that measured local block arrival consumed the
// entire attestation budget and, when another stage is known, propagation was
// also dominant. A lone late propagation stage still proves where the local
// budget was spent; a network baseline is required separately to say why.
func propagationOverspent(tl domain.Timeline, cfg Config) (Stages, bool) {
	stages := ComputeStages(tl)
	if !stages.HasPropagation || stages.Propagation <= tl.AttestationDeadline().Sub(tl.SlotStart) {
		return stages, false
	}
	if stages.HasValidation || stages.HasSigning {
		dominant, ok := stages.Dominant(cfg)
		return stages, ok && dominant == domain.StagePropagation
	}
	return stages, true
}

// timedBlockSeen returns a block-arrival timestamp measured by client metrics.
func timedBlockSeen(tl domain.Timeline) (domain.Observation, bool) {
	for _, obs := range tl.Observations {
		if obs.Kind == domain.ObsBlockSeen && obs.Source == domain.SourcePromScrape {
			return obs, true
		}
	}
	return domain.Observation{}, false
}

// engineCall is the parsed form of one engine_call observation.
type engineCall struct {
	at         time.Time
	method     string
	count      uint64
	durationMS float64
}

// engineCalls parses valid Engine call observations.
func engineCalls(tl domain.Timeline) []engineCall {
	var out []engineCall
	for _, obs := range tl.Observations {
		if obs.Kind != domain.ObsEngineCall || obs.Source != domain.SourcePromScrape {
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
		if err != nil || duration < 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
			continue
		}
		if method != domain.EngineMethodNewPayload && method != domain.EngineMethodForkchoiceUpdated {
			continue
		}
		countStr, ok := obs.Attr(domain.AttrSampleCount)
		if !ok {
			continue
		}
		count, err := strconv.ParseUint(countStr, 10, 64)
		if err != nil || count == 0 {
			continue
		}
		out = append(out, engineCall{at: obs.At, method: method, count: count, durationMS: duration})
	}
	return out
}

func completeEngineCalls(tl domain.Timeline) ([]engineCall, bool) {
	calls := engineCalls(tl)
	if len(calls) != 2 {
		return calls, false
	}
	var newPayload, forkchoice int
	for _, call := range calls {
		switch call.method {
		case domain.EngineMethodNewPayload:
			newPayload++
		case domain.EngineMethodForkchoiceUpdated:
			forkchoice++
		}
	}
	return calls, newPayload == 1 && forkchoice == 1
}

// dutyHasObservableLoss reports whether the closed timeline proves the duty
// lost reward. Host pressure alone is never a root cause for a healthy duty.
func dutyHasObservableLoss(tl domain.Timeline) bool {
	if tl.Duty == nil {
		return false
	}
	if tl.Duty.Kind == domain.DutyProposer {
		return !tl.Has(domain.ObsBlockProposed)
	}
	included, ok := tl.Last(domain.ObsAttestationIncluded)
	if !ok {
		return true
	}
	delayStr, ok := included.Attr(domain.AttrInclusionDelay)
	if !ok {
		return true
	}
	delay, err := strconv.ParseUint(delayStr, 10, 64)
	if err != nil || delay != 1 {
		return true
	}
	headCorrect, haveHead := included.Attr(domain.AttrHeadCorrect)
	targetCorrect, haveTarget := included.Attr(domain.AttrTargetCorrect)
	return !haveHead || !haveTarget || headCorrect != "true" || targetCorrect != "true"
}

func engineCallTotal(calls []engineCall) time.Duration {
	var totalMS float64
	for _, call := range calls {
		totalMS += call.durationMS
	}
	maxMilliseconds := float64(time.Duration(1<<63-1)) / float64(time.Millisecond)
	if math.IsInf(totalMS, 0) || totalMS >= maxMilliseconds {
		return time.Duration(1<<63 - 1)
	}
	return time.Duration(totalMS * float64(time.Millisecond))
}

// hostSampleFact reads the newest corpus observation or live metric sample
// without discarding the timestamp and source needed for auditable evidence.
func hostSampleFact(tl domain.Timeline, obsMetricName, sampleMetricName string) (value float64, at time.Time, source domain.SourceID, ok bool) {
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
		return value, obs.At, obs.Source, true
	}
	for i := len(tl.Samples) - 1; i >= 0; i-- {
		sample := tl.Samples[i]
		if sample.Component == domain.ComponentHost && sample.Name == domain.MetricName(sampleMetricName) {
			return sample.Value, sample.At, sample.Source, true
		}
	}
	return 0, time.Time{}, "", false
}

// peerCountFact reads the newest corpus observation or live metric sample.
func peerCountFact(tl domain.Timeline) (value float64, at time.Time, source domain.SourceID, ok bool) {
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
		return value, obs.At, obs.Source, true
	}
	for i := len(tl.Samples) - 1; i >= 0; i-- {
		sample := tl.Samples[i]
		if sample.Component == domain.ComponentCL && sample.Name == metricCLPeerCount {
			return sample.Value, sample.At, sample.Source, true
		}
	}
	return 0, time.Time{}, "", false
}
