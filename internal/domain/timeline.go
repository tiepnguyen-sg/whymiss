package domain

import (
	"fmt"
	"slices"
	"time"
)

// Timeline is the complete input to the RCA engine for one slot.
//
// It is self-contained on purpose. The engine performs no I/O, so anything a rule
// could need has to be materialised here first: the duty, the observations, the
// samples, the timing model, and the optional network baseline. If a rule wants a
// fact the timeline lacks, the fix belongs in internal/timeline, never in a lookup
// from inside the engine (I-6, ADR-0003).
type Timeline struct {
	Slot Slot `json:"slot"`

	// SlotStart is the wall-clock start of the slot, in UTC. Every offset a human
	// reads in a report is relative to this instant.
	SlotStart time.Time `json:"slot_start"`

	// Schedule is the timing model in force for this slot. It travels with the
	// timeline so a replayed corpus scenario is analysed against the schedule it was
	// recorded under, not whatever the current configuration says.
	Schedule SlotSchedule `json:"schedule"`

	// Duty is what the validator owed. Nil means nothing was owed this slot, which
	// is not a failure and must not be reported as one.
	Duty *Duty `json:"duty,omitempty"`

	// Observations are the facts recorded about this slot, sorted by At ascending.
	// Every observation carries this timeline's slot: an observation is filed under
	// the slot it is *about*, so an inclusion seen in slot N+1 belongs to the
	// timeline of the duty slot N and records the distance in inclusion_delay.
	Observations []Observation `json:"observations"`

	// Samples are the EL, CL, VC, and host measurements a rule may compare against,
	// including derived baselines.
	Samples []MetricSample `json:"samples,omitempty"`

	// Network is the network-wide baseline for this slot, or nil when the baseline
	// is disabled or unavailable (I-4).
	Network *NetworkBaseline `json:"network,omitempty"`
}

// NewTimeline validates a draft timeline and returns a canonical copy.
//
// The copy owns its slices, so the assembler that built the draft cannot mutate a
// timeline the engine is already reasoning about. Observations must already be sorted
// by timestamp: sorting here would hide an assembler bug that produces a
// non-deterministic order, and determinism is the guarantee the engine rests on.
func NewTimeline(draft Timeline) (Timeline, error) {
	if err := draft.Validate(); err != nil {
		return Timeline{}, err
	}
	out := draft
	out.Observations = slices.Clone(draft.Observations)
	out.Samples = slices.Clone(draft.Samples)
	if draft.Duty != nil {
		duty := *draft.Duty
		out.Duty = &duty
	}
	if draft.Network != nil {
		baseline := *draft.Network
		out.Network = &baseline
	}
	return out, nil
}

// Validate reports why the timeline is not well formed, or nil.
func (t Timeline) Validate() error {
	if t.SlotStart.IsZero() {
		return fmt.Errorf("%w: timeline for slot %d", ErrMissingTimestamp, t.Slot)
	}
	if t.SlotStart.Location() != time.UTC {
		return fmt.Errorf("%w: slot_start at %s", ErrNotUTC, t.SlotStart.Location())
	}
	if err := t.Schedule.Validate(); err != nil {
		return fmt.Errorf("timeline for slot %d: %w", t.Slot, err)
	}
	if t.Duty != nil {
		if err := t.Duty.Validate(); err != nil {
			return fmt.Errorf("timeline for slot %d: %w", t.Slot, err)
		}
		if t.Duty.Slot != t.Slot {
			return fmt.Errorf("%w: duty slot %d on timeline for slot %d",
				ErrSlotMismatch, t.Duty.Slot, t.Slot)
		}
	}
	for i, obs := range t.Observations {
		if err := obs.Validate(); err != nil {
			return fmt.Errorf("observation %d: %w", i, err)
		}
		if obs.Slot != t.Slot {
			return fmt.Errorf("%w: observation %d is for slot %d, timeline is slot %d",
				ErrSlotMismatch, i, obs.Slot, t.Slot)
		}
		if i > 0 && obs.At.Before(t.Observations[i-1].At) {
			return fmt.Errorf("%w: observation %d (%s) precedes observation %d (%s)",
				ErrUnsorted, i, obs.At.Format(time.RFC3339Nano),
				i-1, t.Observations[i-1].At.Format(time.RFC3339Nano))
		}
	}
	for i, sample := range t.Samples {
		if err := sample.Validate(); err != nil {
			return fmt.Errorf("sample %d: %w", i, err)
		}
	}
	if t.Network != nil {
		if err := t.Network.Validate(); err != nil {
			return fmt.Errorf("timeline for slot %d: %w", t.Slot, err)
		}
		if t.Network.Slot != t.Slot {
			return fmt.Errorf("%w: baseline for slot %d on timeline for slot %d",
				ErrSlotMismatch, t.Network.Slot, t.Slot)
		}
	}
	return nil
}

// First returns the earliest observation of a kind. Observations are sorted, so
// "first" is well defined and the second return value distinguishes absence from a
// zero value — absence is itself evidence in this domain (docs/causes.md §7,
// network.proposer_missed).
func (t Timeline) First(kind ObservationKind) (Observation, bool) {
	for _, obs := range t.Observations {
		if obs.Kind == kind {
			return obs, true
		}
	}
	return Observation{}, false
}

// Last returns the latest observation of a kind.
func (t Timeline) Last(kind ObservationKind) (Observation, bool) {
	for i := len(t.Observations) - 1; i >= 0; i-- {
		if t.Observations[i].Kind == kind {
			return t.Observations[i], true
		}
	}
	return Observation{}, false
}

// Has reports whether any observation of a kind was recorded.
func (t Timeline) Has(kind ObservationKind) bool {
	_, ok := t.First(kind)
	return ok
}

// Count returns how many observations of a kind were recorded.
func (t Timeline) Count(kind ObservationKind) int {
	n := 0
	for _, obs := range t.Observations {
		if obs.Kind == kind {
			n++
		}
	}
	return n
}

// MaxAbsClockOffset returns the largest absolute clock offset recorded on any
// observation in the timeline, and whether any observation carried one.
//
// It reports the measurement without judging it: whether that offset is tolerable is
// a configured threshold, and the comparison belongs to the rule that suppresses
// timing attribution (I-9, rule R-011).
func (t Timeline) MaxAbsClockOffset() (time.Duration, bool) {
	var worst time.Duration
	found := false
	for _, obs := range t.Observations {
		offset := obs.ClockOffset
		if offset < 0 {
			offset = -offset
		}
		if !found || offset > worst {
			worst, found = offset, true
		}
	}
	return worst, found
}

// SampleValue returns the value of the most recent sample of a metric on a
// component, and whether one was present. Most recent rather than first because a
// rule comparing against a rolling baseline wants the freshest value the assembler
// had.
func (t Timeline) SampleValue(component Component, name MetricName) (float64, bool) {
	var (
		value float64
		at    time.Time
		found bool
	)
	for _, sample := range t.Samples {
		if sample.Component != component || sample.Name != name {
			continue
		}
		if !found || sample.At.After(at) {
			value, at, found = sample.Value, sample.At, true
		}
	}
	return value, found
}

// AttestationDeadline returns the instant by which the attestation had to be
// published for this slot.
func (t Timeline) AttestationDeadline() time.Time {
	return t.Schedule.AttestationDeadlineAt(t.SlotStart)
}
