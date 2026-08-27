package domain

import (
	"math"
	"time"
)

// TaxonomyVersion is the version of docs/causes.md this build encodes. Every
// Verdict records it so a report stays interpretable against the taxonomy it was
// written under (I-10, ADR-0005).
const TaxonomyVersion = "4.0.0"

// SlotsPerEpoch is the consensus-spec constant. It is here rather than in
// configuration because it is a property of the protocol, not of a deployment.
const SlotsPerEpoch = 32

// Slot is a consensus-layer slot number.
type Slot uint64

// Epoch returns the epoch containing the slot.
func (s Slot) Epoch() Epoch { return Epoch(s / SlotsPerEpoch) }

// LastAttestationInclusionSlot returns the final slot in which a Deneb-or-later
// block may include an attestation for s (EIP-7045): the last slot of the epoch
// following the attestation's target epoch.
func (s Slot) LastAttestationInclusionSlot() Slot {
	// The two-epoch look-ahead can overflow for malformed replay input near
	// MaxUint64. Saturating keeps the remaining window representable and lets
	// Timeline validation reject or reason about the input deterministically.
	maxEpoch := Epoch(math.MaxUint64 / SlotsPerEpoch)
	if s.Epoch() >= maxEpoch-1 {
		return Slot(math.MaxUint64)
	}
	return (s.Epoch() + 2).FirstSlot() - 1
}

// CollectionWindowEnd returns the earliest instant by which every block
// through s's final valid attestation-inclusion slot is guaranteed to have
// fully elapsed. This is the wall-clock floor Timeline.Validate enforces on
// ObsCollectionCompleted (ADR-0016/0018): a collector must not claim the
// inclusion window closed before this instant, however early a positive or
// negative inclusion result was itself observed.
func (s Slot) CollectionWindowEnd(slotStart time.Time, secondsPerSlot time.Duration) time.Time {
	windowSlots := uint64(s.LastAttestationInclusionSlot() - s)
	if windowSlots > SlotsPerEpoch*2-1 || secondsPerSlot <= 0 {
		return slotStart.Add(time.Duration(math.MaxInt64))
	}
	return slotStart.Add(time.Duration(windowSlots+1) * secondsPerSlot) //nolint:gosec // windowSlots bounded above
}

// Epoch is a consensus-layer epoch number.
type Epoch uint64

// FirstSlot returns the first slot of the epoch.
func (e Epoch) FirstSlot() Slot { return Slot(e) * SlotsPerEpoch }

// ValidatorIndex identifies a validator in the beacon state.
type ValidatorIndex uint64

// SourceID identifies the adapter that produced a value. Evidence is attributed
// with it, so an operator can tell a scraped metric from a chain observation.
type SourceID string

// The adapters that exist, one per inbound source. A new adapter adds a constant
// here and nothing else outside internal/source (I-11).
const (
	// SourceBeaconAPI is the beacon node's HTTP API and SSE event stream.
	SourceBeaconAPI SourceID = "beaconapi"

	// SourcePromScrape is a Prometheus endpoint exposed by the EL, CL, or VC.
	SourcePromScrape SourceID = "promscrape"

	// SourceHostMetrics is the host itself: PSI I/O, CPU steal, memory pressure.
	SourceHostMetrics SourceID = "hostmetrics"

	// SourceClock is the NTP offset measurement (I-9).
	SourceClock SourceID = "clock"

	// SourceXatu is the public network-baseline dataset. Opt-in (Phase 5).
	SourceXatu SourceID = "xatu"

	// SourceDerived marks a value whymiss computed rather than observed, such as
	// the wall-clock start of a slot.
	SourceDerived SourceID = "derived"
)

// Valid reports whether the source is one this build knows how to produce.
func (s SourceID) Valid() bool {
	switch s {
	case SourceBeaconAPI, SourcePromScrape, SourceHostMetrics,
		SourceClock, SourceXatu, SourceDerived:
		return true
	default:
		return false
	}
}

// Stage is a segment of the latency budget for a duty. Attribution names the stage
// that overspent, which is why the stages are a closed set (docs/causes.md §3.2).
type Stage string

const (
	// StagePropagation covers block arrival: slot start to block first seen.
	StagePropagation Stage = "propagation"

	// StageValidation covers CL and EL processing: block seen to head updated.
	StageValidation Stage = "validation"

	// StageSigning covers the validator client: head updated to attestation
	// published.
	StageSigning Stage = "signing"
)

// Stages returns the stages in the order they occur within a slot. The order is
// part of the timing model, so callers must not sort the result.
func Stages() []Stage {
	return []Stage{StagePropagation, StageValidation, StageSigning}
}

// Valid reports whether the stage is one of the three in the timing model.
func (s Stage) Valid() bool {
	switch s {
	case StagePropagation, StageValidation, StageSigning:
		return true
	default:
		return false
	}
}
