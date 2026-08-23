package domain

import (
	"fmt"
	"math"
	"strconv"
	"time"
)

// Component is the part of the stack a measurement came from. Rules attribute to a
// layer, so samples are labelled by layer rather than by client — a client name here
// would leak adapter knowledge into the engine (I-11).
type Component string

const (
	// ComponentEL is the execution client.
	ComponentEL Component = "el"

	// ComponentCL is the consensus client, also called the beacon node.
	ComponentCL Component = "cl"

	// ComponentVC is the validator client.
	ComponentVC Component = "vc"

	// ComponentHost is the machine everything runs on.
	ComponentHost Component = "host"
)

// Valid reports whether the component is one of the four layers.
func (c Component) Valid() bool {
	switch c {
	case ComponentEL, ComponentCL, ComponentVC, ComponentHost:
		return true
	default:
		return false
	}
}

// MetricName is a normalised metric name. Adapters translate client-specific names
// into these before the sample reaches the timeline, so the engine never sees a
// upstream client's metric name (I-11). The normalised vocabulary is documented in
// docs/configuration.md and fixed by the promscrape adapters in Phase 2.
type MetricName string

const maxMetricNameBytes = 128

// MetricSample is one numeric measurement at one instant.
//
// Samples are not restricted to the slot window. The timeline assembler may include
// a derived value a rule needs — a rolling p99 for the node's own Engine API
// latency, for example — attributed to [SourceDerived]. The engine cannot compute
// such a baseline itself, because it sees one slot at a time (ADR-0003).
type MetricSample struct {
	At            time.Time     `json:"at"`
	Component     Component     `json:"component"`
	Name          MetricName    `json:"name"`
	Value         float64       `json:"value"`
	ClockOffset   time.Duration `json:"clock_offset"`
	ClockMeasured bool          `json:"clock_measured,omitempty"`
	ClockSampleAt time.Time     `json:"clock_sample_at,omitempty"`
	Source        SourceID      `json:"source"`
}

// Validate reports why the sample is not well formed, or nil.
func (m MetricSample) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("invalid sample: %w", ErrInvalidKind)
	}
	if len(m.Name) > maxMetricNameBytes {
		return fmt.Errorf("invalid sample: metric name is %d bytes, limit is %d", len(m.Name), maxMetricNameBytes)
	}
	if !m.Component.Valid() {
		return fmt.Errorf("invalid sample %q: unknown component %q", m.Name, m.Component)
	}
	if m.At.IsZero() {
		return fmt.Errorf("%w: sample %q", ErrMissingTimestamp, m.Name)
	}
	if m.At.Location() != time.UTC {
		return fmt.Errorf("%w: sample %q at %s", ErrNotUTC, m.Name, m.At.Location())
	}
	if !m.Source.Valid() {
		return fmt.Errorf("%w: sample %q from %q", ErrMissingSource, m.Name, m.Source)
	}
	if !sampleSourcePermittedFor(m.Source, m.Component) {
		return fmt.Errorf("%w: source %q on %s sample %q", ErrInvalidSource, m.Source, m.Component, m.Name)
	}
	if math.IsNaN(m.Value) || math.IsInf(m.Value, 0) {
		return fmt.Errorf("invalid sample %q: value must be finite", m.Name)
	}
	if m.ClockMeasured {
		if m.ClockSampleAt.IsZero() {
			return fmt.Errorf("%w: measured sample %q has no clock_sample_at", ErrInvalidClock, m.Name)
		}
		if m.ClockSampleAt.Location() != time.UTC {
			return fmt.Errorf("%w: clock_sample_at for sample %q is at %s", ErrInvalidClock, m.Name, m.ClockSampleAt.Location())
		}
	} else if m.ClockOffset != 0 || !m.ClockSampleAt.IsZero() {
		return fmt.Errorf("%w: unmeasured sample %q carries clock data", ErrInvalidClock, m.Name)
	}
	return nil
}

func sampleSourcePermittedFor(source SourceID, component Component) bool {
	switch source {
	case SourcePromScrape:
		return component == ComponentEL || component == ComponentCL || component == ComponentVC
	case SourceHostMetrics:
		return component == ComponentHost
	case SourceDerived:
		return true
	default:
		return false
	}
}

// NetworkBaseline is what the network as a whole did with this slot.
//
// It is the input that separates "the block was late everywhere" from "the block was
// late here", which is the product's central question. It is optional: the baseline
// is opt-in and defaults to off (I-4), and a nil baseline must degrade the verdict
// rather than break the analysis.
type NetworkBaseline struct {
	Slot Slot `json:"slot"`

	// BlockArrivalP50 and BlockArrivalP90 are network-wide block-arrival offsets
	// measured from the slot start.
	BlockArrivalP50 time.Duration `json:"block_arrival_p50"`
	BlockArrivalP90 time.Duration `json:"block_arrival_p90"`

	// SampleCount is how many observations the percentiles were computed from. A
	// thin sample caps confidence at medium rather than invalidating the baseline
	// (docs/causes.md §7, network.late_block).
	SampleCount int `json:"sample_count"`

	Source SourceID `json:"source"`
}

// Validate reports why the baseline is not usable, or nil.
func (n NetworkBaseline) Validate() error {
	if n.SampleCount <= 0 {
		return fmt.Errorf("invalid baseline for slot %d: sample_count is %d", n.Slot, n.SampleCount)
	}
	if n.BlockArrivalP50 < 0 || n.BlockArrivalP90 < 0 {
		return fmt.Errorf("invalid baseline for slot %d: arrival percentiles must be non-negative", n.Slot)
	}
	if n.BlockArrivalP50 > n.BlockArrivalP90 {
		return fmt.Errorf("invalid baseline for slot %d: p50 %s exceeds p90 %s",
			n.Slot, n.BlockArrivalP50, n.BlockArrivalP90)
	}
	if !n.Source.Valid() {
		return fmt.Errorf("%w: baseline for slot %d from %q", ErrMissingSource, n.Slot, n.Source)
	}
	if n.Source != SourceXatu && n.Source != SourcePromScrape {
		return fmt.Errorf("%w: source %q on network baseline for slot %d", ErrInvalidSource, n.Source, n.Slot)
	}
	return nil
}

// NetworkBaselineFromObservation decodes the bounded wire form used by live
// storage and corpus replay.
func NetworkBaselineFromObservation(obs Observation) (NetworkBaseline, error) {
	if obs.Kind != ObsNetworkBaselineSampled {
		return NetworkBaseline{}, fmt.Errorf("observation kind %q is not a network baseline", obs.Kind)
	}
	p50, err := baselineDurationAttr(obs, AttrBlockArrivalP50MS)
	if err != nil {
		return NetworkBaseline{}, err
	}
	p90, err := baselineDurationAttr(obs, AttrBlockArrivalP90MS)
	if err != nil {
		return NetworkBaseline{}, err
	}
	countText, ok := obs.Attr(AttrSampleCount)
	if !ok {
		return NetworkBaseline{}, fmt.Errorf("network baseline is missing %q", AttrSampleCount)
	}
	count, err := strconv.ParseInt(countText, 10, 32)
	if err != nil || count <= 0 {
		return NetworkBaseline{}, fmt.Errorf("network baseline %q is invalid: %q", AttrSampleCount, countText)
	}
	baseline := NetworkBaseline{
		Slot: obs.Slot, BlockArrivalP50: p50, BlockArrivalP90: p90,
		SampleCount: int(count), Source: obs.Source,
	}
	if err := baseline.Validate(); err != nil {
		return NetworkBaseline{}, err
	}
	return baseline, nil
}

func baselineDurationAttr(obs Observation, key AttrKey) (time.Duration, error) {
	text, ok := obs.Attr(key)
	if !ok {
		return 0, fmt.Errorf("network baseline is missing %q", key)
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("network baseline %q is invalid: %q", key, text)
	}
	if value > float64(math.MaxInt64)/float64(time.Millisecond) {
		return 0, fmt.Errorf("network baseline %q overflows time.Duration: %q", key, text)
	}
	return time.Duration(value * float64(time.Millisecond)), nil
}
