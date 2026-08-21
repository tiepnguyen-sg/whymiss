package main

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// EngineCallSample is one Engine API method's recent call duration, read from
// an execution client's own Prometheus metrics.
type EngineCallSample struct {
	Method     string
	DurationMS float64
}

// engineMetricNames maps the Engine API methods docs/causes.md's stage
// decomposition cares about to geth's Prometheus summary metric names.
// Verified present on a live geth node: `rpc_duration_engine_<Method>_success`,
// a summary with a "quantile=0.5" series, values in nanoseconds. Confined to
// geth's naming — BUILD_PROMPT §3 locks initial client support to Lighthouse
// and Prysm, both of which pair with geth in this project's devnet, so no
// other execution client's metric names are handled here.
var engineMetricNames = map[string]string{
	"newPayload":        "rpc_duration_engine_newPayloadV4_success",
	"forkchoiceUpdated": "rpc_duration_engine_forkchoiceUpdatedV3_success",
}

// SampleEngineCallDurations scrapes an execution client's Prometheus endpoint
// and returns the median (quantile 0.5) duration of each Engine API method in
// [engineMetricNames] that has been called at least once.
//
// This is a rolling figure, not a per-call isolated measurement: geth's summary
// metric reflects recent observations over its own internal window, the same
// kind of "rolling p99 baseline" docs/causes.md §7's local.el_slow rule already
// expects a caller to compare against. A method absent from the result means
// geth has not recorded a call to it yet, not that the call took zero time.
func SampleEngineCallDurations(ctx context.Context, metricsURL string) ([]EngineCallSample, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metricsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", metricsURL, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch metrics from %s: %w", metricsURL, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response body; nothing to act on if Close fails
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch metrics from %s: unexpected status %d", metricsURL, resp.StatusCode)
	}

	byMetricName := make(map[string]string, len(engineMetricNames))
	for method, metric := range engineMetricNames {
		byMetricName[metric] = method
	}

	var samples []EngineCallSample
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
		name := line[:nameEnd]
		method, ok := byMetricName[name]
		if !ok {
			continue
		}
		if !strings.Contains(line, `quantile="0.5"`) {
			continue
		}
		valueStr := line[strings.LastIndexByte(line, ' ')+1:]
		nanoseconds, err := strconv.ParseFloat(valueStr, 64)
		if err != nil {
			return nil, fmt.Errorf("parse duration for %s from line %q: %w", method, line, err)
		}
		samples = append(samples, EngineCallSample{
			Method:     method,
			DurationMS: nanoseconds / float64(time.Millisecond),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan metrics from %s: %w", metricsURL, err)
	}
	return samples, nil
}

// resolveMetricsURL asks Kurtosis for the public URL of target's "metrics"
// port within enclave — the same `kurtosis port print` lookup
// Makefile:corpus.generate already uses for the beacon API, reused here so
// this tool needs no additional CLI flag to reach an execution client's own
// Prometheus endpoint.
func resolveMetricsURL(ctx context.Context, enclave, target string) (string, error) {
	out, err := exec.CommandContext(ctx, "kurtosis", "port", "print", enclave, target, "metrics").Output()
	if err != nil {
		return "", fmt.Errorf("kurtosis port print %s %s metrics: %w", enclave, target, err)
	}
	// The first time this CLI profile ever runs `kurtosis port print`, it
	// prints a one-time analytics-disclosure banner to stdout ahead of the
	// actual URL (verified: a fresh VM's very first invocation did exactly
	// this and broke naive whole-output parsing). The URL is always the
	// command's last printed line regardless of whether that banner is
	// present, so taking the last non-empty line is robust to both cases
	// rather than depending on this being an already-"warmed-up" profile.
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	url := strings.TrimSpace(lines[len(lines)-1])
	if url == "" {
		return "", fmt.Errorf("kurtosis port print %s %s metrics: empty result", enclave, target)
	}
	return url + "/debug/metrics/prometheus", nil
}
