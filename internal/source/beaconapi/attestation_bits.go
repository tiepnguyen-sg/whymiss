package beaconapi

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

const (
	maxCommitteesPerSlot      = 64
	maxValidatorsPerCommittee = 2048
	maxCommitteeCacheEntries  = 64
	committeeCacheTTL         = time.Minute
)

type committeeCacheEntry struct {
	ready         chan struct{}
	lengths       map[domain.CommitteeIndex]uint64
	expectedCount uint64
	err           error
	complete      bool
	fetched       time.Time
}

func attestationsIncludeValidator(atts []apiAttestation, wantSlot domain.Slot, d AttesterDuty, committeeLengths map[domain.CommitteeIndex]uint64) (included, needCommittees bool, match apiAttestation, err error) {
	wantSlotString := strconv.FormatUint(uint64(wantSlot), 10)
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

func attestationIncludesValidator(att apiAttestation, d AttesterDuty, committeeLengths map[domain.CommitteeIndex]uint64) (included, needCommittees bool, err error) {
	if att.CommitteeBits == "" {
		if att.Data.Index != strconv.FormatUint(uint64(d.CommitteeIndex), 10) {
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
		if index == uint64(d.CommitteeIndex) {
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
		length, ok := committeeLengths[domain.CommitteeIndex(index)]
		if !ok || length == 0 || length > maxValidatorsPerCommittee {
			return false, false, fmt.Errorf("committee %d has no valid length", index)
		}
		if offset > math.MaxUint64-length {
			return false, false, fmt.Errorf("committee aggregation offset overflow")
		}
		offset += length
	}
	if d.CommitteeLength > 0 && d.ValidatorCommitteeIndex >= d.CommitteeLength {
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

func (c *Client) fetchCommitteeLengths(ctx context.Context, stateID string, slot domain.Slot, expectedCount uint64) (map[domain.CommitteeIndex]uint64, error) {
	var committees []struct {
		Index      string   `json:"index"`
		Slot       string   `json:"slot"`
		Validators []string `json:"validators"`
	}
	path := fmt.Sprintf("/eth/v1/beacon/states/%s/committees?slot=%d", url.PathEscape(stateID), slot)
	found, err := c.get(ctx, path, &committees)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("committee assignments for slot %d not found", slot)
	}
	if len(committees) == 0 || len(committees) > maxCommitteesPerSlot {
		return nil, fmt.Errorf("slot %d returned %d committees, want 1..%d", slot, len(committees), maxCommitteesPerSlot)
	}
	if expectedCount > 0 && uint64(len(committees)) != expectedCount {
		return nil, fmt.Errorf("slot %d returned %d committees, duty reported %d", slot, len(committees), expectedCount)
	}
	lengths := make(map[domain.CommitteeIndex]uint64, len(committees))
	wantSlot := strconv.FormatUint(uint64(slot), 10)
	for _, committee := range committees {
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
		key := domain.CommitteeIndex(index)
		if _, duplicate := lengths[key]; duplicate {
			return nil, fmt.Errorf("committee %d appears more than once", index)
		}
		lengths[key] = uint64(len(committee.Validators))
	}
	return lengths, nil
}

func (c *Client) committeeLengthsForSlot(ctx context.Context, slot domain.Slot, expectedCount uint64) (map[domain.CommitteeIndex]uint64, error) {
	key := uint64(slot)
	c.committeeMu.Lock()
	if entry, ok := c.committeeCache[key]; ok {
		if entry.expectedCount == expectedCount && (!entry.complete || time.Since(entry.fetched) < committeeCacheTTL) {
			ready := entry.ready
			c.committeeMu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-ready:
				return entry.lengths, entry.err
			}
		}
		delete(c.committeeCache, key)
	}
	entry := &committeeCacheEntry{ready: make(chan struct{}), expectedCount: expectedCount}
	c.committeeCache[key] = entry
	c.committeeMu.Unlock()

	lengths, err := c.fetchCommitteeLengths(ctx, "head", slot, expectedCount)
	c.committeeMu.Lock()
	entry.lengths, entry.err, entry.complete, entry.fetched = lengths, err, true, time.Now()
	close(entry.ready)
	if err != nil {
		if c.committeeCache[key] == entry {
			delete(c.committeeCache, key)
		}
	} else {
		c.trimCommitteeCacheLocked()
	}
	c.committeeMu.Unlock()
	return lengths, err
}

func (c *Client) trimCommitteeCacheLocked() {
	for len(c.committeeCache) > maxCommitteeCacheEntries {
		var oldest uint64
		haveOldest := false
		for slot, entry := range c.committeeCache {
			if entry.complete && (!haveOldest || slot < oldest) {
				oldest, haveOldest = slot, true
			}
		}
		if !haveOldest {
			return
		}
		delete(c.committeeCache, oldest)
	}
}
