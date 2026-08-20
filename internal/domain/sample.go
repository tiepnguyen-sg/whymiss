package domain

import (
	"fmt"
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

// MetricSample is one numeric measurement at one instant.
//
// Samples are not restricted to the slot window. The timeline assembler may include
// a derived value a rule needs — a rolling p99 for the node's own Engine API
// latency, for example — attributed to [SourceDerived]. The engine cannot compute
// such a baseline itself, because it sees one slot at a time (ADR-0003).
type MetricSample struct {
	At        time.Time  `json:"at"`
	Component Component  `json:"component"`
	Name      MetricName `json:"name"`
	Value     float64    `json:"value"`
	Source    SourceID   `json:"source"`
}

// Validate reports why the sample is not well formed, or nil.
func (m MetricSample) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("invalid sample: %w", ErrInvalidKind)
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
	return nil
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
	if n.BlockArrivalP50 > n.BlockArrivalP90 {
		return fmt.Errorf("invalid baseline for slot %d: p50 %s exceeds p90 %s",
			n.Slot, n.BlockArrivalP50, n.BlockArrivalP90)
	}
	if !n.Source.Valid() {
		return fmt.Errorf("%w: baseline for slot %d from %q", ErrMissingSource, n.Slot, n.Source)
	}
	return nil
}
