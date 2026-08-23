package promscrape

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

const (
	// EngineMethodNewPayload is the client-normalized newPayload method name.
	EngineMethodNewPayload = domain.EngineMethodNewPayload
	// EngineMethodForkchoiceUpdated is the client-normalized forkchoiceUpdated method name.
	EngineMethodForkchoiceUpdated = domain.EngineMethodForkchoiceUpdated
	// MetricELEngineCallsP99MS is the rolling per-slot Engine duration baseline.
	MetricELEngineCallsP99MS = domain.MetricName("el_engine_calls_p99_ms")
)

// EngineCounter is one cumulative Engine API latency counter.
type EngineCounter struct {
	Count uint64
	SumMS float64
}

// EngineCounters is a client-normalized, point-in-time counter snapshot.
type EngineCounters struct {
	SampledAt         time.Time
	NewPayload        EngineCounter
	ForkchoiceUpdated EngineCounter
}

// EngineCallWindow is one method's exact count and total duration between two
// consecutive canonical-head counter snapshots.
type EngineCallWindow struct {
	At         time.Time
	Method     string
	Count      uint64
	DurationMS float64
}

// SampleLighthouseEngineCounters reads Lighthouse's cumulative Engine metrics.
func (s *Scraper) SampleLighthouseEngineCounters(ctx context.Context, metricsURL string) (EngineCounters, error) {
	return s.sampleEngineCounters(ctx, metricsURL, engineMetricSet{
		newPayloadSum:   `execution_layer_request_times_sum{method="new_payload"}`,
		newPayloadCount: `execution_layer_request_times_count{method="new_payload"}`,
		forkchoiceSum:   `execution_layer_request_times_sum{method="forkchoice_updated"}`,
		forkchoiceCount: `execution_layer_request_times_count{method="forkchoice_updated"}`,
		sumScaleToMS:    1000,
	})
}

// SamplePrysmEngineCounters reads Prysm's cumulative Engine metrics.
func (s *Scraper) SamplePrysmEngineCounters(ctx context.Context, metricsURL string) (EngineCounters, error) {
	return s.sampleEngineCounters(ctx, metricsURL, engineMetricSet{
		newPayloadSum:   "new_payload_v1_latency_milliseconds_sum",
		newPayloadCount: "new_payload_v1_latency_milliseconds_count",
		forkchoiceSum:   "forkchoice_updated_v1_latency_milliseconds_sum",
		forkchoiceCount: "forkchoice_updated_v1_latency_milliseconds_count",
		sumScaleToMS:    1,
	})
}

// EngineCallsBetween returns one exact aggregate for each required method when
// both counters advanced at least once in the bounded head-to-head window.
func EngineCallsBetween(before, after EngineCounters) ([]EngineCallWindow, error) {
	if before.SampledAt.IsZero() || after.SampledAt.IsZero() || after.SampledAt.Before(before.SampledAt) {
		return nil, fmt.Errorf("invalid Engine counter sample order")
	}
	calls := make([]EngineCallWindow, 0, 2)
	for _, pair := range []struct {
		method        string
		before, after EngineCounter
	}{
		{EngineMethodNewPayload, before.NewPayload, after.NewPayload},
		{EngineMethodForkchoiceUpdated, before.ForkchoiceUpdated, after.ForkchoiceUpdated},
	} {
		if pair.after.Count <= pair.before.Count {
			return nil, fmt.Errorf("%s count did not advance from %d to %d", pair.method, pair.before.Count, pair.after.Count)
		}
		durationMS := pair.after.SumMS - pair.before.SumMS
		if durationMS < 0 || math.IsNaN(durationMS) || math.IsInf(durationMS, 0) {
			return nil, fmt.Errorf("%s cumulative duration moved from %.3fms to %.3fms", pair.method, pair.before.SumMS, pair.after.SumMS)
		}
		calls = append(calls, EngineCallWindow{
			At: after.SampledAt, Method: pair.method,
			Count: pair.after.Count - pair.before.Count, DurationMS: durationMS,
		})
	}
	return calls, nil
}

type engineMetricSet struct {
	newPayloadSum   string
	newPayloadCount string
	forkchoiceSum   string
	forkchoiceCount string
	sumScaleToMS    float64
}

func (s *Scraper) sampleEngineCounters(ctx context.Context, metricsURL string, names engineMetricSet) (EngineCounters, error) {
	lines, err := s.fetchMetricsLines(ctx, metricsURL)
	if err != nil {
		return EngineCounters{}, err
	}

	wanted := []string{names.newPayloadSum, names.newPayloadCount, names.forkchoiceSum, names.forkchoiceCount}
	values := make(map[string]float64, len(wanted))
	for _, line := range lines {
		for _, name := range wanted {
			value, ok, err := exactMetricValue(line, name)
			if err != nil {
				return EngineCounters{}, err
			}
			if ok {
				values[name] = value
			}
		}
	}
	for _, name := range wanted {
		if _, ok := values[name]; !ok {
			return EngineCounters{}, fmt.Errorf("engine metric %q not found in metrics from %s", name, metricsURL)
		}
	}

	newPayloadCount, err := counterValue(values[names.newPayloadCount], names.newPayloadCount)
	if err != nil {
		return EngineCounters{}, err
	}
	forkchoiceCount, err := counterValue(values[names.forkchoiceCount], names.forkchoiceCount)
	if err != nil {
		return EngineCounters{}, err
	}
	return EngineCounters{
		SampledAt: time.Now().UTC(),
		NewPayload: EngineCounter{
			Count: newPayloadCount,
			SumMS: values[names.newPayloadSum] * names.sumScaleToMS,
		},
		ForkchoiceUpdated: EngineCounter{
			Count: forkchoiceCount,
			SumMS: values[names.forkchoiceSum] * names.sumScaleToMS,
		},
	}, nil
}

func exactMetricValue(line, name string) (float64, bool, error) {
	rest, ok := strings.CutPrefix(line, name+" ")
	if !ok {
		return 0, false, nil
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
	if err != nil || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false, fmt.Errorf("parse non-negative value for %s from line %q", name, line)
	}
	return value, true, nil
}

func counterValue(value float64, name string) (uint64, error) {
	if value != math.Trunc(value) || value > math.MaxUint64 {
		return 0, fmt.Errorf("counter %s has non-integer value %g", name, value)
	}
	return uint64(value), nil
}
