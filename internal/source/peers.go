package source

import (
	"context"
	"fmt"

	"github.com/CHANGEME/whymiss/internal/domain"
	"github.com/CHANGEME/whymiss/internal/source/promscrape"
)

// SamplePeerCount scrapes client's Prometheus endpoint for its connected
// peer count, dispatching to the adapter that knows how to read it — the
// "adapter selection" half of task 2.4's "client detection and adapter
// selection" (DetectConsensusClient is the other half).
//
// This is the one place outside promscrape itself allowed to reference a
// consensus client's adapter function by name (I-11) — a caller in
// internal/app detects the client once and passes the resulting
// ConsensusClient here on every subsequent call, never touching a
// client-named symbol itself. Adding a third client means adding one case
// here and one new function in internal/source/promscrape; nothing outside
// internal/source changes, which is what makes that claim in
// docs/architecture.md true rather than aspirational.
func SamplePeerCount(ctx context.Context, client ConsensusClient, metricsURL string) (domain.MetricSample, error) {
	switch client {
	case ConsensusLighthouse:
		return promscrape.SampleLighthousePeerCount(ctx, metricsURL)
	case ConsensusPrysm:
		return promscrape.SamplePrysmPeerCount(ctx, metricsURL)
	default:
		return domain.MetricSample{}, fmt.Errorf("peer count sampling not implemented for consensus client %q", client)
	}
}
