package beaconapi

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"
)

// GenesisInfo is the chain configuration every slot-time computation in this
// package needs. Fetch it once at startup — it never changes for a running
// chain — and pass it to the methods that need it.
type GenesisInfo struct {
	GenesisTime    time.Time
	SecondsPerSlot time.Duration
}

// SlotStart returns the wall-clock instant slot begins.
func (g GenesisInfo) SlotStart(slot uint64) time.Time {
	if g.SecondsPerSlot <= 0 || slot > uint64(math.MaxInt64/int64(g.SecondsPerSlot)) { //nolint:gosec // positive duration is checked before conversion
		return g.GenesisTime.Add(time.Duration(math.MaxInt64))
	}
	return g.GenesisTime.Add(time.Duration(slot) * g.SecondsPerSlot) //nolint:gosec // slot is proven representable as a duration above
}

// FetchGenesis reads GET /eth/v1/beacon/genesis and GET /eth/v1/config/spec
// and combines them into a [GenesisInfo]. Both are one-time, unchanging
// facts about the chain the node is following.
func (c *Client) FetchGenesis(ctx context.Context) (GenesisInfo, error) {
	var genesis struct {
		GenesisTime string `json:"genesis_time"`
	}
	found, err := c.get(ctx, "/eth/v1/beacon/genesis", &genesis)
	if err != nil {
		return GenesisInfo{}, fmt.Errorf("fetch genesis: %w", err)
	}
	if !found {
		return GenesisInfo{}, fmt.Errorf("fetch genesis: node reports no genesis yet")
	}
	genesisUnix, err := strconv.ParseInt(genesis.GenesisTime, 10, 64)
	if err != nil {
		return GenesisInfo{}, fmt.Errorf("fetch genesis: parse genesis_time %q: %w", genesis.GenesisTime, err)
	}

	// Decode only the one field this package needs, not the whole spec
	// object: /eth/v1/config/spec mixes plain string values with
	// non-string ones (e.g. BLOB_SCHEDULE, an array of {EPOCH,
	// MAX_BLOBS_PER_BLOCK} objects added for a later fork), which a
	// map[string]string can't unmarshal — found by running against a real
	// beacon node (Hoodi testnet) whose spec response includes that field;
	// this project's Kurtosis devnet genesis predates it. A struct with
	// only the field we read lets encoding/json ignore everything else,
	// whatever shape it takes.
	var spec struct {
		SecondsPerSlot string `json:"SECONDS_PER_SLOT"`
	}
	if _, err := c.get(ctx, "/eth/v1/config/spec", &spec); err != nil {
		return GenesisInfo{}, fmt.Errorf("fetch spec: %w", err)
	}
	secondsPerSlot, err := strconv.ParseInt(spec.SecondsPerSlot, 10, 64)
	if err != nil {
		return GenesisInfo{}, fmt.Errorf("fetch spec: parse SECONDS_PER_SLOT %q: %w", spec.SecondsPerSlot, err)
	}
	if secondsPerSlot <= 0 || secondsPerSlot > 60 {
		return GenesisInfo{}, fmt.Errorf("fetch spec: SECONDS_PER_SLOT %d is outside supported range [1,60]", secondsPerSlot)
	}

	return GenesisInfo{
		GenesisTime:    time.Unix(genesisUnix, 0).UTC(),
		SecondsPerSlot: time.Duration(secondsPerSlot) * time.Second,
	}, nil
}
