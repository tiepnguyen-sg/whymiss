package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/source/beaconapi"
)

// SamplePeerCount reads target's connected peer count from its Beacon API,
// reusing the same production call the collector uses
// (beaconapi.Client.PeerCount) rather than reimplementing it here.
//
// It used to scrape each client's Prometheus endpoint. That was wrong on
// Lighthouse, whose libp2p_peers gauge reads 0 while genuinely peered — see
// beaconapi.Client.PeerCount. A generator recording a peer count that is always
// zero would bake the same defect into every corpus record it wrote.
func SamplePeerCount(ctx context.Context, enclave, target string) (domain.MetricSample, error) {
	beaconAPI, err := resolveKurtosisPort(ctx, enclave, target, "http")
	if err != nil {
		return domain.MetricSample{}, err
	}
	sample, err := beaconapi.NewClient(beaconAPI, 0).PeerCount(ctx)
	if err != nil {
		return domain.MetricSample{}, fmt.Errorf("sample peer count for %s: %w", target, err)
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
