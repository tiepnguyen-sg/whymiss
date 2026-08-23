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

	var spec map[string]string
	if _, err := c.get(ctx, "/eth/v1/config/spec", &spec); err != nil {
		return GenesisInfo{}, fmt.Errorf("fetch spec: %w", err)
	}
	secondsPerSlot, err := strconv.ParseInt(spec["SECONDS_PER_SLOT"], 10, 64)
	if err != nil {
		return GenesisInfo{}, fmt.Errorf("fetch spec: parse SECONDS_PER_SLOT %q: %w", spec["SECONDS_PER_SLOT"], err)
	}
	if secondsPerSlot <= 0 || secondsPerSlot > 60 {
		return GenesisInfo{}, fmt.Errorf("fetch spec: SECONDS_PER_SLOT %d is outside supported range [1,60]", secondsPerSlot)
	}

	return GenesisInfo{
		GenesisTime:    time.Unix(genesisUnix, 0).UTC(),
		SecondsPerSlot: time.Duration(secondsPerSlot) * time.Second,
	}, nil
}
