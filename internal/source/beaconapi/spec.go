package beaconapi

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// forkUnscheduled is the sentinel a client reports for a fork with no epoch
// assigned. It is 2^64-1 rather than an absent key, so "is Gloas scheduled" is a
// comparison, never a presence check.
const forkUnscheduled = uint64(math.MaxUint64)

// basisPoints is the denominator the consensus spec expresses slot deadlines in:
// ATTESTATION_DUE_BPS 3333 means 33.33% of the way through the slot.
const basisPoints = 10000

// FetchSchedule derives the slot schedule from GET /eth/v1/config/spec, which is
// the node's own statement of the timing model it is running.
//
// It reports false when the node does not publish enough to build one, which is
// not an error: an operator on a client that predates these keys keeps whatever
// they configured.
//
// **Do not decide "this network is post-ePBS" from the presence of the payload
// keys.** A pre-ePBS node publishes them too — measured 2026-08-30, the public
// Hoodi gateway reports PAYLOAD_DUE_BPS and ATTESTATION_DUE_BPS_GLOAS despite
// having Gloas unscheduled, because the client binary knows the constants for a
// fork the network has not reached. Worse, its PAYLOAD_DUE_BPS was 7500 where
// the Glamsterdam devnet's was 5000, so trusting presence would have produced a
// confident, wrong payload deadline on every mainnet node. GLOAS_FORK_EPOCH is
// what decides, and only when the chain has actually reached it.
func (c *Client) FetchSchedule(ctx context.Context, headEpoch uint64) (domain.SlotSchedule, bool, error) {
	// json.RawMessage, not string: the spec's data object is not uniformly
	// string-valued. A real response carries BLOB_SCHEDULE as an array, and
	// decoding into map[string]string fails on the whole document — which is
	// how this was found, against a live node, after fixtures trimmed to the
	// keys of interest had hidden it.
	var spec struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	found, err := c.getEnvelope(ctx, "/eth/v1/config/spec", &spec)
	if err != nil {
		return domain.SlotSchedule{}, false, fmt.Errorf("fetch spec: %w", err)
	}
	if !found || len(spec.Data) == 0 {
		return domain.SlotSchedule{}, false, nil
	}

	secondsPerSlot, ok := specUint(spec.Data, "SECONDS_PER_SLOT")
	if !ok || secondsPerSlot == 0 || secondsPerSlot > uint64(maxSupportedSlotSeconds) {
		return domain.SlotSchedule{}, false, nil
	}
	slot := time.Duration(secondsPerSlot) * time.Second //nolint:gosec // bounded above

	attestationBPS, haveAttestation := specUint(spec.Data, "ATTESTATION_DUE_BPS")
	aggregateBPS, haveAggregate := specUint(spec.Data, "AGGREGATE_DUE_BPS")
	if !haveAttestation || !haveAggregate {
		return domain.SlotSchedule{}, false, nil
	}

	schedule := domain.SlotSchedule{SecondsPerSlot: slot}

	gloasEpoch, haveGloas := specUint(spec.Data, "GLOAS_FORK_EPOCH")
	postEPBS := haveGloas && gloasEpoch != forkUnscheduled && headEpoch >= gloasEpoch
	if postEPBS {
		if bps, ok := specUint(spec.Data, "ATTESTATION_DUE_BPS_GLOAS"); ok {
			attestationBPS = bps
		}
		payloadBPS, havePayload := specUint(spec.Data, "PAYLOAD_DUE_BPS")
		ptcBPS, havePTC := specUint(spec.Data, "PAYLOAD_ATTESTATION_DUE_BPS")
		if !havePayload || !havePTC {
			return domain.SlotSchedule{}, false, nil
		}
		schedule.PayloadRevealDeadline = deadlineFromBPS(slot, payloadBPS)
		schedule.PTCDeadline = deadlineFromBPS(slot, ptcBPS)
	}
	schedule.AttestationDeadline = deadlineFromBPS(slot, attestationBPS)
	schedule.AggregationDeadline = deadlineFromBPS(slot, aggregateBPS)

	if err := schedule.Validate(); err != nil {
		return domain.SlotSchedule{}, false, fmt.Errorf("schedule derived from the node's spec is unusable: %w", err)
	}
	return schedule, true, nil
}

// maxSupportedSlotSeconds mirrors domain's own ceiling on a slot duration; a
// spec claiming more than this is treated as unusable rather than trusted.
const maxSupportedSlotSeconds = 60

// deadlineFromBPS converts a basis-point offset into the slot to a duration,
// rounded to the nearest millisecond.
//
// The rounding is deliberate and it matters. Basis points are a fixed-point
// approximation: ATTESTATION_DUE_BPS 3333 over a 12s slot is 3.9996s, not the 4s
// the spec means, and carrying that 0.4ms through would shift every timing
// verdict off the documented boundary for no reason. At millisecond resolution
// mainnet's constants land exactly where docs/causes.md says they do — 4s, 8s —
// and Gloas's 2500/5000/7500 land on 3s, 6s and 9s.
func deadlineFromBPS(slot time.Duration, bps uint64) time.Duration {
	if bps > basisPoints {
		return slot
	}
	//nolint:gosec // G115: bps is bounded by basisPoints immediately above
	// int64 throughout: slot is at most maxSupportedSlotSeconds and bps at most
	// basisPoints, so the product is bounded by 6e14 nanoseconds and cannot
	// overflow. No unsigned conversion is needed, so none is written.
	nanos := (slot.Nanoseconds() * int64(bps)) / basisPoints
	return (time.Duration(nanos) * time.Nanosecond).Round(time.Millisecond)
}

// specUint reads one spec value as an unsigned integer. Anything that is not a
// quoted decimal — an array, an object, a hex string — reports false rather than
// an error: a value this build does not understand is not a reason to discard a
// schedule the rest of the document describes perfectly well.
func specUint(data map[string]json.RawMessage, key string) (uint64, bool) {
	message, ok := data[key]
	if !ok {
		return 0, false
	}
	var raw string
	if err := json.Unmarshal(message, &raw); err != nil {
		return 0, false
	}
	if raw == "" || len(raw) > 20 {
		return 0, false
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}
