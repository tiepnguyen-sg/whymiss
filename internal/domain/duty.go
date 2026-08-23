package domain

import "fmt"

// DutyKind is the kind of duty a validator owed for a slot.
//
// Only the two duties whymiss attributes in v1 are present. Aggregator and sync
// committee are deferred for low reward impact, and PTC arrives with ePBS behind a
// feature flag (docs/causes.md §2.1). Adding one is a taxonomy change.
type DutyKind string

const (
	// DutyAttester is an attestation duty — the common case, and the one operators
	// lose rewards on.
	DutyAttester DutyKind = "attester"

	// DutyProposer is a block-proposal duty. Rare per validator, and expensive to
	// miss.
	DutyProposer DutyKind = "proposer"
)

// Valid reports whether the duty kind is one whymiss attributes in v1.
func (d DutyKind) Valid() bool {
	switch d {
	case DutyAttester, DutyProposer:
		return true
	default:
		return false
	}
}

// CommitteeIndex identifies an attestation committee within a slot. It is used to
// locate the validator's aggregation bit in pre-Electra and Electra attestations.
type CommitteeIndex uint64

// Duty is what the validator owed for one slot. A nil *Duty on a [Timeline] means
// nothing was owed, which is a different statement from a duty that was missed.
type Duty struct {
	Kind           DutyKind       `json:"kind"`
	Slot           Slot           `json:"slot"`
	ValidatorIndex ValidatorIndex `json:"validator_index"`

	// CommitteeIndex is meaningful for an attester duty only. It is zero and
	// ignored for a proposer duty.
	CommitteeIndex CommitteeIndex `json:"committee_index,omitempty"`
}

// Validate reports why the duty is not well formed, or nil.
func (d Duty) Validate() error {
	if !d.Kind.Valid() {
		return fmt.Errorf("%w: duty kind %q", ErrInvalidKind, d.Kind)
	}
	return nil
}
