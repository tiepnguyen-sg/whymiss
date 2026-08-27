package domain

import (
	"encoding/hex"
	"fmt"
	"maps"
	"math"
	"strconv"
	"strings"
	"time"
)

// ObservationKind is a closed vocabulary: the kinds of fact whymiss is able to
// record. Adding one is a taxonomy change — a minor version bump and an ADR
// (docs/causes.md §8, ADR-0005).
type ObservationKind string

const maxObservationAttrValueBytes = 256

const (
	// ObsSlotStart is the wall-clock start of the slot. Derived, not observed: it
	// anchors every offset in the report.
	ObsSlotStart ObservationKind = "slot_start"

	// ObsDutyAssigned records that an attester or proposer duty is known for this
	// slot.
	ObsDutyAssigned ObservationKind = "duty_assigned"

	// ObsBlockSeen records the beacon block first arriving locally. Ends the
	// propagation stage.
	ObsBlockSeen ObservationKind = "block_seen"

	// ObsBlockSkipped records that a fully synced beacon node confirmed the
	// canonical chain has no block at this slot.
	ObsBlockSkipped ObservationKind = "block_skipped"

	// ObsHeadUpdated records the head advancing to the block after validation.
	// Ends the validation stage.
	ObsHeadUpdated ObservationKind = "head_updated"

	// ObsAttestationPublished records the attestation being broadcast. Ends the
	// signing stage.
	ObsAttestationPublished ObservationKind = "attestation_published"

	// ObsAttestationIncluded records the attestation observed on chain.
	ObsAttestationIncluded ObservationKind = "attestation_included"

	// ObsBlockProposed records this node's own proposal being broadcast.
	ObsBlockProposed ObservationKind = "block_proposed"

	// ObsReorg records a chain reorganisation. Reorgs invalidate inclusion
	// reasoning, so they are recorded even when they look irrelevant.
	ObsReorg ObservationKind = "reorg"

	// ObsPeerCountSampled records a peer-count sample.
	ObsPeerCountSampled ObservationKind = "peer_count_sampled"

	// ObsEngineCall records the total duration and call count for one Engine API
	// method in an exact canonical-head counter window.
	ObsEngineCall ObservationKind = "engine_call"

	// ObsHostSampled records one host resource measurement.
	ObsHostSampled ObservationKind = "host_sampled"

	// ObsClockSampled records one NTP offset measurement (I-9).
	ObsClockSampled ObservationKind = "clock_sampled"

	// ObsCollectionCompleted records that every required observation query ran
	// successfully through the full inclusion window.
	ObsCollectionCompleted ObservationKind = "collection_completed"

	// ObsNetworkBaselineSampled records network-wide block-arrival percentiles
	// for one slot.
	ObsNetworkBaselineSampled ObservationKind = "network_baseline_sampled"
)

// ObservationKinds returns the closed vocabulary in taxonomy order. The returned
// slice is newly allocated; no mutable package-level registry exists.
func ObservationKinds() []ObservationKind {
	return []ObservationKind{
		ObsSlotStart,
		ObsDutyAssigned,
		ObsBlockSeen,
		ObsBlockSkipped,
		ObsHeadUpdated,
		ObsAttestationPublished,
		ObsAttestationIncluded,
		ObsBlockProposed,
		ObsReorg,
		ObsPeerCountSampled,
		ObsEngineCall,
		ObsHostSampled,
		ObsClockSampled,
		ObsCollectionCompleted,
		ObsNetworkBaselineSampled,
	}
}

// Valid reports whether the kind is in the taxonomy vocabulary.
func (k ObservationKind) Valid() bool {
	switch k {
	case ObsSlotStart, ObsDutyAssigned, ObsBlockSeen, ObsBlockSkipped,
		ObsHeadUpdated, ObsAttestationPublished, ObsAttestationIncluded,
		ObsBlockProposed, ObsReorg, ObsPeerCountSampled, ObsEngineCall,
		ObsHostSampled, ObsClockSampled, ObsCollectionCompleted,
		ObsNetworkBaselineSampled:
		return true
	default:
		return false
	}
}

// AttrKey is a documented key of [Observation.Attrs]. The set is closed and each
// key is permitted only for the kinds it makes sense for, so a typo in an adapter
// fails at construction rather than producing an observation nothing reads
// (docs/causes.md §8.1).
type AttrKey string

const (
	// AttrBlockRoot is the beacon block root.
	AttrBlockRoot AttrKey = "block_root"

	// AttrProposerIndex is the index of the validator that proposed the block.
	AttrProposerIndex AttrKey = "proposer_index"

	// AttrValidatorIndex is the index of the validator the duty belongs to.
	AttrValidatorIndex AttrKey = "validator_index"

	// AttrEngineMethod is the Engine API method name, such as newPayload.
	AttrEngineMethod AttrKey = "engine_method"

	// AttrDurationMS is a duration in milliseconds.
	AttrDurationMS AttrKey = "duration_ms"

	// AttrPeerCount is the number of connected peers.
	AttrPeerCount AttrKey = "peer_count"

	// AttrMetric names the host metric a sample carries, such as iowait_pct.
	AttrMetric AttrKey = "metric"

	// AttrValue is the numeric value of a host or clock sample.
	AttrValue AttrKey = "value"

	// AttrInclusionDelay is the distance in slots between attestation and
	// inclusion. A value of 1 is required for the timely-head reward flag.
	AttrInclusionDelay AttrKey = "inclusion_delay"

	// AttrHeadCorrect reports whether the attestation voted for the canonical
	// head root at its duty slot.
	AttrHeadCorrect AttrKey = "head_correct"

	// AttrTargetCorrect reports whether the attestation voted for the canonical
	// target root at its target epoch boundary.
	AttrTargetCorrect AttrKey = "target_correct"

	// AttrBlockArrivalP50MS is the network p50 block-arrival offset in milliseconds.
	AttrBlockArrivalP50MS AttrKey = "block_arrival_p50_ms"

	// AttrBlockArrivalP90MS is the network p90 block-arrival offset in milliseconds.
	AttrBlockArrivalP90MS AttrKey = "block_arrival_p90_ms"

	// AttrSampleCount is the number of observations behind a baseline.
	AttrSampleCount AttrKey = "sample_count"
)

const (
	// EngineMethodNewPayload is the normalized Engine newPayload method name.
	EngineMethodNewPayload = "newPayload"
	// EngineMethodForkchoiceUpdated is the normalized Engine forkchoiceUpdated method name.
	EngineMethodForkchoiceUpdated = "forkchoiceUpdated"
)

// AttrKeys returns every documented attribute key, sorted. Sorted rather than
// declaration-ordered because callers use it to render tables and to compare
// against docs/causes.md, both of which want a stable order.
func AttrKeys() []AttrKey {
	return []AttrKey{
		AttrBlockArrivalP50MS,
		AttrBlockArrivalP90MS,
		AttrBlockRoot,
		AttrDurationMS,
		AttrEngineMethod,
		AttrHeadCorrect,
		AttrInclusionDelay,
		AttrMetric,
		AttrPeerCount,
		AttrProposerIndex,
		AttrSampleCount,
		AttrTargetCorrect,
		AttrValidatorIndex,
		AttrValue,
	}
}

// PermittedFor reports whether the key may appear on an observation of this kind.
func (a AttrKey) PermittedFor(kind ObservationKind) bool {
	switch kind {
	case ObsDutyAssigned, ObsCollectionCompleted:
		return a == AttrValidatorIndex
	case ObsBlockSeen:
		return a == AttrBlockRoot || a == AttrProposerIndex
	case ObsHeadUpdated:
		return a == AttrBlockRoot
	case ObsAttestationPublished, ObsBlockProposed:
		return a == AttrValidatorIndex || a == AttrBlockRoot
	case ObsAttestationIncluded:
		switch a {
		case AttrValidatorIndex, AttrInclusionDelay, AttrBlockRoot, AttrHeadCorrect, AttrTargetCorrect:
			return true
		default:
			return false
		}
	case ObsPeerCountSampled:
		return a == AttrPeerCount
	case ObsEngineCall:
		return a == AttrEngineMethod || a == AttrDurationMS || a == AttrSampleCount
	case ObsHostSampled:
		return a == AttrMetric || a == AttrValue
	case ObsClockSampled:
		return a == AttrValue
	case ObsNetworkBaselineSampled:
		return a == AttrBlockArrivalP50MS || a == AttrBlockArrivalP90MS || a == AttrSampleCount
	case ObsSlotStart, ObsBlockSkipped, ObsReorg:
		return false
	default:
		return false
	}
}

// Observation is a single timestamped fact about a slot.
//
// Treat it as immutable. NewObservation copies the attribute map so a caller cannot
// mutate an observation the timeline already holds; mutating the Attrs of a value
// obtained from NewObservation is a bug, not a supported edit.
type Observation struct {
	Slot Slot            `json:"slot"`
	Kind ObservationKind `json:"kind"`

	// At is when the fact occurred, in UTC, already corrected for the clock offset
	// below.
	At time.Time `json:"at"`

	// ClockOffset is the NTP offset measured at sample time. It travels with the
	// observation because a timing verdict on an untrusted clock is forbidden, and
	// trust is decided per observation rather than globally (I-9).
	ClockOffset time.Duration `json:"clock_offset"`

	// ClockMeasured reports whether ClockOffset came from a real reading. Without
	// it a zero offset is ambiguous — a perfect clock and a clock nobody sampled
	// look identical, and I-9 requires treating the second as untrusted.
	ClockMeasured bool `json:"clock_measured,omitempty"`

	// ClockSampleAt is when the NTP exchange that produced ClockOffset
	// completed, corrected to UTC by that same offset. It makes freshness
	// auditable after persistence instead of turning ClockMeasured into an
	// unverifiable assertion.
	ClockSampleAt time.Time `json:"clock_sample_at,omitempty"`

	Source SourceID           `json:"source"`
	Attrs  map[AttrKey]string `json:"attrs,omitempty"`
}

// NewObservation validates a draft observation and returns a canonical copy.
//
// It rejects an unknown kind, an undocumented or misplaced attribute key, a zero or
// non-UTC timestamp, and an unattributed source. The returned value owns its
// attribute map.
func NewObservation(draft Observation) (Observation, error) {
	if err := draft.Validate(); err != nil {
		return Observation{}, err
	}
	out := draft
	if len(draft.Attrs) > 0 {
		out.Attrs = maps.Clone(draft.Attrs)
		if root, ok := out.Attrs[AttrBlockRoot]; ok {
			out.Attrs[AttrBlockRoot] = strings.ToLower(root)
		}
	} else {
		out.Attrs = nil
	}
	return out, nil
}

// Validate reports why the observation is not well formed, or nil.
func (o Observation) Validate() error {
	if !o.Kind.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidKind, o.Kind)
	}
	if o.At.IsZero() {
		return fmt.Errorf("%w: kind %q", ErrMissingTimestamp, o.Kind)
	}
	if o.At.Location() != time.UTC {
		return fmt.Errorf("%w: kind %q at %s", ErrNotUTC, o.Kind, o.At.Location())
	}
	if o.Source == "" {
		return fmt.Errorf("%w: kind %q", ErrMissingSource, o.Kind)
	}
	if !o.Source.Valid() {
		return fmt.Errorf("%w: unknown source %q", ErrMissingSource, o.Source)
	}
	if !sourcePermittedFor(o.Source, o.Kind) {
		return fmt.Errorf("%w: source %q on kind %q", ErrInvalidSource, o.Source, o.Kind)
	}
	if o.ClockMeasured {
		if o.ClockSampleAt.IsZero() {
			return fmt.Errorf("%w: measured observation kind %q has no clock_sample_at", ErrInvalidClock, o.Kind)
		}
		if o.ClockSampleAt.Location() != time.UTC {
			return fmt.Errorf("%w: clock_sample_at for kind %q is at %s", ErrInvalidClock, o.Kind, o.ClockSampleAt.Location())
		}
	} else if o.ClockOffset != 0 || !o.ClockSampleAt.IsZero() {
		return fmt.Errorf("%w: unmeasured observation kind %q carries clock data", ErrInvalidClock, o.Kind)
	}
	for key, value := range o.Attrs {
		if !key.PermittedFor(o.Kind) {
			return fmt.Errorf("%w: %q on kind %q", ErrInvalidAttr, key, o.Kind)
		}
		if value == "" {
			return fmt.Errorf("%w: %q on kind %q is empty", ErrInvalidAttr, key, o.Kind)
		}
		if len(value) > maxObservationAttrValueBytes {
			return fmt.Errorf("%w: %q on kind %q is %d bytes, limit is %d", ErrInvalidAttr, key, o.Kind, len(value), maxObservationAttrValueBytes)
		}
		if (key == AttrHeadCorrect || key == AttrTargetCorrect) && value != "true" && value != "false" {
			return fmt.Errorf("%w: %q must be true or false, got %q", ErrInvalidAttr, key, value)
		}
		if err := validateObservationAttr(key, value); err != nil {
			return fmt.Errorf("%w: %q on kind %q: %w", ErrInvalidAttr, key, o.Kind, err)
		}
	}
	return nil
}

func validateObservationAttr(key AttrKey, value string) error {
	switch key {
	case AttrProposerIndex, AttrValidatorIndex:
		if _, err := strconv.ParseUint(value, 10, 64); err != nil {
			return fmt.Errorf("must be an unsigned integer, got %q", value)
		}
	case AttrInclusionDelay, AttrSampleCount:
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil || parsed == 0 {
			return fmt.Errorf("must be a positive integer, got %q", value)
		}
	case AttrDurationMS, AttrBlockArrivalP50MS, AttrBlockArrivalP90MS:
		parsed, err := strconv.ParseFloat(value, 64)
		maxMilliseconds := float64(time.Duration(1<<63-1)) / float64(time.Millisecond)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 0 || parsed > maxMilliseconds {
			return fmt.Errorf("must be a finite non-negative duration in milliseconds, got %q", value)
		}
	case AttrPeerCount:
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 0 {
			return fmt.Errorf("must be a finite non-negative number, got %q", value)
		}
	case AttrValue:
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return fmt.Errorf("must be a finite number, got %q", value)
		}
	case AttrEngineMethod:
		if value != EngineMethodNewPayload && value != EngineMethodForkchoiceUpdated {
			return fmt.Errorf("must be %q or %q, got %q", EngineMethodNewPayload, EngineMethodForkchoiceUpdated, value)
		}
	case AttrBlockRoot:
		if len(value) != 66 || !strings.HasPrefix(value, "0x") {
			return fmt.Errorf("must be a 32-byte 0x-prefixed hex root, got %q", value)
		}
		if _, err := hex.DecodeString(value[2:]); err != nil {
			return fmt.Errorf("must be a 32-byte 0x-prefixed hex root: %w", err)
		}
	}
	return nil
}

func sourcePermittedFor(source SourceID, kind ObservationKind) bool {
	switch kind {
	case ObsSlotStart, ObsCollectionCompleted:
		return source == SourceDerived
	case ObsDutyAssigned, ObsBlockSkipped, ObsHeadUpdated, ObsAttestationPublished,
		ObsAttestationIncluded, ObsBlockProposed, ObsReorg:
		return source == SourceBeaconAPI
	case ObsBlockSeen:
		return source == SourceBeaconAPI || source == SourcePromScrape
	case ObsPeerCountSampled:
		// beaconapi was added when the peer count moved to the standardised
		// /eth/v1/node/peer_count endpoint; Lighthouse's Prometheus gauge
		// reports 0 while genuinely peered (see beaconapi.Client.PeerCount).
		// promscrape stays allowed so every already-recorded corpus scenario
		// remains valid — this widens the allow-list, it does not replace it.
		return source == SourceBeaconAPI || source == SourcePromScrape
	case ObsEngineCall:
		return source == SourcePromScrape
	case ObsHostSampled:
		return source == SourceHostMetrics
	case ObsClockSampled:
		return source == SourceClock
	case ObsNetworkBaselineSampled:
		// beaconapi was added when the baseline gained a path that polls the
		// independent node's own /eth/v1/beacon/headers/{slot} instead of
		// scraping its Prometheus surface, so a baseline needs only an API the
		// operator can reach rather than a second node they run (ADR-0025).
		// promscrape and xatu stay allowed; this widens the list.
		return source == SourceXatu || source == SourcePromScrape || source == SourceBeaconAPI
	default:
		return false
	}
}

// Attr returns the value of an attribute and whether it was present.
func (o Observation) Attr(key AttrKey) (string, bool) {
	v, ok := o.Attrs[key]
	return v, ok
}

// Offset returns how far into the slot the observation occurred, relative to the
// slot start. It may be negative for a fact recorded before the slot began, such as
// a duty assignment.
func (o Observation) Offset(slotStart time.Time) time.Duration {
	return o.At.Sub(slotStart)
}
