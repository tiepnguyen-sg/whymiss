package beaconapi

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

type blockAttestation = apiAttestation

const (
	maxBlockCacheEntries      = 128
	maxBlockFetches           = 192
	blockCacheTTL             = time.Minute
	maxPreElectraAttestations = 128
	maxElectraAttestations    = 8
)

type endpointSupport uint8

const (
	endpointUnknown endpointSupport = iota
	endpointSupported
	endpointUnsupported
)

type blockCacheEntry struct {
	ready    chan struct{}
	atts     []blockAttestation
	found    bool
	err      error
	complete bool
	fetched  time.Time
}

// fetchBlockBody coalesces concurrent reads of the same inclusion block and
// retains a small bounded cache. A node with many validators otherwise receives
// the same block request once per validator duty.
func (c *Client) fetchBlockBody(ctx context.Context, slot domain.Slot) (atts []blockAttestation, found bool, err error) {
	key := uint64(slot)
	c.blockMu.Lock()
	if entry, ok := c.blockCache[key]; ok {
		if entry.complete && time.Since(entry.fetched) >= blockCacheTTL {
			delete(c.blockCache, key)
		} else {
			ready := entry.ready
			c.blockMu.Unlock()
			select {
			case <-ctx.Done():
				return nil, false, ctx.Err()
			case <-ready:
				return entry.atts, entry.found, entry.err
			}
		}
	}
	if len(c.blockCache) >= maxBlockFetches {
		c.blockMu.Unlock()
		return c.fetchBlockBodyUncached(ctx, slot)
	}
	entry := &blockCacheEntry{ready: make(chan struct{})}
	c.blockCache[key] = entry
	c.blockMu.Unlock()

	atts, found, err = c.fetchBlockBodyUncached(ctx, slot)
	if err == nil && found {
		err = validateBlockAttestations(atts)
	}
	c.blockMu.Lock()
	entry.atts, entry.found, entry.err, entry.complete, entry.fetched = atts, found, err, true, time.Now()
	close(entry.ready)
	if err != nil {
		delete(c.blockCache, key)
	} else {
		c.trimBlockCacheLocked()
	}
	c.blockMu.Unlock()
	return atts, found, err
}

func validateBlockAttestations(atts []blockAttestation) error {
	electra := false
	for _, att := range atts {
		if att.CommitteeBits != "" {
			electra = true
			break
		}
	}
	limit := maxPreElectraAttestations
	maxAggregationBits := uint64(maxValidatorsPerCommittee)
	if electra {
		limit = maxElectraAttestations
		maxAggregationBits = uint64(maxValidatorsPerCommittee * maxCommitteesPerSlot)
	}
	if len(atts) > limit {
		return fmt.Errorf("block contains %d attestations, limit is %d", len(atts), limit)
	}
	for i, att := range atts {
		if electra && att.CommitteeBits == "" {
			return fmt.Errorf("block attestation %d mixes pre-Electra and Electra forms", i)
		}
		for _, field := range []struct{ name, value string }{
			{name: "slot", value: att.Data.Slot},
			{name: "index", value: att.Data.Index},
			{name: "target epoch", value: att.Data.Target.Epoch},
		} {
			name, value := field.name, field.value
			if len(value) > 20 {
				return fmt.Errorf("block attestation %d %s exceeds 20 digits", i, name)
			}
			if _, err := strconv.ParseUint(value, 10, 64); err != nil {
				return fmt.Errorf("block attestation %d has invalid %s %q: %w", i, name, value, err)
			}
		}
		if err := validateBeaconRoot(att.Data.BeaconBlockRoot); err != nil {
			return fmt.Errorf("block attestation %d beacon block root: %w", i, err)
		}
		if err := validateBeaconRoot(att.Data.Target.Root); err != nil {
			return fmt.Errorf("block attestation %d target root: %w", i, err)
		}
		if err := validateBoundedBitlist("aggregation_bits", att.AggregationBits, maxAggregationBits); err != nil {
			return fmt.Errorf("block attestation %d: %w", i, err)
		}
		if electra {
			if att.Data.Index != "0" {
				return fmt.Errorf("block attestation %d Electra data index is %q, want 0", i, att.Data.Index)
			}
			if len(att.CommitteeBits) != 18 {
				return fmt.Errorf("block attestation %d committee_bits is %d characters, want 18", i, len(att.CommitteeBits))
			}
			if err := validateBoundedHex("committee_bits", att.CommitteeBits, 18); err != nil {
				return fmt.Errorf("block attestation %d: %w", i, err)
			}
		}
	}
	return nil
}

func validateBoundedBitlist(name, value string, maxBits uint64) error {
	maxBytes := (maxBits + 1 + 7) / 8
	if maxBytes > uint64(maxResponseBodyBytes) {
		return fmt.Errorf("%s maximum bit length %d is unsupported", name, maxBits)
	}
	maxLength := 2 + 2*int(maxBytes) //nolint:gosec // maxBytes is checked against the 16 MiB response ceiling above
	if err := validateBoundedHex(name, value, maxLength); err != nil {
		return err
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	if err != nil {
		return fmt.Errorf("decode %s: %w", name, err)
	}
	length := bitlistLength(raw)
	if length == 0 {
		return fmt.Errorf("%s has zero encoded bit length", name)
	}
	if length > maxBits {
		return fmt.Errorf("%s encodes %d bits, maximum is %d", name, length, maxBits)
	}
	return nil
}

func validateBoundedHex(name, value string, maxLength int) error {
	if !strings.HasPrefix(value, "0x") || len(value) < 4 || len(value) > maxLength || len(value)%2 != 0 {
		return fmt.Errorf("%s has invalid encoded length %d (maximum %d)", name, len(value), maxLength)
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(value, "0x")); err != nil {
		return fmt.Errorf("decode %s: %w", name, err)
	}
	return nil
}

func (c *Client) trimBlockCacheLocked() {
	for len(c.blockCache) > maxBlockCacheEntries {
		var oldest uint64
		haveOldest := false
		for slot, entry := range c.blockCache {
			if entry.complete && (!haveOldest || slot < oldest) {
				oldest, haveOldest = slot, true
			}
		}
		if !haveOldest {
			return
		}
		delete(c.blockCache, oldest)
	}
}

// fetchBlockBodyUncached prefers the standard attestations-only endpoint. It
// falls back to the legacy full-block response for Lighthouse/Prysm versions
// that predate Beacon API v3, then remembers endpoint support for the process.
func (c *Client) fetchBlockBodyUncached(ctx context.Context, slot domain.Slot) (atts []blockAttestation, found bool, err error) {
	if c.blockEndpointSupport() != endpointUnsupported {
		found, err = c.get(ctx, fmt.Sprintf("/eth/v2/beacon/blocks/%d/attestations", slot), &atts)
		if err == nil && found {
			c.setBlockEndpointSupport(endpointSupported)
			return atts, true, nil
		}
		if err != nil && !unsupportedEndpointError(err) {
			return nil, false, fmt.Errorf("fetch block %d attestations: %w", slot, err)
		}
		if err != nil {
			c.setBlockEndpointSupport(endpointUnsupported)
		} else if c.blockEndpointSupport() == endpointSupported {
			return nil, false, nil
		}
	}

	return c.fetchLegacyBlockBody(ctx, slot)
}

func (c *Client) fetchLegacyBlockBody(ctx context.Context, slot domain.Slot) (atts []blockAttestation, found bool, err error) {
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
	if c.blockEndpointSupport() == endpointUnknown {
		c.setBlockEndpointSupport(endpointUnsupported)
	}
	return envelope.Message.Body.Attestations, true, nil
}

func (c *Client) blockEndpointSupport() endpointSupport {
	c.blockMu.Lock()
	defer c.blockMu.Unlock()
	return c.blockAttestationsEndpoint
}

func (c *Client) setBlockEndpointSupport(support endpointSupport) {
	c.blockMu.Lock()
	c.blockAttestationsEndpoint = support
	c.blockMu.Unlock()
}

func unsupportedEndpointError(err error) bool {
	var statusErr *httpStatusError
	return errors.As(err, &statusErr) && (statusErr.statusCode == http.StatusMethodNotAllowed || statusErr.statusCode == http.StatusNotImplemented)
}

// blockHeader is what fetchBlockHeader found for one slot.
type blockHeader struct {
	Slot          domain.Slot
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
	header, found, err := c.fetchHeader(ctx, strconv.FormatUint(uint64(slot), 10))
	if err != nil || !found {
		return blockHeader{}, found, err
	}
	if header.Slot != slot {
		return blockHeader{}, false, fmt.Errorf("fetch block header %d: response is for slot %d", slot, header.Slot)
	}
	return header, true, nil
}

func (c *Client) fetchHeader(ctx context.Context, id string) (blockHeader, bool, error) {
	var resp struct {
		ExecutionOptimistic *bool `json:"execution_optimistic"`
		Data                struct {
			Root      string `json:"root"`
			Canonical bool   `json:"canonical"`
			Header    struct {
				Message struct {
					Slot          string `json:"slot"`
					ProposerIndex string `json:"proposer_index"`
				} `json:"message"`
			} `json:"header"`
		} `json:"data"`
	}
	found, err := c.getEnvelope(ctx, "/eth/v1/beacon/headers/"+id, &resp)
	if err != nil {
		return blockHeader{}, false, fmt.Errorf("fetch block header %s: %w", id, err)
	}
	if !found {
		return blockHeader{}, false, nil
	}
	if resp.ExecutionOptimistic == nil {
		return blockHeader{}, false, fmt.Errorf("fetch block header %s: response has no execution_optimistic status", id)
	}
	if *resp.ExecutionOptimistic || !resp.Data.Canonical {
		return blockHeader{}, false, nil
	}
	slot, err := strconv.ParseUint(resp.Data.Header.Message.Slot, 10, 64)
	if err != nil {
		return blockHeader{}, false, fmt.Errorf("fetch block header %s: parse slot %q: %w", id, resp.Data.Header.Message.Slot, err)
	}
	pi, err := strconv.ParseUint(resp.Data.Header.Message.ProposerIndex, 10, 64)
	if err != nil {
		return blockHeader{}, false, fmt.Errorf("fetch block header %s: parse proposer_index %q: %w", id, resp.Data.Header.Message.ProposerIndex, err)
	}
	if err := validateBeaconRoot(resp.Data.Root); err != nil {
		return blockHeader{}, false, fmt.Errorf("fetch block header %s: %w", id, err)
	}
	return blockHeader{Slot: domain.Slot(slot), ProposerIndex: domain.ValidatorIndex(pi), Root: resp.Data.Root}, true, nil
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
			return buildBlockSeen(slot, header)
		}
		if !time.Now().Before(deadline) {
			return c.blockStatusAtDeadline(ctx, slot)
		}
		select {
		case <-ctx.Done():
			return domain.Observation{}, false, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func buildBlockSeen(slot domain.Slot, header blockHeader) (domain.Observation, bool, error) {
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

func (c *Client) blockStatusAtDeadline(ctx context.Context, slot domain.Slot) (domain.Observation, bool, error) {
	synced, err := c.nodeReadyForCanonicalQuery(ctx, slot)
	if err != nil {
		return domain.Observation{}, false, fmt.Errorf("confirm node sync before checking skipped slot %d: %w", slot, err)
	}
	if !synced {
		return domain.Observation{}, false, fmt.Errorf("cannot confirm slot %d as seen or skipped: node is not fully synced, execution-valid, and past the slot", slot)
	}
	header, found, err := c.fetchBlockHeader(ctx, slot)
	if err != nil {
		return domain.Observation{}, false, err
	}
	if found {
		return buildBlockSeen(slot, header)
	}
	obs, err := domain.NewObservation(domain.Observation{
		Slot: slot, Kind: domain.ObsBlockSkipped, At: time.Now().UTC(), Source: domain.SourceBeaconAPI,
	})
	if err != nil {
		return domain.Observation{}, false, fmt.Errorf("build block_skipped observation for slot %d: %w", slot, err)
	}
	return obs, true, nil
}

func (c *Client) nodeReadyForCanonicalQuery(ctx context.Context, slot domain.Slot) (bool, error) {
	var status struct {
		HeadSlot     string `json:"head_slot"`
		SyncDistance string `json:"sync_distance"`
		IsSyncing    bool   `json:"is_syncing"`
		IsOptimistic bool   `json:"is_optimistic"`
		ELOffline    bool   `json:"el_offline"`
	}
	found, err := c.get(ctx, "/eth/v1/node/syncing", &status)
	if err != nil {
		return false, err
	}
	if !found {
		return false, fmt.Errorf("node syncing status not found")
	}
	headSlot, err := strconv.ParseUint(status.HeadSlot, 10, 64)
	if err != nil {
		return false, fmt.Errorf("parse head_slot %q: %w", status.HeadSlot, err)
	}
	distance, err := strconv.ParseUint(status.SyncDistance, 10, 64)
	if err != nil {
		return false, fmt.Errorf("parse sync_distance %q: %w", status.SyncDistance, err)
	}
	return !status.IsSyncing && !status.IsOptimistic && !status.ELOffline && distance == 0 && headSlot > uint64(slot), nil
}

// CheckInclusion waits until the validated canonical head reaches untilSlot,
// then looks for wantSlot's attester duty d's attestation in the current
// canonical block at every slot from wantSlot+1 through untilSlot. Delaying the
// scan avoids saturating the node with requests for future slots and ensures an
// early inclusion is not accepted before the full reorg-sensitive window closes.
// Returns an
// ObsAttestationIncluded observation carrying the inclusion delay
// (docs/causes.md's inclusion_delay attribute: the block slot minus
// wantSlot, where 1 is required for the timely-head reward flag).
func (c *Client) CheckInclusion(ctx context.Context, wantSlot domain.Slot, d AttesterDuty, untilSlot domain.Slot, deadline time.Time) (domain.Observation, bool, error) {
	const pollInterval = time.Second
	if untilSlot <= wantSlot {
		return domain.Observation{}, false, fmt.Errorf("inclusion window for slot %d ends at invalid slot %d", wantSlot, untilSlot)
	}
	for {
		head, found, err := c.latestCanonicalHead(ctx)
		if err != nil {
			return domain.Observation{}, false, fmt.Errorf("wait for inclusion window head %d: %w", untilSlot, err)
		}
		if found && head.Slot >= untilSlot {
			break
		}
		if !time.Now().Before(deadline) {
			return domain.Observation{}, false, fmt.Errorf("validated head did not reach inclusion-window slot %d before deadline", untilSlot)
		}
		select {
		case <-ctx.Done():
			return domain.Observation{}, false, ctx.Err()
		case <-time.After(pollInterval):
		}
	}

	var committeeLengths map[domain.CommitteeIndex]uint64
	for s := wantSlot + 1; ; s++ {
		atts, found, err := c.fetchBlockBody(ctx, s)
		if err != nil {
			return domain.Observation{}, false, err
		}
		if !found {
			if s == untilSlot {
				break
			}
			continue
		}
		included, needCommittees, match, err := attestationsIncludeValidator(atts, wantSlot, d, committeeLengths)
		if err != nil {
			return domain.Observation{}, false, fmt.Errorf("check inclusion in slot %d: %w", s, err)
		}
		if needCommittees {
			committeeLengths, err = c.committeeLengthsForSlot(ctx, wantSlot, d.CommitteesAtSlot)
			if err != nil {
				return domain.Observation{}, false, fmt.Errorf("fetch committee lengths for duty slot %d: %w", wantSlot, err)
			}
			included, _, match, err = attestationsIncludeValidator(atts, wantSlot, d, committeeLengths)
			if err != nil {
				return domain.Observation{}, false, fmt.Errorf("check Electra inclusion in slot %d: %w", s, err)
			}
		}
		if !included {
			if s == untilSlot {
				break
			}
			continue
		}
		headCorrect, targetCorrect, err := c.attestationRewardEvidence(ctx, wantSlot, match)
		if err != nil {
			return domain.Observation{}, false, fmt.Errorf("verify reward evidence for duty slot %d: %w", wantSlot, err)
		}
		attrs := map[domain.AttrKey]string{
			domain.AttrValidatorIndex: strconv.FormatUint(uint64(d.ValidatorIndex), 10),
			domain.AttrInclusionDelay: strconv.FormatUint(uint64(s-wantSlot), 10),
			domain.AttrHeadCorrect:    strconv.FormatBool(headCorrect),
			domain.AttrTargetCorrect:  strconv.FormatBool(targetCorrect),
		}
		if match.Data.BeaconBlockRoot != "" {
			attrs[domain.AttrBlockRoot] = match.Data.BeaconBlockRoot
		}
		obs, err := domain.NewObservation(domain.Observation{
			Slot:   wantSlot,
			Kind:   domain.ObsAttestationIncluded,
			At:     time.Now().UTC(),
			Source: domain.SourceBeaconAPI,
			Attrs:  attrs,
		})
		if err != nil {
			return domain.Observation{}, false, fmt.Errorf("build attestation_included observation for slot %d: %w", wantSlot, err)
		}
		return obs, true, nil
	}
	return domain.Observation{}, false, nil
}
