package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/clock"
	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/source"
)

// Observer watches one validator's duty through a beacon node's standard REST
// API and turns what it actually measures into [domain.Observation] values.
//
// # Scope, honestly stated
//
// This polls REST endpoints; it does not subscribe to the SSE event stream
// Phase 2's real internal/source/beaconapi collector will use. That means
// ObsBlockSeen here is "the poll loop first found this slot's block queryable",
// not "the node's gossip layer received it" — close enough to be a genuine,
// non-fabricated measurement for a corpus fixture, but coarser than the client
// metric used by the shipped collector. Head timing polls the standard canonical
// header endpoint and rejects optimistic or already-advanced heads, so it records
// when this observer first verified the exact slot as validated head.
type Observer struct {
	BeaconAPI      string
	Client         *http.Client
	GenesisTime    time.Time
	SecondsPerSlot time.Duration

	// NodeRecoveryBudget is how long blockStatusAtDeadline waits for the
	// watched node to return to a fully synced, execution-valid state
	// before giving up. Zero means defaultNodeRecoveryBudget; tests set it
	// small to assert the give-up path without waiting for it.
	NodeRecoveryBudget time.Duration
}

// defaultNodeRecoveryBudget bounds the wait for a node degraded by the very
// fault the scenario injected.
//
// A fault aimed at a node component can leave the beacon node reporting
// unsynced or optimistic for a while after the fault is reverted. Sampling
// that state exactly once at the deadline turned a
// transient into a hard failure that aborted corpus generation — observed on
// a real devnet, where the run died at this check on slot 3442 while the node
// went on to catch up to slot 3708 entirely unaided.
//
// Waiting is not the same as assuming: a node that never recovers still fails
// the check, because a 404 from a node that is not execution-valid is not
// evidence of a skipped slot (ADR-0015).
const defaultNodeRecoveryBudget = 3 * time.Minute

// NewObserver fetches genesis time and the slot duration from beaconAPI and
// returns an Observer ready to watch duties on that chain.
func NewObserver(ctx context.Context, beaconAPI string) (*Observer, error) {
	// 25s, not something tighter: a cgroup_cpu fault applied directly to the
	// node this tool is polling also starves that node's own REST API of
	// CPU, since it's the same process — a low enough quota to produce a
	// genuinely late duty also makes the API slow to answer, not just the
	// duty slow to complete. Verified in practice generating cl-slow-lighthouse:
	// 5%/7%/9% quota all made requests exceed a 10s timeout outright before
	// any real degradation could be observed; only 10% answered inside 10s,
	// but at 10% the duty itself was fully timely — too little CPU pressure
	// left to attribute anything to. 25s gives enough room for the API to
	// answer under real CPU pressure while still bounding how long a single
	// request can hang if something is genuinely broken.
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		defaultTransport = &http.Transport{Proxy: http.ProxyFromEnvironment}
	}
	transport := defaultTransport.Clone()
	transport.MaxIdleConns = 8
	transport.MaxIdleConnsPerHost = 8
	transport.MaxConnsPerHost = 8
	client := &http.Client{Transport: transport, Timeout: 25 * time.Second}

	var genesis struct {
		Data struct {
			GenesisTime string `json:"genesis_time"`
		} `json:"data"`
	}
	if err := getJSON(ctx, client, beaconAPI+"/eth/v1/beacon/genesis", &genesis); err != nil {
		return nil, fmt.Errorf("fetch genesis: %w", err)
	}
	genesisUnix, err := strconv.ParseInt(genesis.Data.GenesisTime, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse genesis_time %q: %w", genesis.Data.GenesisTime, err)
	}

	var spec struct {
		Data map[string]string `json:"data"`
	}
	if err := getJSON(ctx, client, beaconAPI+"/eth/v1/config/spec", &spec); err != nil {
		return nil, fmt.Errorf("fetch spec: %w", err)
	}
	secondsPerSlot, err := strconv.ParseInt(spec.Data["SECONDS_PER_SLOT"], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse SECONDS_PER_SLOT %q: %w", spec.Data["SECONDS_PER_SLOT"], err)
	}
	if secondsPerSlot <= 0 || secondsPerSlot > 60 {
		return nil, fmt.Errorf("SECONDS_PER_SLOT %d is outside supported range [1,60]", secondsPerSlot)
	}

	return &Observer{
		BeaconAPI:          beaconAPI,
		Client:             client,
		GenesisTime:        time.Unix(genesisUnix, 0).UTC(),
		SecondsPerSlot:     time.Duration(secondsPerSlot) * time.Second,
		NodeRecoveryBudget: defaultNodeRecoveryBudget,
	}, nil
}

// SlotStart returns the wall-clock instant slot begins at.
func (o *Observer) SlotStart(slot uint64) time.Time {
	//nolint:gosec // G115: slot is nowhere near time.Duration's int64 range for any devnet this tool targets
	return o.GenesisTime.Add(time.Duration(slot) * o.SecondsPerSlot)
}

// duty is what FetchDuty learns about a validator's attester assignment —
// exactly the fields needed to later locate that validator's bit in an
// attestation's aggregation bitlist.
type duty struct {
	CommitteeIndex          uint64
	CommitteeLength         uint64
	CommitteesAtSlot        uint64
	ValidatorCommitteeIndex uint64
}

// dutyAssignment is one validator's attester assignment within an epoch:
// which slot it was assigned, and its committee position in that slot.
type dutyAssignment struct {
	Slot uint64
	Duty duty
}

// FetchDuties resolves the attester committee assignment of every validator
// in validatorIndices for epoch, in a single request — the standard
// POST /eth/v1/validator/duties/attester/{epoch} endpoint takes an array of
// indices, so asking about a whole node's validator set costs exactly as
// much as asking about one. Attester duty is assigned once per epoch to
// exactly one slot per validator; the caller does not choose the slot, this
// call reports which one each validator got.
//
// Validators with no duty in this epoch are simply absent from the result
// rather than an error — a caller supplying a range wants whichever of them
// the shuffle actually assigned, not a guarantee that all of them were.
func (o *Observer) FetchDuties(ctx context.Context, epoch uint64, validatorIndices []uint64) (map[uint64]dutyAssignment, time.Time, error) {
	indices := make([]string, 0, len(validatorIndices))
	for _, vi := range validatorIndices {
		indices = append(indices, strconv.FormatUint(vi, 10))
	}
	body, err := json.Marshal(indices)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("marshal duty request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/eth/v1/validator/duties/attester/%d", o.BeaconAPI, epoch),
		strings.NewReader(string(body)))
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("build duty request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	at := time.Now().UTC()
	var resp struct {
		Data []struct {
			ValidatorIndex          string `json:"validator_index"`
			Slot                    string `json:"slot"`
			CommitteeIndex          string `json:"committee_index"`
			CommitteeLength         string `json:"committee_length"`
			CommitteesAtSlot        string `json:"committees_at_slot"`
			ValidatorCommitteeIndex string `json:"validator_committee_index"`
		} `json:"data"`
	}
	if err := doJSON(o.Client, req, &resp); err != nil {
		return nil, time.Time{}, fmt.Errorf("fetch attester duties: %w", err)
	}

	out := make(map[uint64]dutyAssignment, len(resp.Data))
	for _, entry := range resp.Data {
		vi, err0 := strconv.ParseUint(entry.ValidatorIndex, 10, 64)
		slot, err1 := strconv.ParseUint(entry.Slot, 10, 64)
		ci, err2 := strconv.ParseUint(entry.CommitteeIndex, 10, 64)
		cl, err3 := strconv.ParseUint(entry.CommitteeLength, 10, 64)
		cs, err4 := strconv.ParseUint(entry.CommitteesAtSlot, 10, 64)
		vci, err5 := strconv.ParseUint(entry.ValidatorCommitteeIndex, 10, 64)
		if err := errors.Join(err0, err1, err2, err3, err4, err5); err != nil {
			return nil, time.Time{}, fmt.Errorf("parse duty fields in epoch %d: %w", epoch, err)
		}
		if cl == 0 || cs == 0 || ci >= cs || vci >= cl {
			return nil, time.Time{}, fmt.Errorf("invalid duty assignment in epoch %d: committee_index=%d committee_length=%d committees_at_slot=%d validator_position=%d", epoch, ci, cl, cs, vci)
		}
		out[vi] = dutyAssignment{
			Slot: slot,
			Duty: duty{CommitteeIndex: ci, CommitteeLength: cl, CommitteesAtSlot: cs, ValidatorCommitteeIndex: vci},
		}
	}
	return out, at, nil
}

// FetchProposers resolves every slot's proposer for epoch in a single
// request, via the standard GET /eth/v1/validator/duties/proposer/{epoch}
// endpoint, keyed by slot. The endpoint always returns the whole epoch, so
// asking about one slot and asking about all of them is the same call —
// callers checking several candidate slots should fetch this map once
// rather than per slot.
func (o *Observer) FetchProposers(ctx context.Context, epoch uint64) (map[uint64]uint64, error) {
	var resp struct {
		Data []struct {
			ValidatorIndex string `json:"validator_index"`
			Slot           string `json:"slot"`
		} `json:"data"`
	}
	url := fmt.Sprintf("%s/eth/v1/validator/duties/proposer/%d", o.BeaconAPI, epoch)
	if err := getJSON(ctx, o.Client, url, &resp); err != nil {
		return nil, fmt.Errorf("fetch proposer duties for epoch %d: %w", epoch, err)
	}

	out := make(map[uint64]uint64, len(resp.Data))
	for _, d := range resp.Data {
		slot, err0 := strconv.ParseUint(d.Slot, 10, 64)
		vi, err1 := strconv.ParseUint(d.ValidatorIndex, 10, 64)
		if err := errors.Join(err0, err1); err != nil {
			return nil, fmt.Errorf("parse proposer duty in epoch %d: %w", epoch, err)
		}
		out[slot] = vi
	}
	return out, nil
}

type blockAttestation = apiAttestation

type blockStatus struct {
	Root          string
	ProposerIndex uint64
	At            time.Time
	Found         bool
	Skipped       bool
}

// PollBlockSeen polls for slot's block until it appears or deadline passes,
// returning the instant the poll loop first observed it. At the deadline it
// reports Skipped only after a fully synced, execution-online node has advanced
// past the slot and a repeated canonical-header lookup still returns 404.
func (o *Observer) PollBlockSeen(ctx context.Context, slot uint64, deadline time.Time) (blockStatus, error) {
	const pollInterval = 500 * time.Millisecond

	for {
		header, ok, ferr := o.fetchBlockHeader(ctx, slot)
		if ferr != nil {
			return blockStatus{}, ferr
		}
		if ok {
			return observedBlockStatus(slot, header)
		}
		if !time.Now().Before(deadline) {
			return o.blockStatusAtDeadline(ctx, slot)
		}
		select {
		case <-ctx.Done():
			return blockStatus{}, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func observedBlockStatus(slot uint64, header beaconHeader) (blockStatus, error) {
	pi, err := strconv.ParseUint(header.ProposerIndex, 10, 64)
	if err != nil {
		return blockStatus{}, fmt.Errorf("parse proposer_index %q: %w", header.ProposerIndex, err)
	}
	if err := validateBeaconRoot(header.Root); err != nil {
		return blockStatus{}, fmt.Errorf("block %d: %w", slot, err)
	}
	return blockStatus{Root: header.Root, ProposerIndex: pi, At: time.Now().UTC(), Found: true}, nil
}

func (o *Observer) blockStatusAtDeadline(ctx context.Context, slot uint64) (blockStatus, error) {
	const recoveryPoll = 2 * time.Second

	budget := o.NodeRecoveryBudget
	if budget <= 0 {
		budget = defaultNodeRecoveryBudget
	}
	giveUpAt := time.Now().Add(budget)

	for {
		status, err := o.fetchNodeSyncStatus(ctx)
		if err != nil {
			return blockStatus{}, fmt.Errorf("confirm node sync before checking skipped slot %d: %w", slot, err)
		}
		if status.ready() && status.HeadSlot > slot {
			break
		}
		if !time.Now().Before(giveUpAt) {
			return blockStatus{}, fmt.Errorf("cannot confirm slot %d as seen or skipped: node is not fully synced, execution-valid, and past the slot after waiting %s for it to recover", slot, budget)
		}
		select {
		case <-ctx.Done():
			return blockStatus{}, ctx.Err()
		case <-time.After(recoveryPoll):
		}
	}

	header, found, err := o.fetchBlockHeader(ctx, slot)
	if err != nil {
		return blockStatus{}, err
	}
	if found {
		return observedBlockStatus(slot, header)
	}
	return blockStatus{At: time.Now().UTC(), Skipped: true}, nil
}

type nodeSyncStatus struct {
	HeadSlot     uint64
	SyncDistance uint64
	IsSyncing    bool
	IsOptimistic bool
	ELOffline    bool
}

func (s nodeSyncStatus) ready() bool {
	return !s.IsSyncing && !s.IsOptimistic && !s.ELOffline && s.SyncDistance == 0
}

func (o *Observer) fetchNodeSyncStatus(ctx context.Context) (nodeSyncStatus, error) {
	var response struct {
		Data struct {
			HeadSlot     string `json:"head_slot"`
			SyncDistance string `json:"sync_distance"`
			IsSyncing    bool   `json:"is_syncing"`
			IsOptimistic bool   `json:"is_optimistic"`
			ELOffline    bool   `json:"el_offline"`
		} `json:"data"`
	}
	if err := getJSON(ctx, o.Client, o.BeaconAPI+"/eth/v1/node/syncing", &response); err != nil {
		return nodeSyncStatus{}, err
	}
	headSlot, err := strconv.ParseUint(response.Data.HeadSlot, 10, 64)
	if err != nil {
		return nodeSyncStatus{}, fmt.Errorf("parse head_slot %q: %w", response.Data.HeadSlot, err)
	}
	distance, err := strconv.ParseUint(response.Data.SyncDistance, 10, 64)
	if err != nil {
		return nodeSyncStatus{}, fmt.Errorf("parse sync_distance %q: %w", response.Data.SyncDistance, err)
	}
	return nodeSyncStatus{
		HeadSlot: headSlot, SyncDistance: distance, IsSyncing: response.Data.IsSyncing,
		IsOptimistic: response.Data.IsOptimistic, ELOffline: response.Data.ELOffline,
	}, nil
}

type beaconHeader struct {
	Root          string
	ProposerIndex string
}

func (o *Observer) fetchBlockHeader(ctx context.Context, slot uint64) (beaconHeader, bool, error) {
	endpoint := fmt.Sprintf("%s/eth/v1/beacon/headers/%d", o.BeaconAPI, slot)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return beaconHeader{}, false, fmt.Errorf("build block header request: %w", err)
	}
	resp, err := o.Client.Do(req)
	if err != nil {
		return beaconHeader{}, false, fmt.Errorf("fetch block header %d: %w", slot, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response body
	if resp.StatusCode == http.StatusNotFound {
		return beaconHeader{}, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return beaconHeader{}, false, fmt.Errorf("fetch block header %d: unexpected status %d", slot, resp.StatusCode)
	}
	var out struct {
		ExecutionOptimistic *bool `json:"execution_optimistic"`
		Data                struct {
			Root      string `json:"root"`
			Canonical bool   `json:"canonical"`
			Header    struct {
				Message struct {
					ProposerIndex string `json:"proposer_index"`
				} `json:"message"`
			} `json:"header"`
		} `json:"data"`
	}
	if err := decodeJSONBody(resp, &out); err != nil {
		return beaconHeader{}, false, fmt.Errorf("decode block header %d: %w", slot, err)
	}
	if out.ExecutionOptimistic == nil {
		return beaconHeader{}, false, fmt.Errorf("block header %d has no execution_optimistic status", slot)
	}
	if *out.ExecutionOptimistic || !out.Data.Canonical {
		return beaconHeader{}, false, nil
	}
	return beaconHeader{Root: out.Data.Root, ProposerIndex: out.Data.Header.Message.ProposerIndex}, true, nil
}

type poolAttestation = apiAttestation

// PollAttestationPublished polls the beacon node's attestation pool
// (GET /eth/v1/beacon/pool/attestations) for dutySlot until d's validator's bit
// appears set in a matching attestation, or deadline passes.
//
// This is a proxy for "the validator client published its attestation", not a
// direct measurement of it: the pool reflects what this beacon node has
// received and aggregated, which lags true publish time by however long gossip
// propagation and aggregation took. On this project's two-node devnet that lag
// is small, but it is not zero, and a caller comparing this timestamp against
// the attestation deadline should keep that slack in mind — this is the same
// kind of honestly-stated coarseness ObsBlockSeen's doc comment describes for
// [Observer].
func (o *Observer) PollAttestationPublished(ctx context.Context, dutySlot uint64, d duty, deadline time.Time) (publishedAt time.Time, blockRoot string, found bool, err error) {
	const pollInterval = 500 * time.Millisecond

	for {
		ok, match, ferr := o.poolIncludesAttestation(ctx, dutySlot, d)
		if ferr != nil {
			// Same tolerance as beaconapi.Client.AttestationPublished: a
			// single transient fetch error (a node under this scenario's own
			// CPU/network fault answering one poll too slowly) must not
			// abort the whole watch — a real, late publish is exactly what
			// this fault is meant to produce, and losing it here mislabels
			// the record as if the validator never attested at all.
			if ctx.Err() != nil {
				return time.Time{}, "", false, ferr
			}
			ok = false
		}
		if ok {
			return time.Now().UTC(), match.Data.BeaconBlockRoot, true, nil
		}
		if !time.Now().Before(deadline) {
			return time.Time{}, "", false, nil
		}
		select {
		case <-ctx.Done():
			return time.Time{}, "", false, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func (o *Observer) poolIncludesAttestation(ctx context.Context, dutySlot uint64, d duty) (bool, apiAttestation, error) {
	url := fmt.Sprintf("%s/eth/v1/beacon/pool/attestations?slot=%d", o.BeaconAPI, dutySlot)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, apiAttestation{}, fmt.Errorf("build attestation pool request for slot %d: %w", dutySlot, err)
	}
	httpResp, err := o.Client.Do(req)
	if err != nil {
		return false, apiAttestation{}, fmt.Errorf("fetch attestation pool for slot %d: %w", dutySlot, err)
	}
	defer httpResp.Body.Close() //nolint:errcheck // read-only response body; nothing to act on if Close fails

	// The pool only holds recent, not-yet-included attestations — once a slot
	// ages out (its data pruned, or its epoch has moved on), the beacon node
	// reports 410 Gone rather than an empty result. That is not a failure to
	// report: it means the window for seeing this validator publish here has
	// closed, the same as a 404 on a block that was never produced.
	if httpResp.StatusCode == http.StatusGone || httpResp.StatusCode == http.StatusNotFound {
		return false, apiAttestation{}, nil
	}
	if httpResp.StatusCode != http.StatusOK {
		return false, apiAttestation{}, fmt.Errorf("fetch attestation pool for slot %d: unexpected status %d", dutySlot, httpResp.StatusCode)
	}

	var resp struct {
		Data []poolAttestation `json:"data"`
	}
	if err := decodeJSONBody(httpResp, &resp); err != nil {
		return false, apiAttestation{}, fmt.Errorf("decode attestation pool for slot %d: %w", dutySlot, err)
	}

	included, needCommittees, match, err := attestationsIncludeValidator(resp.Data, dutySlot, d, nil)
	if err != nil || included || !needCommittees {
		return included, match, err
	}
	// state_id must resolve to dutySlot's own epoch, not "head" — see the
	// slot-anchored state_id used below in fetchCommitteeLengths's other
	// call site (attestationRewardEvidence's inclusion check) for the same
	// reason: "head" drifts into a later epoch as real time passes, and the
	// beacon API rejects a ?slot= query whose epoch differs from the state's
	// own. dutySlot's own state always has dutySlot's own epoch.
	lengths, err := o.fetchCommitteeLengths(ctx, strconv.FormatUint(dutySlot, 10), dutySlot, d.CommitteesAtSlot)
	if err != nil {
		return false, apiAttestation{}, fmt.Errorf("fetch committee lengths for duty slot %d: %w", dutySlot, err)
	}
	included, _, match, err = attestationsIncludeValidator(resp.Data, dutySlot, d, lengths)
	return included, match, err
}

// CheckInclusion looks for d's validator's attestation bit set in any block from
// dutySlot+1 up to and including untilSlot, returning the slot and instant it was
// found included.
func (o *Observer) CheckInclusion(ctx context.Context, dutySlot uint64, d duty, untilSlot uint64, deadline time.Time) (includedInSlot uint64, includedAt time.Time, blockRoot string, headCorrect, targetCorrect, found bool, err error) {
	if untilSlot <= dutySlot {
		return 0, time.Time{}, "", false, false, false, fmt.Errorf("inclusion window for slot %d ends at invalid slot %d", dutySlot, untilSlot)
	}
	for {
		status, statusErr := o.fetchNodeSyncStatus(ctx)
		if statusErr != nil {
			return 0, time.Time{}, "", false, false, false, fmt.Errorf("wait for inclusion window head %d: %w", untilSlot, statusErr)
		}
		if status.ready() && status.HeadSlot >= untilSlot {
			break
		}
		if !time.Now().Before(deadline) {
			return 0, time.Time{}, "", false, false, false, fmt.Errorf("validated head did not reach inclusion-window slot %d before deadline", untilSlot)
		}
		select {
		case <-ctx.Done():
			return 0, time.Time{}, "", false, false, false, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	for s := dutySlot + 1; s <= untilSlot; s++ {
		ok, match, ferr := o.blockIncludesAttestation(ctx, s, dutySlot, d)
		if ferr != nil {
			return 0, time.Time{}, "", false, false, false, ferr
		}
		if !ok {
			continue
		}
		headCorrect, targetCorrect, err := o.attestationRewardEvidence(ctx, dutySlot, match)
		if err != nil {
			return 0, time.Time{}, "", false, false, false, err
		}
		return s, time.Now().UTC(), match.Data.BeaconBlockRoot, headCorrect, targetCorrect, true, nil
	}
	return 0, time.Time{}, "", false, false, false, nil
}

func (o *Observer) blockIncludesAttestation(ctx context.Context, blockSlot, wantSlot uint64, d duty) (bool, apiAttestation, error) {
	block, ok, err := o.fetchBlock(ctx, blockSlot)
	if err != nil || !ok {
		return false, apiAttestation{}, err
	}
	included, needCommittees, match, err := attestationsIncludeValidator(block.Message.Body.Attestations, wantSlot, d, nil)
	if err != nil || included || !needCommittees {
		return included, match, err
	}
	lengths, err := o.fetchCommitteeLengths(ctx, strconv.FormatUint(blockSlot, 10), wantSlot, d.CommitteesAtSlot)
	if err != nil {
		return false, apiAttestation{}, fmt.Errorf("fetch committee lengths for duty slot %d: %w", wantSlot, err)
	}
	included, _, match, err = attestationsIncludeValidator(block.Message.Body.Attestations, wantSlot, d, lengths)
	return included, match, err
}

type beaconBlock struct {
	Message struct {
		ProposerIndex string `json:"proposer_index"`
		Body          struct {
			Attestations []blockAttestation `json:"attestations"`
			// Post-ePBS only: the payload-timeliness committee's votes on the
			// *previous* slot's payload. Absent on every pre-Gloas block, which
			// decodes to an empty slice rather than an error.
			PayloadAttestations []payloadAttestation `json:"payload_attestations"`
		} `json:"body"`
	} `json:"message"`
}

// payloadAttestation is one PTC vote as the Beacon API serves it.
type payloadAttestation struct {
	Data struct {
		Slot           string `json:"slot"`
		PayloadPresent bool   `json:"payload_present"`
	} `json:"data"`
}

// PayloadAttested reports the payload-timeliness committee's verdict for
// dutySlot, read from the following block.
//
// Mirrors internal/source/beaconapi.Client.PayloadAttested deliberately: the
// generator and the daemon must record the same fact the same way, or a corpus
// record would not represent what an operator's node would have seen. found is
// false on a pre-ePBS chain, a skipped following slot, or a block carrying no
// vote for this slot — none of which is an error.
func (o *Observer) PayloadAttested(ctx context.Context, dutySlot uint64) (present bool, votes int, found bool, err error) {
	block, ok, err := o.fetchBlock(ctx, dutySlot+1)
	if err != nil {
		return false, 0, false, fmt.Errorf("fetch block %d for payload attestations: %w", dutySlot+1, err)
	}
	if !ok {
		return false, 0, false, nil
	}
	counted, seen := 0, 0
	for _, vote := range block.Message.Body.PayloadAttestations {
		voteSlot, parseErr := strconv.ParseUint(vote.Data.Slot, 10, 64)
		if parseErr != nil || voteSlot != dutySlot {
			continue
		}
		counted++
		if vote.Data.PayloadPresent {
			seen++
		}
	}
	if counted == 0 {
		return false, 0, false, nil
	}
	return seen*2 > counted, counted, true, nil
}

// fetchBlock fetches slot's block, treating "not found" (slot empty or not yet
// produced) as a normal, non-error outcome. Absence here is not skipped-slot
// evidence; PollBlockSeen performs the stronger canonical check separately.
func (o *Observer) fetchBlock(ctx context.Context, slot uint64) (beaconBlock, bool, error) {
	url := fmt.Sprintf("%s/eth/v2/beacon/blocks/%d", o.BeaconAPI, slot)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return beaconBlock{}, false, fmt.Errorf("build block request: %w", err)
	}

	resp, err := o.Client.Do(req)
	if err != nil {
		return beaconBlock{}, false, fmt.Errorf("fetch block %d: %w", slot, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response body; nothing to act on if Close fails

	if resp.StatusCode == http.StatusNotFound {
		return beaconBlock{}, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return beaconBlock{}, false, fmt.Errorf("fetch block %d: unexpected status %d", slot, resp.StatusCode)
	}

	var out struct {
		ExecutionOptimistic *bool       `json:"execution_optimistic"`
		Data                beaconBlock `json:"data"`
	}
	if err := decodeJSONBody(resp, &out); err != nil {
		return beaconBlock{}, false, fmt.Errorf("decode block %d: %w", slot, err)
	}
	if out.ExecutionOptimistic == nil {
		return beaconBlock{}, false, fmt.Errorf("block %d has no execution_optimistic status", slot)
	}
	if *out.ExecutionOptimistic {
		return beaconBlock{}, false, nil
	}
	return out.Data, true, nil
}

// bitSet decodes an SSZ Bitlist (hex-encoded, as the Beacon API returns it) and
// reports whether the bit at index is set. An SSZ Bitlist's highest set bit is a
// length sentinel, not data — everything below it is the payload.
func bitSet(hexBits string, index uint64) (bool, error) {
	raw := strings.TrimPrefix(hexBits, "0x")
	if len(raw)%2 != 0 {
		raw = "0" + raw
	}
	buf := make([]byte, 0, len(raw)/2)
	for i := 0; i < len(raw); i += 2 {
		var b uint64
		if _, err := fmt.Sscanf(raw[i:i+2], "%02x", &b); err != nil {
			return false, fmt.Errorf("decode hex byte %q: %w", raw[i:i+2], err)
		}
		buf = append(buf, byte(b))
	}

	length := bitlistLength(buf)
	if index >= length {
		return false, fmt.Errorf("index %d out of range for bitlist of length %d", index, length)
	}
	byteIdx, bitIdx := index/8, index%8
	return buf[byteIdx]&(1<<bitIdx) != 0, nil
}

// bitlistLength returns the number of data bits in an SSZ Bitlist encoding: the
// position of the highest set bit across the whole byte slice, most-significant
// byte last (little-endian byte order, as SSZ specifies).
func bitlistLength(buf []byte) uint64 {
	for i := len(buf) - 1; i >= 0; i-- {
		if buf[i] == 0 {
			continue
		}
		topBit := bits.Len8(buf[i]) - 1
		//nolint:gosec // G115: i is a bounded slice index and topBit is in [0,7]; neither can be negative here
		return uint64(i)*8 + uint64(topBit)
	}
	return 0
}

func getJSON(ctx context.Context, client *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request for %s: %w", url, err)
	}
	return doJSON(client, req, out)
}

func doJSON(client *http.Client, req *http.Request, out any) error {
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", req.URL, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response body; nothing to act on if Close fails
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("request %s: unexpected status %d", req.URL, resp.StatusCode)
	}
	if err := decodeJSONBody(resp, out); err != nil {
		return fmt.Errorf("decode response from %s: %w", req.URL, err)
	}
	return nil
}

const maxDevnetResponseBodyBytes = 64 << 20

func decodeJSONBody(resp *http.Response, out any) error {
	if resp.ContentLength > maxDevnetResponseBodyBytes {
		return fmt.Errorf("response body is %d bytes, limit is %d", resp.ContentLength, maxDevnetResponseBodyBytes)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDevnetResponseBodyBytes+1))
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}
	if len(body) > maxDevnetResponseBodyBytes {
		return fmt.Errorf("response body exceeds %d bytes", maxDevnetResponseBodyBytes)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	return nil
}

// buildObservations turns what RunScenario actually measured into
// domain.Observation values, sorted by timestamp as domain.Timeline requires.
// Every value here traces to something recorded during this run — see
// RunScenario's doc comment.
// dutyOutcome carries everything RunScenario measured about how one duty slot
// resolved — the input buildObservations turns into domain.Observation values.
// One field per fact, grown as new evidence kinds were added, so extending what a scenario can
// record means adding a field here rather than another positional parameter
// to buildObservations.
type dutyOutcome struct {
	BlockFound          bool
	BlockSkipped        bool
	BlockRoot           string
	ProposerIndex       uint64
	ProposerKnown       bool
	BlockSeenAt         time.Time
	BlockTimingMeasured bool

	HeadFound     bool
	HeadRoot      string
	HeadUpdatedAt time.Time
	EngineCalls   []source.EngineCallWindow

	Published     bool
	PublishedAt   time.Time
	PublishedRoot string

	PayloadPresent    bool
	PayloadPTCVotes   int
	PayloadAttested   bool
	PayloadAttestedAt time.Time

	Included       bool
	IncludedInSlot uint64
	IncludedAt     time.Time
	IncludedRoot   string
	HeadCorrect    bool
	TargetCorrect  bool

	CollectionCompletedAt time.Time

	// HostPressure is the sampled "some avg10" PSI percentage, for whichever
	// file HostPressureMetric names ("host_iowait_pct" -> io.pressure,
	// "host_mem_pressure_pct" -> memory.pressure). Present only when
	// Scenario.SamplePressure was set.
	HostPressure       *float64
	HostPressureMetric string
	HostSampledAt      time.Time

	// PeerCount is what SamplePeerCount returned. Present only when
	// Scenario.PeerCountTarget was set.
	PeerCount          *float64
	PeerCountSampledAt time.Time
	// PeerCountSource is the sample's own provenance, carried through rather than
	// assumed. SamplePeerCount reads /eth/v1/node/peer_count (ADR-0023), so
	// stamping promscrape here — as this recorder used to — put a provenance on
	// the observation that its own collection path contradicts, and R-200 prints
	// that source verbatim as the evidence line's attribution.
	PeerCountSource domain.SourceID

	Network          *domain.NetworkBaseline
	NetworkSampledAt time.Time
}

func buildObservations(s Scenario, slot uint64, slotStart, dutyAt time.Time, o dutyOutcome, readings []clock.Reading) ([]domain.Observation, error) {
	if len(readings) == 0 {
		return nil, fmt.Errorf("build observations: at least one clock reading is required")
	}
	drafts := []domain.Observation{
		{
			Slot: domain.Slot(slot), Kind: domain.ObsSlotStart,
			At: slotStart, Source: domain.SourceDerived,
		},
		{
			Slot: domain.Slot(slot), Kind: domain.ObsDutyAssigned,
			At: dutyAt, Source: domain.SourceBeaconAPI,
			Attrs: map[domain.AttrKey]string{
				domain.AttrValidatorIndex: strconv.FormatUint(s.ValidatorIndex, 10),
			},
		},
	}

	if o.BlockFound {
		attrs := map[domain.AttrKey]string{}
		if o.ProposerKnown {
			attrs[domain.AttrProposerIndex] = strconv.FormatUint(o.ProposerIndex, 10)
		}
		if o.BlockRoot != "" {
			attrs[domain.AttrBlockRoot] = o.BlockRoot
		}
		source := domain.SourceBeaconAPI
		if o.BlockTimingMeasured {
			source = domain.SourcePromScrape
		}
		drafts = append(drafts, domain.Observation{
			Slot: domain.Slot(slot), Kind: domain.ObsBlockSeen,
			At: o.BlockSeenAt, Source: source, Attrs: attrs,
		})
	}
	if o.BlockSkipped {
		drafts = append(drafts, domain.Observation{
			Slot: domain.Slot(slot), Kind: domain.ObsBlockSkipped,
			At: o.BlockSeenAt, Source: domain.SourceBeaconAPI,
		})
	}

	if o.HeadFound {
		attrs := map[domain.AttrKey]string{}
		if o.HeadRoot != "" {
			attrs[domain.AttrBlockRoot] = o.HeadRoot
		}
		drafts = append(drafts, domain.Observation{
			Slot: domain.Slot(slot), Kind: domain.ObsHeadUpdated,
			At: o.HeadUpdatedAt, Source: domain.SourceBeaconAPI, Attrs: attrs,
		})
	}

	if o.Published {
		attrs := map[domain.AttrKey]string{
			domain.AttrValidatorIndex: strconv.FormatUint(s.ValidatorIndex, 10),
		}
		if o.PublishedRoot != "" {
			attrs[domain.AttrBlockRoot] = o.PublishedRoot
		}
		drafts = append(drafts, domain.Observation{
			Slot: domain.Slot(slot), Kind: domain.ObsAttestationPublished,
			At: o.PublishedAt, Source: domain.SourceBeaconAPI,
			Attrs: attrs,
		})
	}

	if o.Included {
		attrs := map[domain.AttrKey]string{
			domain.AttrValidatorIndex: strconv.FormatUint(s.ValidatorIndex, 10),
			domain.AttrInclusionDelay: strconv.FormatUint(o.IncludedInSlot-slot, 10),
			domain.AttrHeadCorrect:    strconv.FormatBool(o.HeadCorrect),
			domain.AttrTargetCorrect:  strconv.FormatBool(o.TargetCorrect),
		}
		if o.IncludedRoot != "" {
			attrs[domain.AttrBlockRoot] = o.IncludedRoot
		}
		drafts = append(drafts, domain.Observation{
			Slot: domain.Slot(slot), Kind: domain.ObsAttestationIncluded,
			At: o.IncludedAt, Source: domain.SourceBeaconAPI,
			Attrs: attrs,
		})
	}
	if o.PayloadAttested {
		drafts = append(drafts, domain.Observation{
			Slot: domain.Slot(slot), Kind: domain.ObsPayloadAttested,
			At: o.PayloadAttestedAt, Source: domain.SourceBeaconAPI,
			Attrs: map[domain.AttrKey]string{
				domain.AttrPayloadPresent: strconv.FormatBool(o.PayloadPresent),
				domain.AttrPTCVotes:       strconv.Itoa(o.PayloadPTCVotes),
			},
		})
	}

	if !o.CollectionCompletedAt.IsZero() {
		drafts = append(drafts, domain.Observation{
			Slot: domain.Slot(slot), Kind: domain.ObsCollectionCompleted,
			At: o.CollectionCompletedAt, Source: domain.SourceDerived,
			Attrs: map[domain.AttrKey]string{
				domain.AttrValidatorIndex: strconv.FormatUint(s.ValidatorIndex, 10),
			},
		})
	}

	if o.HostPressure != nil {
		drafts = append(drafts, domain.Observation{
			Slot: domain.Slot(slot), Kind: domain.ObsHostSampled,
			At: o.HostSampledAt, Source: domain.SourceHostMetrics,
			Attrs: map[domain.AttrKey]string{
				domain.AttrMetric: o.HostPressureMetric,
				domain.AttrValue:  strconv.FormatFloat(*o.HostPressure, 'f', -1, 64),
			},
		})
	}

	if o.PeerCount != nil {
		drafts = append(drafts, domain.Observation{
			Slot: domain.Slot(slot), Kind: domain.ObsPeerCountSampled,
			At: o.PeerCountSampledAt, Source: o.PeerCountSource,
			Attrs: map[domain.AttrKey]string{
				domain.AttrPeerCount: strconv.FormatFloat(*o.PeerCount, 'f', -1, 64),
			},
		})
	}
	if o.Network != nil {
		drafts = append(drafts, domain.Observation{
			Slot: domain.Slot(slot), Kind: domain.ObsNetworkBaselineSampled,
			At: o.NetworkSampledAt, Source: o.Network.Source,
			Attrs: map[domain.AttrKey]string{
				domain.AttrBlockArrivalP50MS: strconv.FormatFloat(o.Network.BlockArrivalP50.Seconds()*1000, 'f', -1, 64),
				domain.AttrBlockArrivalP90MS: strconv.FormatFloat(o.Network.BlockArrivalP90.Seconds()*1000, 'f', -1, 64),
				domain.AttrSampleCount:       strconv.Itoa(o.Network.SampleCount),
			},
		})
	}

	for _, call := range o.EngineCalls {
		drafts = append(drafts, domain.Observation{
			Slot: domain.Slot(slot), Kind: domain.ObsEngineCall,
			At: call.At, Source: domain.SourcePromScrape,
			Attrs: map[domain.AttrKey]string{
				domain.AttrEngineMethod: call.Method,
				domain.AttrDurationMS:   strconv.FormatFloat(call.DurationMS, 'f', -1, 64),
				domain.AttrSampleCount:  strconv.FormatUint(call.Count, 10),
			},
		})
	}

	for i := range drafts {
		// Use the real reading nearest this observation. One sample cannot stay
		// fresh across the full EIP-7045 inclusion window, so early slot facts
		// and late inclusion facts intentionally carry different provenance.
		reading := nearestClockReading(drafts[i].At, readings)
		drafts[i].ClockOffset = reading.Offset
		drafts[i].ClockMeasured = true
		drafts[i].ClockSampleAt = reading.At.Add(reading.Offset).UTC()
		if drafts[i].Kind != domain.ObsSlotStart && drafts[i].Kind != domain.ObsNetworkBaselineSampled && (drafts[i].Kind != domain.ObsBlockSeen || drafts[i].Source != domain.SourcePromScrape) {
			drafts[i].At = drafts[i].At.Add(reading.Offset).UTC()
		}
	}
	sort.SliceStable(drafts, func(i, j int) bool { return drafts[i].At.Before(drafts[j].At) })

	out := make([]domain.Observation, 0, len(drafts))
	for i, d := range drafts {
		obs, err := domain.NewObservation(d)
		if err != nil {
			// A malformed observation here is a bug in this file, not a fact
			// about the devnet — fail loudly rather than write a corpus
			// scenario with a silently-dropped fact.
			return nil, fmt.Errorf("built an invalid observation %d (kind %s): %w", i, d.Kind, err)
		}
		out = append(out, obs)
	}
	return out, nil
}

func nearestClockReading(at time.Time, readings []clock.Reading) clock.Reading {
	best := readings[0]
	bestDistance := absoluteDuration(at.Sub(best.At))
	for _, candidate := range readings[1:] {
		distance := absoluteDuration(at.Sub(candidate.At))
		if distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	return best
}
