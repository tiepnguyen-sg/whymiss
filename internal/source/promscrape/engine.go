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

// requestTimeout bounds every scrape this package makes (I-5).
const requestTimeout = 10 * time.Second

// Engine call durations, normalised to domain.MetricName. Two per method:
// this package's own vocabulary, not geth's raw metric name, so
// internal/rca never has to know geth calls it
// "rpc_duration_engine_newPayloadV4_success" (I-11) — and so a second EL
// client, when one is added, normalises into the same two names instead of
// its own.
const (
	// MetricELNewPayloadMS is the newPayload Engine API call's rolling
	// median duration in milliseconds.
	MetricELNewPayloadMS domain.MetricName = "el_engine_newpayload_ms"

	// MetricELForkchoiceUpdatedMS is the forkchoiceUpdated Engine API
	// call's rolling median duration in milliseconds.
	MetricELForkchoiceUpdatedMS domain.MetricName = "el_engine_forkchoiceupdated_ms"
)

// engineMetricNames maps geth's own Prometheus summary metric names (as
// scraped from its /debug/metrics/prometheus endpoint) to the normalised
// MetricName this package reports. Verified against a live geth node
// (test/e2e/kurtosis): a summary with a "quantile" label, values in
// nanoseconds. BUILD_PROMPT §3 locks the initial EL client scope to geth
// paired with Lighthouse or Prysm, so no other EL client's metric names are
// handled here yet.
var engineMetricNames = map[string]domain.MetricName{
	"rpc_duration_engine_newPayloadV4_success":        MetricELNewPayloadMS,
	"rpc_duration_engine_forkchoiceUpdatedV3_success": MetricELForkchoiceUpdatedMS,
}

// SampleEngineCalls scrapes an execution client's Prometheus endpoint
// (metricsURL, e.g. "http://host:6060/debug/metrics/prometheus") and
// returns the median (quantile 0.5) duration of each Engine API method in
// [engineMetricNames] that has been called at least once, as
// domain.MetricSample values attributed to domain.ComponentEL.
//
// This is a rolling figure, not a per-call isolated measurement: geth's
// summary metric reflects recent observations over its own internal
// window — the same kind of "rolling baseline" docs/causes.md §7's
// local.el_slow rule compares a single call's duration against
// (MetricSample's own doc comment). A method absent from the result means
// geth has not recorded a call to it yet, not that the call took zero time.
func SampleEngineCalls(ctx context.Context, metricsURL string) ([]domain.MetricSample, error) {
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

	at := time.Now().UTC()
	var samples []domain.MetricSample
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		nameEnd := strings.IndexAny(line, " {")
		if nameEnd < 0 {
			continue
		}
		metricName, ok := engineMetricNames[line[:nameEnd]]
		if !ok {
			continue
		}
		if !strings.Contains(line, `quantile="0.5"`) {
			continue
		}
		valueStr := line[strings.LastIndexByte(line, ' ')+1:]
		nanoseconds, err := strconv.ParseFloat(valueStr, 64)
		if err != nil {
			return nil, fmt.Errorf("parse value for %s from line %q: %w", metricName, line, err)
		}

		sample := domain.MetricSample{
			At:        at,
			Component: domain.ComponentEL,
			Name:      metricName,
			Value:     nanoseconds / float64(time.Millisecond),
			Source:    domain.SourcePromScrape,
		}
		if err := sample.Validate(); err != nil {
			return nil, fmt.Errorf("build sample for %s: %w", metricName, err)
		}
		samples = append(samples, sample)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan metrics from %s: %w", metricsURL, err)
	}
	return samples, nil
}
