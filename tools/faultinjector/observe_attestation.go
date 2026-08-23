package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

const (
	maxCommitteesPerSlot      = 64
	maxValidatorsPerCommittee = 2048
	maxCanonicalRootLookback  = 64
)

// apiAttestation is the subset shared by the Beacon API's pool and block
// responses. Electra may concatenate several committees into one aggregation
// bitlist; committee_bits identifies their order (EIP-7549).
type apiAttestation struct {
	AggregationBits string `json:"aggregation_bits"`
	CommitteeBits   string `json:"committee_bits"`
	Data            struct {
		Slot            string `json:"slot"`
		Index           string `json:"index"`
		BeaconBlockRoot string `json:"beacon_block_root"`
		Target          struct {
			Epoch string `json:"epoch"`
			Root  string `json:"root"`
		} `json:"target"`
	} `json:"data"`
}

func attestationsIncludeValidator(atts []apiAttestation, wantSlot uint64, d duty, committeeLengths map[uint64]uint64) (included, needCommittees bool, match apiAttestation, err error) {
	wantSlotString := strconv.FormatUint(wantSlot, 10)
	for _, att := range atts {
		if att.Data.Slot != wantSlotString {
			continue
		}
		included, needCommittees, err = attestationIncludesValidator(att, d, committeeLengths)
		if err != nil || included || needCommittees {
			return included, needCommittees, att, err
		}
	}
	return false, false, apiAttestation{}, nil
}

func (o *Observer) attestationRewardEvidence(ctx context.Context, dutySlot uint64, att apiAttestation) (headCorrect, targetCorrect bool, err error) {
	if err := validateBeaconRoot(att.Data.BeaconBlockRoot); err != nil {
		return false, false, fmt.Errorf("attested head root: %w", err)
	}
	if err := validateBeaconRoot(att.Data.Target.Root); err != nil {
		return false, false, fmt.Errorf("attested target root: %w", err)
	}
	targetEpoch, err := strconv.ParseUint(att.Data.Target.Epoch, 10, 64)
	if err != nil {
		return false, false, fmt.Errorf("parse attestation target epoch %q: %w", att.Data.Target.Epoch, err)
	}
	wantEpoch := dutySlot / domain.SlotsPerEpoch
	if targetEpoch != wantEpoch {
		return false, false, fmt.Errorf("attestation target epoch %d does not match duty slot %d epoch %d", targetEpoch, dutySlot, wantEpoch)
	}
	canonicalHead, err := o.fetchCanonicalRootAtSlot(ctx, dutySlot)
	if err != nil {
		return false, false, fmt.Errorf("resolve canonical head for duty slot %d: %w", dutySlot, err)
	}
	canonicalTarget, err := o.fetchCanonicalRootAtSlot(ctx, targetEpoch*domain.SlotsPerEpoch)
	if err != nil {
		return false, false, fmt.Errorf("resolve canonical target for epoch %d: %w", targetEpoch, err)
	}
	return att.Data.BeaconBlockRoot == canonicalHead, att.Data.Target.Root == canonicalTarget, nil
}

func (o *Observer) fetchCanonicalRootAtSlot(ctx context.Context, slot uint64) (string, error) {
	for lookback := uint64(0); lookback <= maxCanonicalRootLookback && lookback <= slot; lookback++ {
		header, found, err := o.fetchBlockHeader(ctx, slot-lookback)
		if err != nil {
			return "", err
		}
		if !found {
			continue
		}
		if err := validateBeaconRoot(header.Root); err != nil {
			return "", fmt.Errorf("canonical root at or before slot %d: %w", slot, err)
		}
		return header.Root, nil
	}
	return "", fmt.Errorf("no canonical block found within %d slots at or before slot %d", maxCanonicalRootLookback, slot)
}

func validateBeaconRoot(root string) error {
	if !strings.HasPrefix(root, "0x") || len(root) != 66 {
		return fmt.Errorf("root %q is not 32-byte 0x-prefixed hex", root)
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(root, "0x")); err != nil {
		return fmt.Errorf("decode root %q: %w", root, err)
	}
	return nil
}

func attestationIncludesValidator(att apiAttestation, d duty, committeeLengths map[uint64]uint64) (included, needCommittees bool, err error) {
	if att.CommitteeBits == "" {
		if att.Data.Index != strconv.FormatUint(d.CommitteeIndex, 10) {
			return false, false, nil
		}
		included, err := bitSet(att.AggregationBits, d.ValidatorCommitteeIndex)
		return included, false, err
	}
	if att.Data.Index != "0" {
		return false, false, fmt.Errorf("electra attestation data index is %q, want 0", att.Data.Index)
	}
	indices, err := bitvectorIndices(att.CommitteeBits, maxCommitteesPerSlot)
	if err != nil {
		return false, false, fmt.Errorf("decode committee_bits: %w", err)
	}
	targetPosition := -1
	for i, index := range indices {
		if index == d.CommitteeIndex {
			targetPosition = i
			break
		}
	}
	if targetPosition < 0 {
		return false, false, nil
	}
	if targetPosition > 0 && committeeLengths == nil {
		return false, true, nil
	}

	var offset uint64
	for _, index := range indices[:targetPosition] {
		length, ok := committeeLengths[index]
		if !ok || length == 0 || length > maxValidatorsPerCommittee {
			return false, false, fmt.Errorf("committee %d has no valid length", index)
		}
		if offset > math.MaxUint64-length {
			return false, false, fmt.Errorf("committee aggregation offset overflow")
		}
		offset += length
	}
	if d.CommitteeLength == 0 || d.ValidatorCommitteeIndex >= d.CommitteeLength {
		return false, false, fmt.Errorf("validator committee position %d is outside committee length %d", d.ValidatorCommitteeIndex, d.CommitteeLength)
	}
	included, err = bitSet(att.AggregationBits, offset+d.ValidatorCommitteeIndex)
	if err != nil {
		return false, false, fmt.Errorf("decode aggregation_bits: %w", err)
	}
	return included, false, nil
}

func bitvectorIndices(hexBits string, bitLength uint64) ([]uint64, error) {
	if !strings.HasPrefix(hexBits, "0x") {
		return nil, fmt.Errorf("bitvector is not 0x-prefixed")
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(hexBits, "0x"))
	if err != nil {
		return nil, fmt.Errorf("decode hex: %w", err)
	}
	wantBytes := (bitLength + 7) / 8
	if uint64(len(raw)) != wantBytes {
		return nil, fmt.Errorf("bitvector is %d bytes, want %d", len(raw), wantBytes)
	}
	indices := make([]uint64, 0, bitLength)
	for index := uint64(0); index < bitLength; index++ {
		if raw[index/8]&(1<<(index%8)) != 0 {
			indices = append(indices, index)
		}
	}
	if len(indices) == 0 {
		return nil, fmt.Errorf("bitvector has no committee set")
	}
	return indices, nil
}

func (o *Observer) fetchCommitteeLengths(ctx context.Context, stateID string, slot, expectedCount uint64) (map[uint64]uint64, error) {
	var resp struct {
		Data []struct {
			Index      string   `json:"index"`
			Slot       string   `json:"slot"`
			Validators []string `json:"validators"`
		} `json:"data"`
	}
	endpoint := fmt.Sprintf("%s/eth/v1/beacon/states/%s/committees?slot=%d", o.BeaconAPI, url.PathEscape(stateID), slot)
	if err := getJSON(ctx, o.Client, endpoint, &resp); err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 || len(resp.Data) > maxCommitteesPerSlot {
		return nil, fmt.Errorf("slot %d returned %d committees, want 1..%d", slot, len(resp.Data), maxCommitteesPerSlot)
	}
	if expectedCount > 0 && uint64(len(resp.Data)) != expectedCount {
		return nil, fmt.Errorf("slot %d returned %d committees, duty reported %d", slot, len(resp.Data), expectedCount)
	}
	lengths := make(map[uint64]uint64, len(resp.Data))
	wantSlot := strconv.FormatUint(slot, 10)
	for _, committee := range resp.Data {
		index, err := strconv.ParseUint(committee.Index, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse committee index %q: %w", committee.Index, err)
		}
		if committee.Slot != wantSlot || index >= maxCommitteesPerSlot {
			return nil, fmt.Errorf("unexpected committee index=%d slot=%q for slot %d", index, committee.Slot, slot)
		}
		if len(committee.Validators) == 0 || len(committee.Validators) > maxValidatorsPerCommittee {
			return nil, fmt.Errorf("committee %d has invalid length %d", index, len(committee.Validators))
		}
		if _, duplicate := lengths[index]; duplicate {
			return nil, fmt.Errorf("committee %d appears more than once", index)
		}
		lengths[index] = uint64(len(committee.Validators))
	}
	return lengths, nil
}
