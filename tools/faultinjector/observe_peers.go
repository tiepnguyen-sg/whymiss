package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/source"
)

// SamplePeerCount scrapes target's own Prometheus endpoint for its connected
// peer count, reusing internal/source's already-verified production
// scraper (internal/source.MetricsSampler.SamplePeerCount, which dispatches to
// internal/source/promscrape's client-specific parsers) rather than
// reimplementing the parsing here.
//
// Client selection uses the same version-based registry as production; service
// names are deployment metadata, never adapter selection logic (I-11).
func SamplePeerCount(ctx context.Context, sampler *source.MetricsSampler, enclave, target string) (domain.MetricSample, error) {
	beaconAPI, err := resolveKurtosisPort(ctx, enclave, target, "http")
	if err != nil {
		return domain.MetricSample{}, err
	}
	_, client, metricsURL, err := prepareHeadTiming(ctx, beaconAPI, enclave, target)
	if err != nil {
		return domain.MetricSample{}, fmt.Errorf("prepare peer-count sampling: %w", err)
	}

	sample, err := sampler.SamplePeerCount(ctx, client, metricsURL)
	if err != nil {
		return domain.MetricSample{}, err
	}
	return sample, nil
}

// resolveCLMetricsURL is resolveMetricsURL's counterpart for a consensus
// client's Prometheus endpoint, which is served at "/metrics" — unlike
// geth's "/debug/metrics/prometheus" — confirmed against
// cmd/whymiss/watch.go's --cl-metrics-api flag help text
// ("http://127.0.0.1:5054/metrics").
func resolveCLMetricsURL(ctx context.Context, enclave, target string) (string, error) {
	url, err := resolveKurtosisPort(ctx, enclave, target, "metrics")
	if err != nil {
		return "", err
	}
	return url + "/metrics", nil
}

func resolveKurtosisPort(ctx context.Context, enclave, target, port string) (string, error) {
	out, err := exec.CommandContext(ctx, "kurtosis", "port", "print", enclave, target, port).Output()
	if err != nil {
		return "", fmt.Errorf("kurtosis port print %s %s %s: %w", enclave, target, port, err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	url := strings.TrimSpace(lines[len(lines)-1])
	if url == "" {
		return "", fmt.Errorf("kurtosis port print %s %s %s: empty result", enclave, target, port)
	}
	return url, nil
}
