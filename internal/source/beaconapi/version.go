package beaconapi

import (
	"context"
	"fmt"
)

// FetchNodeVersion reads GET /eth/v1/node/version, the standard Beacon API
// endpoint a node uses to self-report its client and version — the
// standard, spec-documented way to detect which client this package is
// talking to (github.com/ethereum/beacon-APIs), rather than relying on the
// HTTP "Server" response header some clients also happen to set.
func (c *Client) FetchNodeVersion(ctx context.Context) (string, error) {
	var resp struct {
		Version string `json:"version"`
	}
	found, err := c.get(ctx, "/eth/v1/node/version", &resp)
	if err != nil {
		return "", fmt.Errorf("fetch node version: %w", err)
	}
	if !found {
		return "", fmt.Errorf("fetch node version: not found")
	}
	return resp.Version, nil
}
