package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/bits"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/CHANGEME/whymiss/internal/domain"
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
// non-fabricated measurement for a corpus fixture, but coarser than what the
// shipped product will record. ObsHeadUpdated is not emitted at all: telling
// "seen" apart from "validated as head" needs the event stream this tool does
// not have. A scenario built from this Observer is honest about what it
// contains; it is not a preview of Phase 2's collector fidelity.
type Observer struct {
	BeaconAPI      string
	Client         *http.Client
	GenesisTime    time.Time
	SecondsPerSlot time.Duration
}

// NewObserver fetches genesis time and the slot duration from beaconAPI and
// returns an Observer ready to watch duties on that chain.
func NewObserver(ctx context.Context, beaconAPI string) (*Observer, error) {
	client := &http.Client{Timeout: 10 * time.Second}

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

	return &Observer{
		BeaconAPI:      beaconAPI,
		Client:         client,
		GenesisTime:    time.Unix(genesisUnix, 0).UTC(),
		SecondsPerSlot: time.Duration(secondsPerSlot) * time.Second,
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
	ValidatorCommitteeIndex uint64
}

// FetchDuty resolves validatorIndex's attester committee assignment for epoch,
// via the standard POST /eth/v1/validator/duties/attester/{epoch} endpoint.
// Attester duty is assigned once per epoch to exactly one slot within it — the
// caller does not choose the slot, this call reports which one it is.
func (o *Observer) FetchDuty(ctx context.Context, epoch, validatorIndex uint64) (assignedSlot uint64, d duty, at time.Time, err error) {
	body, err := json.Marshal([]string{strconv.FormatUint(validatorIndex, 10)})
	if err != nil {
		return 0, duty{}, time.Time{}, fmt.Errorf("marshal duty request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/eth/v1/validator/duties/attester/%d", o.BeaconAPI, epoch),
		strings.NewReader(string(body)))
	if err != nil {
		return 0, duty{}, time.Time{}, fmt.Errorf("build duty request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	at = time.Now().UTC()
	var resp struct {
		Data []struct {
			ValidatorIndex          string `json:"validator_index"`
			Slot                    string `json:"slot"`
			CommitteeIndex          string `json:"committee_index"`
			CommitteeLength         string `json:"committee_length"`
			ValidatorCommitteeIndex string `json:"validator_committee_index"`
		} `json:"data"`
	}
	if err := doJSON(o.Client, req, &resp); err != nil {
		return 0, duty{}, time.Time{}, fmt.Errorf("fetch attester duty: %w", err)
	}

	target := strconv.FormatUint(validatorIndex, 10)
	for _, entry := range resp.Data {
		if entry.ValidatorIndex != target {
			continue
		}
		slot, err0 := strconv.ParseUint(entry.Slot, 10, 64)
		ci, err1 := strconv.ParseUint(entry.CommitteeIndex, 10, 64)
		cl, err2 := strconv.ParseUint(entry.CommitteeLength, 10, 64)
		vci, err3 := strconv.ParseUint(entry.ValidatorCommitteeIndex, 10, 64)
		if err := errors.Join(err0, err1, err2, err3); err != nil {
			return 0, duty{}, time.Time{}, fmt.Errorf("parse duty fields for validator %d epoch %d: %w",
				validatorIndex, epoch, err)
		}
		return slot, duty{CommitteeIndex: ci, CommitteeLength: cl, ValidatorCommitteeIndex: vci}, at, nil
	}
	return 0, duty{}, time.Time{}, fmt.Errorf("no attester duty found for validator %d in epoch %d", validatorIndex, epoch)
}

// FetchProposer resolves the proposer duty for slot via the standard
// GET /eth/v1/validator/duties/proposer/{epoch} endpoint.
func (o *Observer) FetchProposer(ctx context.Context, slot uint64) (validatorIndex uint64, err error) {
	epoch := slot / domain.SlotsPerEpoch

	var resp struct {
		Data []struct {
			ValidatorIndex string `json:"validator_index"`
			Slot           string `json:"slot"`
		} `json:"data"`
	}
	url := fmt.Sprintf("%s/eth/v1/validator/duties/proposer/%d", o.BeaconAPI, epoch)
	if err := getJSON(ctx, o.Client, url, &resp); err != nil {
		return 0, fmt.Errorf("fetch proposer duties for epoch %d: %w", epoch, err)
	}

	wantSlot := strconv.FormatUint(slot, 10)
	for _, d := range resp.Data {
		if d.Slot != wantSlot {
			continue
		}
		vi, err := strconv.ParseUint(d.ValidatorIndex, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse proposer validator_index %q: %w", d.ValidatorIndex, err)
		}
		return vi, nil
	}
	return 0, fmt.Errorf("no proposer duty found for slot %d", slot)
}

// blockAttestation is the subset of an Electra-style attestation this tool reads.
// Committee-bits (EIP-7549 multi-committee aggregation) are deliberately not
// handled: this devnet's committee-per-slot count is small enough that Lighthouse
// represents each attestation as a single committee's data plus that committee's
// aggregation bitlist, which is what CheckInclusion decodes. A devnet with enough
// validators to produce genuinely multi-committee aggregates would need that
// added — a real gap, not a hidden one.
type blockAttestation struct {
	AggregationBits string `json:"aggregation_bits"`
	Data            struct {
		Slot  string `json:"slot"`
		Index string `json:"index"`
	} `json:"data"`
}

// PollBlockSeen polls for slot's block until it appears or deadline passes,
// returning the instant the poll loop first observed it.
func (o *Observer) PollBlockSeen(ctx context.Context, slot uint64, deadline time.Time) (blockRoot string, proposerIndex uint64, seenAt time.Time, found bool, err error) {
	const pollInterval = 500 * time.Millisecond

	for {
		block, root, ok, ferr := o.fetchBlock(ctx, slot)
		if ferr != nil {
			return "", 0, time.Time{}, false, ferr
		}
		if ok {
			pi, perr := strconv.ParseUint(block.Message.ProposerIndex, 10, 64)
			if perr != nil {
				return "", 0, time.Time{}, false, fmt.Errorf("parse proposer_index %q: %w", block.Message.ProposerIndex, perr)
			}
			return root, pi, time.Now().UTC(), true, nil
		}
		if !time.Now().Before(deadline) {
			return "", 0, time.Time{}, false, nil
		}
		select {
		case <-ctx.Done():
			return "", 0, time.Time{}, false, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// poolAttestation is the subset of the attestation-pool response this tool reads.
// Same Electra-style, single-committee-per-attestation shape as blockAttestation
// — see that type's doc comment for the scope this implies.
type poolAttestation struct {
	AggregationBits string `json:"aggregation_bits"`
	Data            struct {
		Slot  string `json:"slot"`
		Index string `json:"index"`
	} `json:"data"`
}

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
func (o *Observer) PollAttestationPublished(ctx context.Context, dutySlot uint64, d duty, deadline time.Time) (publishedAt time.Time, found bool, err error) {
	const pollInterval = 500 * time.Millisecond

	for {
		ok, ferr := o.poolIncludesAttestation(ctx, dutySlot, d)
		if ferr != nil {
			return time.Time{}, false, ferr
		}
		if ok {
			return time.Now().UTC(), true, nil
		}
		if !time.Now().Before(deadline) {
			return time.Time{}, false, nil
		}
		select {
		case <-ctx.Done():
			return time.Time{}, false, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func (o *Observer) poolIncludesAttestation(ctx context.Context, dutySlot uint64, d duty) (bool, error) {
	url := fmt.Sprintf("%s/eth/v1/beacon/pool/attestations?slot=%d", o.BeaconAPI, dutySlot)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("build attestation pool request for slot %d: %w", dutySlot, err)
	}
	httpResp, err := o.Client.Do(req)
	if err != nil {
		return false, fmt.Errorf("fetch attestation pool for slot %d: %w", dutySlot, err)
	}
	defer httpResp.Body.Close() //nolint:errcheck // read-only response body; nothing to act on if Close fails

	// The pool only holds recent, not-yet-included attestations — once a slot
	// ages out (its data pruned, or its epoch has moved on), the beacon node
	// reports 410 Gone rather than an empty result. That is not a failure to
	// report: it means the window for seeing this validator publish here has
	// closed, the same as a 404 on a block that was never produced.
	if httpResp.StatusCode == http.StatusGone || httpResp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if httpResp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("fetch attestation pool for slot %d: unexpected status %d", dutySlot, httpResp.StatusCode)
	}

	var resp struct {
		Data []poolAttestation `json:"data"`
	}
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return false, fmt.Errorf("decode attestation pool for slot %d: %w", dutySlot, err)
	}

	wantIndexStr := strconv.FormatUint(d.CommitteeIndex, 10)
	for _, att := range resp.Data {
		if att.Data.Index != wantIndexStr {
			continue
		}
		included, err := bitSet(att.AggregationBits, d.ValidatorCommitteeIndex)
		if err != nil {
			return false, fmt.Errorf("decode aggregation_bits for slot %d: %w", dutySlot, err)
		}
		if included {
			return true, nil
		}
	}
	return false, nil
}

// CheckInclusion looks for d's validator's attestation bit set in any block from
// dutySlot+1 up to and including untilSlot, returning the slot and instant it was
// found included.
func (o *Observer) CheckInclusion(ctx context.Context, dutySlot uint64, d duty, untilSlot uint64, deadline time.Time) (includedInSlot uint64, includedAt time.Time, found bool, err error) {
	for {
		for s := dutySlot + 1; s <= untilSlot; s++ {
			ok, ferr := o.blockIncludesAttestation(ctx, s, dutySlot, d)
			if ferr != nil {
				return 0, time.Time{}, false, ferr
			}
			if ok {
				return s, time.Now().UTC(), true, nil
			}
		}
		if !time.Now().Before(deadline) {
			return 0, time.Time{}, false, nil
		}
		select {
		case <-ctx.Done():
			return 0, time.Time{}, false, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (o *Observer) blockIncludesAttestation(ctx context.Context, blockSlot, wantSlot uint64, d duty) (bool, error) {
	block, _, ok, err := o.fetchBlock(ctx, blockSlot)
	if err != nil || !ok {
		return false, err
	}
	wantSlotStr := strconv.FormatUint(wantSlot, 10)
	wantIndexStr := strconv.FormatUint(d.CommitteeIndex, 10)
	for _, att := range block.Message.Body.Attestations {
		if att.Data.Slot != wantSlotStr || att.Data.Index != wantIndexStr {
			continue
		}
		included, err := bitSet(att.AggregationBits, d.ValidatorCommitteeIndex)
		if err != nil {
			return false, fmt.Errorf("decode aggregation_bits for slot %d: %w", blockSlot, err)
		}
		if included {
			return true, nil
		}
	}
	return false, nil
}

type beaconBlock struct {
	Message struct {
		ProposerIndex string `json:"proposer_index"`
		Body          struct {
			Attestations []blockAttestation `json:"attestations"`
		} `json:"body"`
	} `json:"message"`
}

// fetchBlock fetches slot's block, treating "not found" (slot empty or not yet
// produced) as a normal, non-error outcome — that absence is itself evidence
// (docs/causes.md, network.proposer_missed).
func (o *Observer) fetchBlock(ctx context.Context, slot uint64) (beaconBlock, string, bool, error) {
	url := fmt.Sprintf("%s/eth/v2/beacon/blocks/%d", o.BeaconAPI, slot)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return beaconBlock{}, "", false, fmt.Errorf("build block request: %w", err)
	}

	resp, err := o.Client.Do(req)
	if err != nil {
		return beaconBlock{}, "", false, fmt.Errorf("fetch block %d: %w", slot, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response body; nothing to act on if Close fails

	if resp.StatusCode == http.StatusNotFound {
		return beaconBlock{}, "", false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return beaconBlock{}, "", false, fmt.Errorf("fetch block %d: unexpected status %d", slot, resp.StatusCode)
	}

	var out struct {
		Data beaconBlock `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return beaconBlock{}, "", false, fmt.Errorf("decode block %d: %w", slot, err)
	}
	root := resp.Header.Get("Eth-Consensus-Block-Root")
	return out.Data, root, true, nil
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
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response from %s: %w", req.URL, err)
	}
	return nil
}

// buildObservations turns what RunScenario actually measured into
// domain.Observation values, sorted by timestamp as domain.Timeline requires.
// Every value here traces to something recorded during this run — see
// RunScenario's doc comment.
// dutyOutcome carries everything RunScenario measured about how one duty slot
// resolved — the input buildObservations turns into domain.Observation values.
// One field per fact, grown as new evidence kinds (SampleIOPressure,
// SampleEngineCallDurations) were added, so extending what a scenario can
// record means adding a field here rather than another positional parameter
// to buildObservations.
type dutyOutcome struct {
	BlockFound    bool
	BlockRoot     string
	ProposerIndex uint64
	BlockSeenAt   time.Time

	Published   bool
	PublishedAt time.Time

	Included       bool
	IncludedInSlot uint64
	IncludedAt     time.Time

	// HostPressure is the sampled io.pressure "some avg10" percentage. Present
	// only when Scenario.SampleHostPressure was set.
	HostPressure  *float64
	HostSampledAt time.Time

	// EngineSamples is what SampleEngineCallDurations returned. Present only
	// when Scenario.MetricsTarget was set.
	EngineSamples   []EngineCallSample
	EngineSampledAt time.Time
}

func buildObservations(s Scenario, slot uint64, slotStart, dutyAt time.Time, o dutyOutcome) ([]domain.Observation, error) {
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
		attrs := map[domain.AttrKey]string{
			domain.AttrProposerIndex: strconv.FormatUint(o.ProposerIndex, 10),
		}
		if o.BlockRoot != "" {
			attrs[domain.AttrBlockRoot] = o.BlockRoot
		}
		drafts = append(drafts, domain.Observation{
			Slot: domain.Slot(slot), Kind: domain.ObsBlockSeen,
			At: o.BlockSeenAt, Source: domain.SourceBeaconAPI, Attrs: attrs,
		})
	}

	if o.Published {
		drafts = append(drafts, domain.Observation{
			Slot: domain.Slot(slot), Kind: domain.ObsAttestationPublished,
			At: o.PublishedAt, Source: domain.SourceBeaconAPI,
			Attrs: map[domain.AttrKey]string{
				domain.AttrValidatorIndex: strconv.FormatUint(s.ValidatorIndex, 10),
			},
		})
	}

	if o.Included {
		drafts = append(drafts, domain.Observation{
			Slot: domain.Slot(slot), Kind: domain.ObsAttestationIncluded,
			At: o.IncludedAt, Source: domain.SourceBeaconAPI,
			Attrs: map[domain.AttrKey]string{
				domain.AttrValidatorIndex: strconv.FormatUint(s.ValidatorIndex, 10),
				domain.AttrInclusionDelay: strconv.FormatUint(o.IncludedInSlot-slot, 10),
			},
		})
	}

	if o.HostPressure != nil {
		drafts = append(drafts, domain.Observation{
			Slot: domain.Slot(slot), Kind: domain.ObsHostSampled,
			At: o.HostSampledAt, Source: domain.SourceHostMetrics,
			Attrs: map[domain.AttrKey]string{
				domain.AttrMetric: "iowait_pct",
				domain.AttrValue:  strconv.FormatFloat(*o.HostPressure, 'f', -1, 64),
			},
		})
	}

	for _, sample := range o.EngineSamples {
		drafts = append(drafts, domain.Observation{
			Slot: domain.Slot(slot), Kind: domain.ObsEngineCall,
			At: o.EngineSampledAt, Source: domain.SourcePromScrape,
			Attrs: map[domain.AttrKey]string{
				domain.AttrEngineMethod: sample.Method,
				domain.AttrDurationMS:   strconv.FormatFloat(sample.DurationMS, 'f', -1, 64),
			},
		})
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
