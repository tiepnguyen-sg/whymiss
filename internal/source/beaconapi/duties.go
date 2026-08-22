package beaconapi

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

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
	body := make([]string, len(validators))
	for i, v := range validators {
		body[i] = strconv.FormatUint(uint64(v), 10)
	}

	var resp []struct {
		ValidatorIndex          string `json:"validator_index"`
		Slot                    string `json:"slot"`
		CommitteeIndex          string `json:"committee_index"`
		ValidatorCommitteeIndex string `json:"validator_committee_index"`
	}
	found, err := c.post(ctx, fmt.Sprintf("/eth/v1/validator/duties/attester/%d", epoch), body, &resp)
	if err != nil {
		return nil, fmt.Errorf("fetch attester duties for epoch %d: %w", epoch, err)
	}
	if !found {
		return nil, fmt.Errorf("fetch attester duties for epoch %d: not found", epoch)
	}

	duties := make([]AttesterDuty, 0, len(resp))
	for _, entry := range resp {
		slot, err0 := strconv.ParseUint(entry.Slot, 10, 64)
		vi, err1 := strconv.ParseUint(entry.ValidatorIndex, 10, 64)
		ci, err2 := strconv.ParseUint(entry.CommitteeIndex, 10, 64)
		vci, err3 := strconv.ParseUint(entry.ValidatorCommitteeIndex, 10, 64)
		if err := errors.Join(err0, err1, err2, err3); err != nil {
			return nil, fmt.Errorf("fetch attester duties for epoch %d: parse entry: %w", epoch, err)
		}
		duties = append(duties, AttesterDuty{
			Duty: domain.Duty{
				Kind:           domain.DutyAttester,
				Slot:           domain.Slot(slot),
				ValidatorIndex: domain.ValidatorIndex(vi),
				CommitteeIndex: domain.CommitteeIndex(ci),
			},
			ValidatorCommitteeIndex: vci,
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

	duties := make([]domain.Duty, 0, len(resp))
	for _, entry := range resp {
		slot, err0 := strconv.ParseUint(entry.Slot, 10, 64)
		vi, err1 := strconv.ParseUint(entry.ValidatorIndex, 10, 64)
		if err := errors.Join(err0, err1); err != nil {
			return nil, fmt.Errorf("fetch proposer duties for epoch %d: parse entry: %w", epoch, err)
		}
		duties = append(duties, domain.Duty{
			Kind:           domain.DutyProposer,
			Slot:           domain.Slot(slot),
			ValidatorIndex: domain.ValidatorIndex(vi),
		})
	}
	return duties, nil
}
