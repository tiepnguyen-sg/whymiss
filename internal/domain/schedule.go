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

	// PayloadRevealDeadline is how far into the slot the builder must reveal the
	// execution payload, under a fork that separates consensus block from payload
	// (EIP-7732). Zero means the schedule is pre-ePBS: the payload arrives with
	// the block and there is no separate deadline to miss.
	//
	// Deliberately no default anywhere in this package. The spec value is not
	// final, and a plausible-looking constant compiled in would be indistinguishable
	// from a measured one at the point where it produced a wrong verdict (I-8).
	// An operator running an ePBS network sets it; everyone else leaves it zero.
	PayloadRevealDeadline time.Duration `json:"payload_reveal_deadline"`

	// PTCDeadline is how far into the slot the payload-timeliness committee votes
	// on whether the payload was revealed on time. Zero means pre-ePBS, and it is
	// meaningless without PayloadRevealDeadline — Validate rejects that pairing
	// rather than letting it read as "PTC at an unknown payload deadline".
	PTCDeadline time.Duration `json:"ptc_deadline"`
}

// IsPostEPBS reports whether this schedule describes a fork that separates the
// consensus block from the execution payload.
//
// It is the presence of a payload-reveal deadline that decides, not a version
// number or a fork name: whymiss never asks which fork is running, only what the
// timing model says, which is what makes a fork a configuration change.
func (s SlotSchedule) IsPostEPBS() bool { return s.PayloadRevealDeadline > 0 }

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

	// The ePBS pair. Both are zero on a pre-ePBS schedule and every case below
	// passes untouched; the checks exist so a half-configured ePBS schedule fails
	// at load rather than producing timings nobody meant.
	case s.PayloadRevealDeadline < 0:
		return fmt.Errorf("invalid schedule: payload_reveal_deadline is %s, must not be negative", s.PayloadRevealDeadline)
	case s.PTCDeadline < 0:
		return fmt.Errorf("invalid schedule: ptc_deadline is %s, must not be negative", s.PTCDeadline)
	case s.PTCDeadline > 0 && s.PayloadRevealDeadline == 0:
		return fmt.Errorf("invalid schedule: ptc_deadline %s is set without payload_reveal_deadline, "+
			"so there is no payload deadline for the committee to vote on", s.PTCDeadline)
	case s.PayloadRevealDeadline > 0 && s.PayloadRevealDeadline <= s.AttestationDeadline:
		return fmt.Errorf("invalid schedule: payload_reveal_deadline %s is at or before attestation_deadline %s",
			s.PayloadRevealDeadline, s.AttestationDeadline)
	case s.PayloadRevealDeadline > s.SecondsPerSlot:
		return fmt.Errorf("invalid schedule: payload_reveal_deadline %s exceeds seconds_per_slot %s",
			s.PayloadRevealDeadline, s.SecondsPerSlot)
	case s.PTCDeadline > 0 && s.PTCDeadline <= s.PayloadRevealDeadline:
		return fmt.Errorf("invalid schedule: ptc_deadline %s is at or before payload_reveal_deadline %s",
			s.PTCDeadline, s.PayloadRevealDeadline)
	case s.PTCDeadline > s.SecondsPerSlot:
		return fmt.Errorf("invalid schedule: ptc_deadline %s exceeds seconds_per_slot %s",
			s.PTCDeadline, s.SecondsPerSlot)
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

// PayloadRevealDeadlineAt returns the instant the payload must be revealed by,
// and false on a pre-ePBS schedule, where no such instant exists.
//
// The bool is the point. A caller that ignored it would get slotStart back and
// read it as "the deadline already passed", turning a fork that has no payload
// deadline into one that misses it on every slot.
func (s SlotSchedule) PayloadRevealDeadlineAt(slotStart time.Time) (time.Time, bool) {
	if !s.IsPostEPBS() {
		return time.Time{}, false
	}
	return slotStart.Add(s.PayloadRevealDeadline), true
}

// PTCDeadlineAt returns the instant the payload-timeliness committee's vote is
// due, and false when this schedule has no PTC deadline.
func (s SlotSchedule) PTCDeadlineAt(slotStart time.Time) (time.Time, bool) {
	if s.PTCDeadline <= 0 {
		return time.Time{}, false
	}
	return slotStart.Add(s.PTCDeadline), true
}
