package domain

import (
	"fmt"
	"maps"
	"sort"
	"time"
)

// ObservationKind is a closed vocabulary: the kinds of fact whymiss is able to
// record. Adding one is a taxonomy change — a minor version bump and an ADR
// (docs/causes.md §8, ADR-0005).
type ObservationKind string

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

	// ObsEngineCall records the duration of one Engine API call.
	ObsEngineCall ObservationKind = "engine_call"

	// ObsHostSampled records one host resource measurement.
	ObsHostSampled ObservationKind = "host_sampled"

	// ObsClockSampled records one NTP offset measurement (I-9).
	ObsClockSampled ObservationKind = "clock_sampled"
)

// observationKinds is the closed set, in the order docs/causes.md §8 lists it.
var observationKinds = []ObservationKind{
	ObsSlotStart,
	ObsDutyAssigned,
	ObsBlockSeen,
	ObsHeadUpdated,
	ObsAttestationPublished,
	ObsAttestationIncluded,
	ObsBlockProposed,
	ObsReorg,
	ObsPeerCountSampled,
	ObsEngineCall,
	ObsHostSampled,
	ObsClockSampled,
}

// ObservationKinds returns the closed vocabulary in taxonomy order. The returned
// slice is a copy; mutating it does not change the vocabulary.
func ObservationKinds() []ObservationKind {
	out := make([]ObservationKind, len(observationKinds))
	copy(out, observationKinds)
	return out
}

// Valid reports whether the kind is in the taxonomy vocabulary.
func (k ObservationKind) Valid() bool {
	_, ok := permittedAttrs[k]
	return ok
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

	// AttrDurationMS is a duration in whole milliseconds.
	AttrDurationMS AttrKey = "duration_ms"

	// AttrPeerCount is the number of connected peers.
	AttrPeerCount AttrKey = "peer_count"

	// AttrSubnetID is the attestation subnet the peer count applies to.
	AttrSubnetID AttrKey = "subnet_id"

	// AttrMetric names the host metric a sample carries, such as iowait_pct.
	AttrMetric AttrKey = "metric"

	// AttrValue is the numeric value of a host or clock sample.
	AttrValue AttrKey = "value"

	// AttrInclusionDelay is the distance in slots between attestation and
	// inclusion. A value of 1 is required for the timely-head reward flag.
	AttrInclusionDelay AttrKey = "inclusion_delay"
)

// permittedAttrs maps each kind to the keys it may carry. A kind absent from this
// map is not in the vocabulary, which makes the map the single definition of both
// closed sets — they cannot drift apart.
var permittedAttrs = map[ObservationKind]map[AttrKey]struct{}{
	ObsSlotStart: {},
	ObsDutyAssigned: {
		AttrValidatorIndex: {},
	},
	ObsBlockSeen: {
		AttrBlockRoot:     {},
		AttrProposerIndex: {},
	},
	ObsHeadUpdated: {
		AttrBlockRoot: {},
	},
	ObsAttestationPublished: {
		AttrValidatorIndex: {},
		AttrBlockRoot:      {},
	},
	ObsAttestationIncluded: {
		AttrValidatorIndex: {},
		AttrInclusionDelay: {},
		AttrBlockRoot:      {},
	},
	ObsBlockProposed: {
		AttrValidatorIndex: {},
		AttrBlockRoot:      {},
	},
	ObsReorg: {},
	ObsPeerCountSampled: {
		AttrPeerCount: {},
		AttrSubnetID:  {},
	},
	ObsEngineCall: {
		AttrEngineMethod: {},
		AttrDurationMS:   {},
	},
	ObsHostSampled: {
		AttrMetric: {},
		AttrValue:  {},
	},
	ObsClockSampled: {
		AttrValue: {},
	},
}

// AttrKeys returns every documented attribute key, sorted. Sorted rather than
// declaration-ordered because callers use it to render tables and to compare
// against docs/causes.md, both of which want a stable order.
func AttrKeys() []AttrKey {
	seen := map[AttrKey]struct{}{}
	for _, keys := range permittedAttrs {
		for k := range keys {
			seen[k] = struct{}{}
		}
	}
	out := make([]AttrKey, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// PermittedFor reports whether the key may appear on an observation of this kind.
func (a AttrKey) PermittedFor(kind ObservationKind) bool {
	keys, ok := permittedAttrs[kind]
	if !ok {
		return false
	}
	_, ok = keys[a]
	return ok
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
	for key := range o.Attrs {
		if !key.PermittedFor(o.Kind) {
			return fmt.Errorf("%w: %q on kind %q", ErrInvalidAttr, key, o.Kind)
		}
	}
	return nil
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
