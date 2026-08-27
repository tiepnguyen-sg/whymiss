package promscrape

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

const (
	// requestTimeout bounds every scrape this package makes (I-5).
	requestTimeout      = 10 * time.Second
	maxMetricsBodyBytes = 16 << 20
	maxMetricLineBytes  = 256 << 10
	maxMetricLines      = 100_000
	maxMetricConns      = 4
)

// Scraper owns the bounded HTTP client used for Prometheus scrapes. Construct
// one per collector and reuse it so connections are pooled without package
// globals or singletons.
type Scraper struct {
	httpClient *http.Client
}

// New returns a scraper with bounded connections, response sizes, redirects,
// and request duration.
func New() *Scraper {
	return &Scraper{httpClient: newMetricsHTTPClient()}
}

func newMetricsHTTPClient() *http.Client {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		transport = &http.Transport{}
	}
	bounded := transport.Clone()
	bounded.Proxy = nil
	bounded.MaxIdleConns = maxMetricConns
	bounded.MaxIdleConnsPerHost = maxMetricConns
	bounded.MaxConnsPerHost = maxMetricConns
	bounded.ResponseHeaderTimeout = requestTimeout
	return &http.Client{Transport: bounded, Timeout: requestTimeout, CheckRedirect: rejectMetricsRedirect}
}

func rejectMetricsRedirect(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }

// MetricCLPeerCount is the consensus client's connected-peer count,
// normalised across clients (I-11) — docs/causes.md's local.p2p_degraded
// rule (R-200) reads it via thresholds.peer_count_min.
//
// The value behind this name is no longer scraped from Prometheus. Lighthouse
// v8.2.2 reports 0 on its libp2p_peers gauge while genuinely peered, which made
// R-200's peer corroboration vacuous there, so the fact now comes from the
// standardised /eth/v1/node/peer_count endpoint — see
// internal/source/beaconapi.Client.PeerCount. The client-specific parsers that
// used to live here were deleted rather than left in place: a wrong adapter that
// still compiles is one refactor away from being wired back in.
const MetricCLPeerCount domain.MetricName = "cl_peer_count"

// fetchMetricsLines fetches metricsURL and returns its body as non-comment,
// non-empty lines — the shared scanning step both peer-count scrapers
// start from.
func (s *Scraper) fetchMetricsLines(ctx context.Context, metricsURL string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metricsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", metricsURL, err)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch metrics from %s: %w", metricsURL, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response body; nothing to act on if Close fails
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch metrics from %s: unexpected status %d", metricsURL, resp.StatusCode)
	}
	if resp.ContentLength > maxMetricsBodyBytes {
		return nil, fmt.Errorf("metrics response from %s is %d bytes, limit is %d", metricsURL, resp.ContentLength, maxMetricsBodyBytes)
	}

	lines := make([]string, 0, 1024)
	limited := &io.LimitedReader{R: resp.Body, N: maxMetricsBodyBytes + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 64<<10), maxMetricLineBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if len(lines) >= maxMetricLines {
			return nil, fmt.Errorf("metrics response from %s exceeds %d data lines", metricsURL, maxMetricLines)
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan metrics from %s: %w", metricsURL, err)
	}
	if limited.N == 0 {
		return nil, fmt.Errorf("metrics response from %s exceeds %d bytes", metricsURL, maxMetricsBodyBytes)
	}
	return lines, nil
}
