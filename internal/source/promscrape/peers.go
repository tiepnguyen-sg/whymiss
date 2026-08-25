package promscrape

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
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
const MetricCLPeerCount domain.MetricName = "cl_peer_count"

// SampleLighthousePeerCount scrapes a Lighthouse node's Prometheus endpoint
// for its "libp2p_peers" gauge (verified against a live devnet node:
// unlabelled, a single value — see testdata/lighthouse_metrics.txt).
func (s *Scraper) SampleLighthousePeerCount(ctx context.Context, metricsURL string) (domain.MetricSample, error) {
	lines, err := s.fetchMetricsLines(ctx, metricsURL)
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
func (s *Scraper) SamplePrysmPeerCount(ctx context.Context, metricsURL string) (domain.MetricSample, error) {
	lines, err := s.fetchMetricsLines(ctx, metricsURL)
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
		if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return domain.MetricSample{}, fmt.Errorf("connected_libp2p_peers value must be finite and non-negative in line %q", line)
		}
		total += v
		found = true
	}
	// Unlike SampleLighthousePeerCount's bare, always-registered
	// "libp2p_peers" gauge, connected_libp2p_peers is a per-agent labelled
	// vector: a Prometheus client only exposes a label combination once it
	// has actually occurred, so a Prysm node with zero currently connected
	// peers omits the series entirely rather than reporting it at 0 — found
	// running local.p2p_degraded's netem isolation fault against a real
	// Prysm node, which reliably produces exactly that state (this devnet's
	// only real capture, testdata/prysm_metrics.txt, happened to have one
	// peer connected and never exercised the zero case). No matching line
	// after a successful, well-formed scrape is that legitimate zero, not a
	// missing or misnamed metric.
	if !found {
		return buildPeerCountSample("0")
	}
	return buildPeerCountSample(strconv.FormatFloat(total, 'f', -1, 64))
}

func buildPeerCountSample(valueStr string) (domain.MetricSample, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(valueStr), 64)
	if err != nil {
		return domain.MetricSample{}, fmt.Errorf("parse peer count %q: %w", valueStr, err)
	}
	if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return domain.MetricSample{}, fmt.Errorf("peer count must be finite and non-negative, got %q", valueStr)
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
