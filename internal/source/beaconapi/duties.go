package beaconapi

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

const maxAttesterDutyValidators = 64

// AttesterDuty is a validator's attester assignment, carrying one field
// beyond domain.Duty: ValidatorCommitteeIndex, its position within the
// committee's SSZ aggregation bitlist. That position is pure Beacon-API
// mechanics — needed to decode whether a specific validator's bit is set in
// a published attestation (see [Client.AttestationPublished] and
// [Client.CheckInclusion]) — not a fact about the world the frozen
// domain.Duty type needs to carry, which is why it lives here instead.
type AttesterDuty struct {
	domain.Duty
	ValidatorCommitteeIndex uint64
	CommitteeLength         uint64
	CommitteesAtSlot        uint64
}

// FetchAttesterDuties reads POST /eth/v1/validator/duties/attester/{epoch}
// for the given validators, returning one AttesterDuty per validator that
// has an attester duty in epoch.
//
// The standard Beacon API only guarantees this endpoint is computable for
// the current epoch and the one after it — querying further ahead reliably
// 400s against both Lighthouse and Prysm. Callers needing duties further out
// must wait and re-query once the target epoch is in range.
func (c *Client) FetchAttesterDuties(ctx context.Context, epoch domain.Epoch, validators []domain.ValidatorIndex) ([]AttesterDuty, error) {
	if len(validators) == 0 {
		return nil, fmt.Errorf("fetch attester duties for epoch %d: no validators requested", epoch)
	}
	if len(validators) > maxAttesterDutyValidators {
		return nil, fmt.Errorf("fetch attester duties for epoch %d: validator count %d exceeds limit %d", epoch, len(validators), maxAttesterDutyValidators)
	}
	requested := make(map[domain.ValidatorIndex]struct{}, len(validators))
	body := make([]string, len(validators))
	for i, v := range validators {
		if _, duplicate := requested[v]; duplicate {
			return nil, fmt.Errorf("fetch attester duties for epoch %d: validator %d requested more than once", epoch, v)
		}
		requested[v] = struct{}{}
		body[i] = strconv.FormatUint(uint64(v), 10)
	}

	var resp []struct {
		ValidatorIndex          string `json:"validator_index"`
		Slot                    string `json:"slot"`
		CommitteeIndex          string `json:"committee_index"`
		CommitteeLength         string `json:"committee_length"`
		CommitteesAtSlot        string `json:"committees_at_slot"`
		ValidatorCommitteeIndex string `json:"validator_committee_index"`
	}
	found, err := c.post(ctx, fmt.Sprintf("/eth/v1/validator/duties/attester/%d", epoch), body, &resp)
	if err != nil {
		return nil, fmt.Errorf("fetch attester duties for epoch %d: %w", epoch, err)
	}
	if !found {
		return nil, fmt.Errorf("fetch attester duties for epoch %d: not found", epoch)
	}
	if len(resp) > len(validators) {
		return nil, fmt.Errorf("fetch attester duties for epoch %d: response has %d entries for %d requested validators", epoch, len(resp), len(validators))
	}

	duties := make([]AttesterDuty, 0, len(resp))
	seen := make(map[domain.ValidatorIndex]struct{}, len(resp))
	for _, entry := range resp {
		slot, err0 := strconv.ParseUint(entry.Slot, 10, 64)
		vi, err1 := strconv.ParseUint(entry.ValidatorIndex, 10, 64)
		ci, err2 := strconv.ParseUint(entry.CommitteeIndex, 10, 64)
		cl, err3 := strconv.ParseUint(entry.CommitteeLength, 10, 64)
		cs, err4 := strconv.ParseUint(entry.CommitteesAtSlot, 10, 64)
		vci, err5 := strconv.ParseUint(entry.ValidatorCommitteeIndex, 10, 64)
		if err := errors.Join(err0, err1, err2, err3, err4, err5); err != nil {
			return nil, fmt.Errorf("fetch attester duties for epoch %d: parse entry: %w", epoch, err)
		}
		validatorIndex := domain.ValidatorIndex(vi)
		if _, ok := requested[validatorIndex]; !ok {
			return nil, fmt.Errorf("fetch attester duties for epoch %d: response contains unrequested validator %d", epoch, vi)
		}
		if _, duplicate := seen[validatorIndex]; duplicate {
			return nil, fmt.Errorf("fetch attester duties for epoch %d: response contains validator %d more than once", epoch, vi)
		}
		seen[validatorIndex] = struct{}{}
		if domain.Slot(slot).Epoch() != epoch {
			return nil, fmt.Errorf("fetch attester duties for epoch %d: response slot %d belongs to epoch %d", epoch, slot, domain.Slot(slot).Epoch())
		}
		if cl == 0 || cs == 0 || ci >= cs || vci >= cl {
			return nil, fmt.Errorf("fetch attester duties for epoch %d: invalid committee assignment index=%d length=%d committees_at_slot=%d validator_position=%d", epoch, ci, cl, cs, vci)
		}
		duties = append(duties, AttesterDuty{
			Duty: domain.Duty{
				Kind:           domain.DutyAttester,
				Slot:           domain.Slot(slot),
				ValidatorIndex: validatorIndex,
				CommitteeIndex: domain.CommitteeIndex(ci),
			},
			ValidatorCommitteeIndex: vci,
			CommitteeLength:         cl,
			CommitteesAtSlot:        cs,
		})
	}
	return duties, nil
}

// FetchProposerDuties reads GET /eth/v1/validator/duties/proposer/{epoch},
// returning one domain.Duty per slot in the epoch — every slot always has
// exactly one proposer duty, unlike attester duties which are per-validator.
func (c *Client) FetchProposerDuties(ctx context.Context, epoch domain.Epoch) ([]domain.Duty, error) {
	var resp []struct {
		ValidatorIndex string `json:"validator_index"`
		Slot           string `json:"slot"`
	}
	found, err := c.get(ctx, fmt.Sprintf("/eth/v1/validator/duties/proposer/%d", epoch), &resp)
	if err != nil {
		return nil, fmt.Errorf("fetch proposer duties for epoch %d: %w", epoch, err)
	}
	if !found {
		return nil, fmt.Errorf("fetch proposer duties for epoch %d: not found", epoch)
	}
	if len(resp) != domain.SlotsPerEpoch {
		return nil, fmt.Errorf("fetch proposer duties for epoch %d: response has %d entries, want %d", epoch, len(resp), domain.SlotsPerEpoch)
	}

	duties := make([]domain.Duty, 0, len(resp))
	seenSlots := make(map[domain.Slot]struct{}, domain.SlotsPerEpoch)
	for _, entry := range resp {
		slot, err0 := strconv.ParseUint(entry.Slot, 10, 64)
		vi, err1 := strconv.ParseUint(entry.ValidatorIndex, 10, 64)
		if err := errors.Join(err0, err1); err != nil {
			return nil, fmt.Errorf("fetch proposer duties for epoch %d: parse entry: %w", epoch, err)
		}
		dutySlot := domain.Slot(slot)
		if dutySlot.Epoch() != epoch {
			return nil, fmt.Errorf("fetch proposer duties for epoch %d: response slot %d belongs to epoch %d", epoch, slot, dutySlot.Epoch())
		}
		if _, duplicate := seenSlots[dutySlot]; duplicate {
			return nil, fmt.Errorf("fetch proposer duties for epoch %d: response contains slot %d more than once", epoch, slot)
		}
		seenSlots[dutySlot] = struct{}{}
		duties = append(duties, domain.Duty{
			Kind:           domain.DutyProposer,
			Slot:           dutySlot,
			ValidatorIndex: domain.ValidatorIndex(vi),
		})
	}
	return duties, nil
}
