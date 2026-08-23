package domain

import (
	"fmt"
	"time"
)

const maxSupportedSlotDuration = time.Minute

// SlotSchedule is the timing model of a slot, expressed as data.
//
// Every deadline whymiss reasons about comes from here. Hard-coding a slot duration
// or an attestation deadline anywhere else is a bug: the constants change at
// Glamsterdam, and the design intent is that a fork becomes a configuration change
// rather than a rewrite (docs/causes.md §3.1, BUILD_PROMPT task 5.4).
//
// Phase 5 adds the post-ePBS payload-reveal and PTC deadlines as further fields.
// That is additive by design; existing fields keep their meaning.
type SlotSchedule struct {
	// SecondsPerSlot is the slot duration. Named for the consensus-spec constant it
	// mirrors, though it is a duration rather than a count.
	SecondsPerSlot time.Duration `json:"seconds_per_slot"`

	// AttestationDeadline is how far into the slot an attestation must be published
	// to earn the timely-source flag. Spec value is
	// SECONDS_PER_SLOT / INTERVALS_PER_SLOT.
	AttestationDeadline time.Duration `json:"attestation_deadline"`

	// AggregationDeadline is how far into the slot aggregation completes. whymiss
	// does not attribute aggregator duties in v1 but needs the boundary to bound
	// the inclusion window.
	AggregationDeadline time.Duration `json:"aggregation_deadline"`
}

// MainnetPreEPBS is the pre-ePBS mainnet schedule (docs/causes.md §3.1).
//
// It is a default for configuration to start from, not a constant for rules to
// reach for. A rule reads the schedule off the timeline it was given.
func MainnetPreEPBS() SlotSchedule {
	return SlotSchedule{
		SecondsPerSlot:      12 * time.Second,
		AttestationDeadline: 4 * time.Second,
		AggregationDeadline: 8 * time.Second,
	}
}

// Validate reports why the schedule is not usable, or nil.
//
// The ordering constraints matter: a schedule whose deadlines fall outside the slot,
// or in the wrong order, would silently produce nonsense attributions rather than
// failing.
func (s SlotSchedule) Validate() error {
	switch {
	case s.SecondsPerSlot <= 0:
		return fmt.Errorf("invalid schedule: seconds_per_slot is %s, must be positive", s.SecondsPerSlot)
	case s.SecondsPerSlot > maxSupportedSlotDuration:
		return fmt.Errorf("invalid schedule: seconds_per_slot is %s, maximum supported is %s", s.SecondsPerSlot, maxSupportedSlotDuration)
	case s.AttestationDeadline <= 0:
		return fmt.Errorf("invalid schedule: attestation_deadline is %s, must be positive", s.AttestationDeadline)
	case s.AggregationDeadline <= 0:
		return fmt.Errorf("invalid schedule: aggregation_deadline is %s, must be positive", s.AggregationDeadline)
	case s.AttestationDeadline > s.AggregationDeadline:
		return fmt.Errorf("invalid schedule: attestation_deadline %s is after aggregation_deadline %s",
			s.AttestationDeadline, s.AggregationDeadline)
	case s.AggregationDeadline > s.SecondsPerSlot:
		return fmt.Errorf("invalid schedule: aggregation_deadline %s exceeds seconds_per_slot %s",
			s.AggregationDeadline, s.SecondsPerSlot)
	default:
		return nil
	}
}

// AttestationDeadlineAt returns the wall-clock instant the attestation deadline
// falls at for a slot beginning at slotStart.
func (s SlotSchedule) AttestationDeadlineAt(slotStart time.Time) time.Time {
	return slotStart.Add(s.AttestationDeadline)
}

// SlotEndAt returns the wall-clock instant the slot ends.
func (s SlotSchedule) SlotEndAt(slotStart time.Time) time.Time {
	return slotStart.Add(s.SecondsPerSlot)
}
