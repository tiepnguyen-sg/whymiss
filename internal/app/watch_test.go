package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/source/beaconapi"
	"github.com/tiepnguyen-sg/whymiss/internal/source/promscrape"
	"github.com/tiepnguyen-sg/whymiss/internal/store"
)

func validWatchConfig() WatchConfig {
	return WatchConfig{
		BeaconAPI:          "http://127.0.0.1:5052",
		DBPath:             "whymiss.db",
		MinRequestInterval: 200 * time.Millisecond,
		RetentionMaxAge:    14 * 24 * time.Hour,
		RetentionMaxBytes:  1 << 30,
		RetentionInterval:  time.Hour,
		// Peer sampling is no longer gated behind CLMetricsAPI, so the interval is
		// validated on every deployment. config's own default is 15s.
		PeerSampleInterval: 15 * time.Second,
	}
}

func TestWatch_CancellationWaitsForBackgroundWork(t *testing.T) {
	var connected sync.Once
	streamConnected := make(chan struct{})
	genesis := time.Now().Add(time.Hour).Unix()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/eth/v1/beacon/genesis":
			_, _ = fmt.Fprintf(w, `{"data":{"genesis_time":%q}}`, strconv.FormatInt(genesis, 10))
		case "/eth/v1/config/spec":
			_, _ = fmt.Fprint(w, `{"data":{"SECONDS_PER_SLOT":"12"}}`)
		case "/eth/v1/events":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			connected.Do(func() { close(streamConnected) })
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := validWatchConfig()
	cfg.BeaconAPI = srv.URL
	cfg.DBPath = filepath.Join(t.TempDir(), "whymiss.db")
	cfg.RetentionInterval = 0

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Watch(ctx, cfg) }()
	select {
	case <-streamConnected:
	case <-time.After(3 * time.Second):
		t.Fatal("watch did not connect to the event stream")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Watch: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Watch did not stop its background work after cancellation")
	}
}

func TestWatchConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*WatchConfig)
		want   string
	}{
		{"missing beacon API", func(c *WatchConfig) { c.BeaconAPI = "" }, "beacon API"},
		{"beacon API with credentials", func(c *WatchConfig) { c.BeaconAPI = "http://user:pass@127.0.0.1:5052" }, "credentials"},
		{"beacon API with query", func(c *WatchConfig) { c.BeaconAPI = "http://127.0.0.1:5052?token=x" }, "query"},
		{"missing database path", func(c *WatchConfig) { c.DBPath = "" }, "database path"},
		{"zero request interval", func(c *WatchConfig) { c.MinRequestInterval = 0 }, "request interval"},
		{"negative host interval", func(c *WatchConfig) { c.HostSampleInterval = -time.Second }, "host sample interval"},
		{"zero peer interval", func(c *WatchConfig) { c.PeerSampleInterval = 0 }, "peer sample interval"},
		{"zero clock interval", func(c *WatchConfig) { c.NTPServers = []string{"ntp.example"} }, "clock sample interval"},
		{"negative retention interval", func(c *WatchConfig) { c.RetentionInterval = -time.Second }, "retention interval"},
		{"zero retention age", func(c *WatchConfig) { c.RetentionMaxAge = 0 }, "retention max age"},
		{"zero retention bytes", func(c *WatchConfig) { c.RetentionMaxBytes = 0 }, "retention max bytes"},
		{"request interval too small", func(c *WatchConfig) { c.MinRequestInterval = time.Millisecond }, "request interval"},
		{"host interval too small", func(c *WatchConfig) { c.HostSampleInterval = time.Second }, "host sample interval"},
		{"peer interval too large", func(c *WatchConfig) {
			c.PeerSampleInterval = 2 * time.Minute
		}, "peer sample interval"},
		{"clock interval too large", func(c *WatchConfig) { c.NTPServers = []string{"ntp.example"}; c.ClockSampleInterval = 2 * time.Minute }, "clock sample interval"},
		{"retention interval too small", func(c *WatchConfig) { c.RetentionInterval = time.Minute }, "retention interval"},
		{"retention age too large", func(c *WatchConfig) { c.RetentionMaxAge = 100 * 24 * time.Hour }, "retention max age"},
		{"retention bytes too small", func(c *WatchConfig) { c.RetentionMaxBytes = 1 << 20 }, "retention max bytes"},
		{"too many validators", func(c *WatchConfig) {
			c.ValidatorIndices = make([]domain.ValidatorIndex, maxTrackedValidators+1)
			for i := range c.ValidatorIndices {
				c.ValidatorIndices[i] = domain.ValidatorIndex(i)
			}
		}, "validator index count"},
		{"duplicate validator", func(c *WatchConfig) { c.ValidatorIndices = []domain.ValidatorIndex{24, 24} }, "more than once"},
		{"baseline metrics without beacon API", func(c *WatchConfig) {
			c.BaselineMetricsAPI = "http://127.0.0.1:6054/metrics"
		}, "name the node it belongs to"},
		// A baseline pointing back at the watched node would report local
		// lateness as network-wide lateness, exonerating a real local fault.
		{"baseline is the watched node", func(c *WatchConfig) {
			c.BaselineBeaconAPI = c.BeaconAPI
			c.BaselineMetricsAPI = "http://127.0.0.1:6054/metrics"
		}, "different node"},
		{"baseline trailing slash aliases watched node", func(c *WatchConfig) {
			c.BaselineBeaconAPI = c.BeaconAPI + "/"
			c.BaselineMetricsAPI = "http://127.0.0.1:6054/metrics"
		}, "different node"},
		{"baseline beacon API with credentials", func(c *WatchConfig) {
			c.BaselineBeaconAPI = "http://user:pass@127.0.0.1:6052"
			c.BaselineMetricsAPI = "http://127.0.0.1:6054/metrics"
		}, "credentials"},
	}

	if err := validWatchConfig().Validate(); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := validWatchConfig()
			tc.mutate(&cfg)
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.want)
			}
		})
	}
}

// A baseline beacon API on its own is now valid: without a metrics endpoint the
// baseline is measured from that node's own /eth/v1/beacon/headers/{slot}
// (ADR-0025), which is the whole point — an operator needs a node they can
// reach, not a second one they run and scrape.
func TestWatchConfigAcceptsBaselineWithoutMetrics(t *testing.T) {
	t.Parallel()
	cfg := validWatchConfig()
	cfg.BaselineBeaconAPI = "http://127.0.0.1:6052"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want a baseline beacon API alone to be accepted", err)
	}
}

func TestValidateBaselineGenesis(t *testing.T) {
	t.Parallel()
	watched := beaconapi.GenesisInfo{GenesisTime: time.Unix(1_700_000_000, 0).UTC(), SecondsPerSlot: 12 * time.Second}
	if err := validateBaselineGenesis(watched, watched); err != nil {
		t.Fatalf("matching genesis: %v", err)
	}
	other := watched
	other.GenesisTime = other.GenesisTime.Add(time.Second)
	if err := validateBaselineGenesis(watched, other); err == nil {
		t.Fatal("different genesis time was accepted")
	}
	other = watched
	other.SecondsPerSlot = 6 * time.Second
	if err := validateBaselineGenesis(watched, other); err == nil {
		t.Fatal("different slot duration was accepted")
	}
}

// TestWatch_BaselineFromBeaconAPIShutsDownCleanly covers the goroutine ADR-0025
// added, which nothing else does: every other BaselineBeaconAPI reference in this
// file is a config-validation case that never starts the daemon, so
// runNetworkBaselineFromAPI had no goleak coverage at all despite Phase 2's DoD
// requiring zero goroutine leaks. It polls the baseline node with a 15s deadline,
// so a missed context check here would hang a shutdown rather than fail loudly.
func TestWatch_BaselineFromBeaconAPIShutsDownCleanly(t *testing.T) {
	genesis := time.Now().Add(-time.Hour).Unix()
	chain := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/eth/v1/beacon/genesis":
			_, _ = fmt.Fprintf(w, `{"data":{"genesis_time":%q}}`, strconv.FormatInt(genesis, 10))
		case "/eth/v1/config/spec":
			_, _ = fmt.Fprint(w, `{"data":{"SECONDS_PER_SLOT":"12"}}`)
		case "/eth/v1/events":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			<-r.Context().Done()
		default:
			// Every header lookup 404s, so BlockSeen polls to its deadline and
			// only the context can end it — which is the case worth covering.
			http.NotFound(w, r)
		}
	}
	watched := httptest.NewServer(http.HandlerFunc(chain))
	defer watched.Close()
	baseline := httptest.NewServer(http.HandlerFunc(chain))
	defer baseline.Close()

	cfg := validWatchConfig()
	cfg.BeaconAPI = watched.URL
	cfg.BaselineBeaconAPI = baseline.URL // no metrics endpoint: the ADR-0025 path
	cfg.DBPath = filepath.Join(t.TempDir(), "whymiss.db")
	cfg.RetentionInterval = 0

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Watch(ctx, cfg) }()

	// Long enough for the baseline collector to be inside BlockSeen's poll loop.
	time.Sleep(1500 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Watch: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Watch did not stop the Beacon API baseline collector after cancellation")
	}
}

// TestWatch_EveryCollectorShutsDownCleanly is the goroutine-leak gate Phase 2's
// DoD actually asks for.
//
// validWatchConfig enables none of the optional collectors, so before this test
// the goleak check covered three of the daemon's nine goroutines: block timing,
// peer sampling, clock sampling, duty tracking, host sampling, and the metrics
// baseline all started only under configuration no test set. A leak in any of
// them would have shipped with the DoD item ticked. This turns every one of them
// on at once and asserts the whole daemon unwinds on cancellation.
func TestWatch_EveryCollectorShutsDownCleanly(t *testing.T) {
	genesisTime := time.Now().Add(-time.Hour).Unix()
	beacon := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/eth/v1/beacon/genesis":
			_, _ = fmt.Fprintf(w, `{"data":{"genesis_time":%q}}`, strconv.FormatInt(genesisTime, 10))
		case r.URL.Path == "/eth/v1/config/spec":
			_, _ = fmt.Fprint(w, `{"data":{"SECONDS_PER_SLOT":"12"}}`)
		case r.URL.Path == "/eth/v1/node/version":
			_, _ = fmt.Fprint(w, `{"data":{"version":"Lighthouse/v8.2.2-e423a66/x86_64-linux"}}`)
		case r.URL.Path == "/eth/v1/node/peer_count":
			_, _ = fmt.Fprint(w, `{"data":{"connected":"2","connecting":"0","disconnected":"0","disconnecting":"0"}}`)
		case r.URL.Path == "/eth/v1/events":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			<-r.Context().Done()
		case strings.HasPrefix(r.URL.Path, "/eth/v1/validator/duties/attester/"):
			_, _ = fmt.Fprint(w, `{"data":[]}`)
		default:
			http.NotFound(w, r)
		}
	}
	watched := httptest.NewServer(http.HandlerFunc(beacon))
	defer watched.Close()
	baselineNode := httptest.NewServer(http.HandlerFunc(beacon))
	defer baselineNode.Close()
	metrics := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "beacon_head_slot 100\nbeacon_block_delay_observed_slot_start 90\n")
	}))
	defer metrics.Close()

	cfg := validWatchConfig()
	cfg.BeaconAPI = watched.URL
	cfg.DBPath = filepath.Join(t.TempDir(), "whymiss.db")
	cfg.CLMetricsAPI = metrics.URL
	cfg.PeerSampleInterval = 12 * time.Second
	cfg.BaselineBeaconAPI = baselineNode.URL
	cfg.BaselineMetricsAPI = metrics.URL // exercises the metrics baseline, not ADR-0025's
	cfg.HostSampleInterval = 10 * time.Second
	// An address that refuses immediately keeps the clock sampler in its error
	// path without waiting on DNS or a real NTP round trip.
	cfg.NTPServers = []string{"127.0.0.1:1"}
	cfg.ClockSampleInterval = 30 * time.Second
	cfg.ValidatorIndices = []domain.ValidatorIndex{24}
	cfg.RetentionInterval = 0

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Watch(ctx, cfg) }()

	time.Sleep(1500 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Watch: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Watch did not unwind every collector after cancellation")
	}
}

// TestWatch_PeerSamplingRunsWithoutACLMetricsEndpoint pins the decoupling.
// Peer sampling reads /eth/v1/node/peer_count on the watched node's own Beacon
// API (ADR-0023), but it used to be started only inside the CLMetricsAPI branch
// — a leftover from when the count came from Prometheus. That denied R-200's
// peer corroboration to every operator running nothing but a beacon node, for a
// reason the measurement had stopped having.
func TestWatch_PeerSamplingRunsWithoutACLMetricsEndpoint(t *testing.T) {
	genesisTime := time.Now().Add(-time.Hour).Unix()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/eth/v1/beacon/genesis":
			_, _ = fmt.Fprintf(w, `{"data":{"genesis_time":%q}}`, strconv.FormatInt(genesisTime, 10))
		case "/eth/v1/config/spec":
			_, _ = fmt.Fprint(w, `{"data":{"SECONDS_PER_SLOT":"12"}}`)
		case "/eth/v1/node/peer_count":
			_, _ = fmt.Fprint(w, `{"data":{"connected":"37","disconnected":"2","connecting":"0","disconnecting":"0"}}`)
		case "/eth/v1/events":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := validWatchConfig()
	cfg.BeaconAPI = server.URL
	cfg.CLMetricsAPI = ""                    // the whole point: no metrics endpoint anywhere
	cfg.PeerSampleInterval = 5 * time.Second // the validated minimum
	cfg.DBPath = filepath.Join(t.TempDir(), "whymiss.db")
	cfg.RetentionInterval = 0

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Watch(ctx, cfg) }()

	st, err := waitForPeerSample(cfg.DBPath, 10*time.Second)
	cancel()
	if waitErr := <-done; waitErr != nil {
		t.Fatalf("Watch: %v", waitErr)
	}
	if err != nil {
		t.Fatalf("no peer count was sampled without a CL metrics endpoint: %v", err)
	}
	if st != 37 {
		t.Errorf("sampled peer count = %v, want 37", st)
	}
}

// waitForPeerSample polls the store for a peer-count sample until one appears or
// the budget runs out, so the test finishes as soon as the sample lands rather
// than always sleeping for the full interval.
func waitForPeerSample(dbPath string, budget time.Duration) (float64, error) {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		time.Sleep(250 * time.Millisecond)
		st, err := store.Open(context.Background(), dbPath)
		if err != nil {
			continue // the daemon may not have created it yet
		}
		samples, readErr := st.SamplesBetween(context.Background(),
			time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
		closeErr := st.Close()
		if readErr != nil || closeErr != nil {
			continue
		}
		for _, sample := range samples {
			if sample.Name == promscrape.MetricCLPeerCount {
				return sample.Value, nil
			}
		}
	}
	return 0, fmt.Errorf("no peer_count sample within %s", budget)
}
