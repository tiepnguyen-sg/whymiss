package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/tiepnguyen-sg/whymiss/internal/source"
)

// SamplePeerCount scrapes target's own Prometheus endpoint for its connected
// peer count, reusing internal/source's already-verified production
// scraper (internal/source.SamplePeerCount, which dispatches to
// internal/source/promscrape's client-specific parsers) rather than
// reimplementing the parsing here.
//
// tools/faultinjector is not bound by I-11's client-isolation rule (that
// only restricts internal and cmd — see make check.isolation's own grep
// scope), so branching on whether target's name contains "lighthouse" or
// "prysm" to pick the right internal/source.ConsensusClient is fine here,
// the same way cmd/whymiss/watch.go does for --cl-metrics-api in
// production.
func SamplePeerCount(ctx context.Context, enclave, target string) (float64, error) {
	metricsURL, err := resolveCLMetricsURL(ctx, enclave, target)
	if err != nil {
		return 0, err
	}

	var client source.ConsensusClient
	switch {
	case strings.Contains(target, "lighthouse"):
		client = source.ConsensusLighthouse
	case strings.Contains(target, "prysm"):
		client = source.ConsensusPrysm
	default:
		return 0, fmt.Errorf("peer_count_target %q: cannot tell consensus client from name (want it to contain \"lighthouse\" or \"prysm\")", target)
	}

	sample, err := source.SamplePeerCount(ctx, client, metricsURL)
	if err != nil {
		return 0, err
	}
	return sample.Value, nil
}

// resolveCLMetricsURL is resolveMetricsURL's counterpart for a consensus
// client's Prometheus endpoint, which is served at "/metrics" — unlike
// geth's "/debug/metrics/prometheus" — confirmed against
// cmd/whymiss/watch.go's --cl-metrics-api flag help text
// ("http://127.0.0.1:5054/metrics").
func resolveCLMetricsURL(ctx context.Context, enclave, target string) (string, error) {
	out, err := exec.CommandContext(ctx, "kurtosis", "port", "print", enclave, target, "metrics").Output()
	if err != nil {
		return "", fmt.Errorf("kurtosis port print %s %s metrics: %w", enclave, target, err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	url := strings.TrimSpace(lines[len(lines)-1])
	if url == "" {
		return "", fmt.Errorf("kurtosis port print %s %s metrics: empty result", enclave, target)
	}
	return url + "/metrics", nil
}
