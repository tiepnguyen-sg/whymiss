package beaconapi

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// blockAttestation is the subset of an Electra-style attestation this
// package reads out of a block body. Committee-bits (EIP-7549
// multi-committee aggregation) are deliberately not handled: a single
// committee's data plus that committee's aggregation bitlist is what
// [Client.CheckInclusion] decodes. A chain with enough validators to
// produce genuinely multi-committee aggregates would need this extended —
// a real gap, not a hidden one.
type blockAttestation struct {
	AggregationBits string `json:"aggregation_bits"`
	Data            struct {
		Slot  string `json:"slot"`
		Index string `json:"index"`
	} `json:"data"`
}

// fetchBlockBody reads GET /eth/v2/beacon/blocks/{slot} for its
// attestations only — [Client.CheckInclusion]'s one caller needs nothing
// else from this endpoint. found is false, with no error, when the node
// reports 404 for slot.
func (c *Client) fetchBlockBody(ctx context.Context, slot domain.Slot) (atts []blockAttestation, found bool, err error) {
	var envelope struct {
		Message struct {
			Body struct {
				Attestations []blockAttestation `json:"attestations"`
			} `json:"body"`
		} `json:"message"`
	}
	found, err = c.get(ctx, fmt.Sprintf("/eth/v2/beacon/blocks/%d", slot), &envelope)
	if err != nil {
		return nil, false, fmt.Errorf("fetch block %d: %w", slot, err)
	}
	if !found {
		return nil, false, nil
	}
	return envelope.Message.Body.Attestations, true, nil
}

// blockHeader is what fetchBlockHeader found for one slot.
type blockHeader struct {
	ProposerIndex domain.ValidatorIndex
	Root          string
}

// fetchBlockHeader reads GET /eth/v1/beacon/headers/{slot} — this endpoint,
// unlike /eth/v2/beacon/blocks/{slot}, reports the block's own root
// directly in its response body ("data.root"); the v2 blocks endpoint does
// not expose the root anywhere in its response (verified against a real
// Lighthouse node: no header or body field carries it — an earlier version
// of this logic read a "Eth-Consensus-Block-Root" response header that does
// not exist, which silently produced an empty root string every time).
func (c *Client) fetchBlockHeader(ctx context.Context, slot domain.Slot) (blockHeader, bool, error) {
	var resp struct {
		Root   string `json:"root"`
		Header struct {
			Message struct {
				ProposerIndex string `json:"proposer_index"`
			} `json:"message"`
		} `json:"header"`
	}
	found, err := c.get(ctx, fmt.Sprintf("/eth/v1/beacon/headers/%d", slot), &resp)
	if err != nil {
		return blockHeader{}, false, fmt.Errorf("fetch block header %d: %w", slot, err)
	}
	if !found {
		return blockHeader{}, false, nil
	}
	pi, err := strconv.ParseUint(resp.Header.Message.ProposerIndex, 10, 64)
	if err != nil {
		return blockHeader{}, false, fmt.Errorf("fetch block header %d: parse proposer_index %q: %w", slot, resp.Header.Message.ProposerIndex, err)
	}
	return blockHeader{ProposerIndex: domain.ValidatorIndex(pi), Root: resp.Root}, true, nil
}

// BlockSeen polls GET /eth/v1/beacon/headers/{slot} every 500ms until it
// appears or deadline passes, returning an ObsBlockSeen observation the
// instant it does. A caller polling this is the collector's real-time
// substitute for the block SSE topic's "block" event — the SSE stream is
// used for the head/reorg topics this package doesn't have a clean REST
// polling substitute for (see [Client.Stream]); block existence is cheap
// enough to poll directly and doing so avoids a second connection just for
// this one fact.
func (c *Client) BlockSeen(ctx context.Context, slot domain.Slot, deadline time.Time) (domain.Observation, bool, error) {
	const pollInterval = 500 * time.Millisecond
	for {
		header, found, err := c.fetchBlockHeader(ctx, slot)
		if err != nil {
			return domain.Observation{}, false, err
		}
		if found {
			obs, err := domain.NewObservation(domain.Observation{
				Slot:   slot,
				Kind:   domain.ObsBlockSeen,
				At:     time.Now().UTC(),
				Source: domain.SourceBeaconAPI,
				Attrs: map[domain.AttrKey]string{
					domain.AttrProposerIndex: strconv.FormatUint(uint64(header.ProposerIndex), 10),
					domain.AttrBlockRoot:     header.Root,
				},
			})
			if err != nil {
				return domain.Observation{}, false, fmt.Errorf("build block_seen observation for slot %d: %w", slot, err)
			}
			return obs, true, nil
		}
		if !time.Now().Before(deadline) {
			return domain.Observation{}, false, nil
		}
		select {
		case <-ctx.Done():
			return domain.Observation{}, false, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// CheckInclusion looks for wantSlot's attester duty d's attestation in the
// block body of every slot from wantSlot+1 up to untilSlot, retrying once a
// second until one is found or deadline passes. Returns an
// ObsAttestationIncluded observation carrying the inclusion delay
// (docs/causes.md's inclusion_delay attribute: the block slot minus
// wantSlot, where 1 is required for the timely-head reward flag).
func (c *Client) CheckInclusion(ctx context.Context, wantSlot domain.Slot, d AttesterDuty, untilSlot domain.Slot, deadline time.Time) (domain.Observation, bool, error) {
	for {
		for s := wantSlot + 1; s <= untilSlot; s++ {
			atts, found, err := c.fetchBlockBody(ctx, s)
			if err != nil {
				return domain.Observation{}, false, err
			}
			if !found {
				continue
			}
			included, err := blockIncludesAttestation(atts, wantSlot, d)
			if err != nil {
				return domain.Observation{}, false, fmt.Errorf("check inclusion in slot %d: %w", s, err)
			}
			if !included {
				continue
			}
			obs, err := domain.NewObservation(domain.Observation{
				Slot:   wantSlot,
				Kind:   domain.ObsAttestationIncluded,
				At:     time.Now().UTC(),
				Source: domain.SourceBeaconAPI,
				Attrs: map[domain.AttrKey]string{
					domain.AttrValidatorIndex: strconv.FormatUint(uint64(d.ValidatorIndex), 10),
					domain.AttrInclusionDelay: strconv.FormatUint(uint64(s-wantSlot), 10),
				},
			})
			if err != nil {
				return domain.Observation{}, false, fmt.Errorf("build attestation_included observation for slot %d: %w", wantSlot, err)
			}
			return obs, true, nil
		}
		if !time.Now().Before(deadline) {
			return domain.Observation{}, false, nil
		}
		select {
		case <-ctx.Done():
			return domain.Observation{}, false, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func blockIncludesAttestation(atts []blockAttestation, wantSlot domain.Slot, d AttesterDuty) (bool, error) {
	wantSlotStr := strconv.FormatUint(uint64(wantSlot), 10)
	wantIndexStr := strconv.FormatUint(uint64(d.CommitteeIndex), 10)
	for _, att := range atts {
		if att.Data.Slot != wantSlotStr || att.Data.Index != wantIndexStr {
			continue
		}
		included, err := bitSet(att.AggregationBits, d.ValidatorCommitteeIndex)
		if err != nil {
			return false, fmt.Errorf("decode aggregation_bits: %w", err)
		}
		if included {
			return true, nil
		}
	}
	return false, nil
}
