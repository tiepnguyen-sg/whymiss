package beaconapi

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// MetricCLPeerCount mirrors promscrape.MetricCLPeerCount. The normalised name
// is what R-200 reads (docs/causes.md's local.p2p_degraded), and it stays the
// same regardless of which adapter produced the fact.
const MetricCLPeerCount domain.MetricName = "cl_peer_count"

// PeerCount reads GET /eth/v1/node/peer_count, the standard Beacon API endpoint
// for how many peers a consensus node currently has
// (github.com/ethereum/beacon-APIs).
//
// This fact used to be scraped from each client's Prometheus endpoint, and that
// was wrong on Lighthouse. Lighthouse v8.2.2 keeps a `libp2p_peers` gauge
// registered and reports **0** on it while genuinely peered: measured on a fresh
// two-node devnet where `/eth/v1/node/peer_count` returned `connected: 1`,
// Prysm's own `connected_libp2p_peers{agent="lighthouse"}` returned 1, and
// Lighthouse's gossip mesh showed `block_mesh_peers_per_client{Client="Prysm"} 1`
// — while every `libp2p_peers*` series read 0. The recorded fixture the unit
// test replays (`promscrape/testdata/lighthouse_metrics.txt`) contains
// `libp2p_peers 0` too, so the test agreed with the adapter and neither noticed.
//
// The consequence was not cosmetic. R-200 will only blame local.p2p_degraded
// when the peer count is *below* thresholds.peer_count_min, so a permanent zero
// made that corroboration vacuous on Lighthouse: the check that is supposed to
// establish "insufficient peering explains this delay" passed unconditionally.
//
// Reading the standardised endpoint removes the client-specific parsing for this
// fact entirely, which is the direction I-11 points, and makes the same code
// correct for any client that implements the Beacon API.
func (c *Client) PeerCount(ctx context.Context) (domain.MetricSample, error) {
	var resp struct {
		Connected string `json:"connected"`
	}
	found, err := c.get(ctx, "/eth/v1/node/peer_count", &resp)
	if err != nil {
		return domain.MetricSample{}, fmt.Errorf("fetch peer count: %w", err)
	}
	if !found {
		return domain.MetricSample{}, fmt.Errorf("fetch peer count: not found")
	}
	value, err := strconv.ParseFloat(resp.Connected, 64)
	if err != nil {
		return domain.MetricSample{}, fmt.Errorf("parse connected peer count %q: %w", resp.Connected, err)
	}
	if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return domain.MetricSample{}, fmt.Errorf("connected peer count must be finite and non-negative, got %q", resp.Connected)
	}
	sample := domain.MetricSample{
		At:        time.Now().UTC(),
		Component: domain.ComponentCL,
		Name:      MetricCLPeerCount,
		Value:     value,
		Source:    domain.SourceBeaconAPI,
	}
	if err := sample.Validate(); err != nil {
		return domain.MetricSample{}, fmt.Errorf("build sample %s: %w", MetricCLPeerCount, err)
	}
	return sample, nil
}
