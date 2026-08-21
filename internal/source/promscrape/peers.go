package promscrape

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/CHANGEME/whymiss/internal/domain"
)

// MetricCLPeerCount is the consensus client's connected-peer count,
// normalised across clients (I-11) — docs/causes.md's local.p2p_degraded
// rule (R-200) reads it via thresholds.peer_count_min.
const MetricCLPeerCount domain.MetricName = "cl_peer_count"

// SampleLighthousePeerCount scrapes a Lighthouse node's Prometheus endpoint
// for its "libp2p_peers" gauge (verified against a live devnet node:
// unlabelled, a single value — see testdata/lighthouse_metrics.txt).
func SampleLighthousePeerCount(ctx context.Context, metricsURL string) (domain.MetricSample, error) {
	lines, err := fetchMetricsLines(ctx, metricsURL)
	if err != nil {
		return domain.MetricSample{}, err
	}
	for _, line := range lines {
		value, ok := strings.CutPrefix(line, "libp2p_peers ")
		if !ok {
			continue
		}
		return buildPeerCountSample(value)
	}
	return domain.MetricSample{}, fmt.Errorf("libp2p_peers not found in metrics from %s", metricsURL)
}

// SamplePrysmPeerCount scrapes a Prysm node's Prometheus endpoint for its
// "connected_libp2p_peers" gauge (verified against a live devnet node:
// labelled by peer agent string, e.g. connected_libp2p_peers{agent="lighthouse"}
// — see testdata/prysm_metrics.txt) and sums every label's value, since the
// normalised metric is the total peer count regardless of which client
// each peer runs.
func SamplePrysmPeerCount(ctx context.Context, metricsURL string) (domain.MetricSample, error) {
	lines, err := fetchMetricsLines(ctx, metricsURL)
	if err != nil {
		return domain.MetricSample{}, err
	}

	var total float64
	found := false
	for _, line := range lines {
		rest, ok := strings.CutPrefix(line, "connected_libp2p_peers{")
		if !ok {
			continue
		}
		closeIdx := strings.IndexByte(rest, ' ')
		if closeIdx < 0 {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(rest[closeIdx+1:]), 64)
		if err != nil {
			return domain.MetricSample{}, fmt.Errorf("parse connected_libp2p_peers value from line %q: %w", line, err)
		}
		total += v
		found = true
	}
	if !found {
		return domain.MetricSample{}, fmt.Errorf("connected_libp2p_peers not found in metrics from %s", metricsURL)
	}
	return buildPeerCountSample(strconv.FormatFloat(total, 'f', -1, 64))
}

func buildPeerCountSample(valueStr string) (domain.MetricSample, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(valueStr), 64)
	if err != nil {
		return domain.MetricSample{}, fmt.Errorf("parse peer count %q: %w", valueStr, err)
	}
	sample := domain.MetricSample{
		At:        time.Now().UTC(),
		Component: domain.ComponentCL,
		Name:      MetricCLPeerCount,
		Value:     value,
		Source:    domain.SourcePromScrape,
	}
	if err := sample.Validate(); err != nil {
		return domain.MetricSample{}, fmt.Errorf("build sample %s: %w", MetricCLPeerCount, err)
	}
	return sample, nil
}

// fetchMetricsLines fetches metricsURL and returns its body as non-comment,
// non-empty lines — the shared scanning step both peer-count scrapers
// start from.
func fetchMetricsLines(ctx context.Context, metricsURL string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metricsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", metricsURL, err)
	}
	client := &http.Client{Timeout: requestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch metrics from %s: %w", metricsURL, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response body; nothing to act on if Close fails
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch metrics from %s: unexpected status %d", metricsURL, resp.StatusCode)
	}

	var lines []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan metrics from %s: %w", metricsURL, err)
	}
	return lines, nil
}
