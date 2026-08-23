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
)

func validWatchConfig() WatchConfig {
	return WatchConfig{
		BeaconAPI:          "http://127.0.0.1:5052",
		DBPath:             "whymiss.db",
		MinRequestInterval: 200 * time.Millisecond,
		RetentionMaxAge:    14 * 24 * time.Hour,
		RetentionMaxBytes:  1 << 30,
		RetentionInterval:  time.Hour,
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
		{"zero peer interval", func(c *WatchConfig) { c.CLMetricsAPI = "http://127.0.0.1:5054/metrics" }, "peer sample interval"},
		{"zero clock interval", func(c *WatchConfig) { c.NTPServers = []string{"ntp.example"} }, "clock sample interval"},
		{"negative retention interval", func(c *WatchConfig) { c.RetentionInterval = -time.Second }, "retention interval"},
		{"zero retention age", func(c *WatchConfig) { c.RetentionMaxAge = 0 }, "retention max age"},
		{"zero retention bytes", func(c *WatchConfig) { c.RetentionMaxBytes = 0 }, "retention max bytes"},
		{"request interval too small", func(c *WatchConfig) { c.MinRequestInterval = time.Millisecond }, "request interval"},
		{"host interval too small", func(c *WatchConfig) { c.HostSampleInterval = time.Second }, "host sample interval"},
		{"peer interval too large", func(c *WatchConfig) {
			c.CLMetricsAPI = "http://127.0.0.1:5054/metrics"
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
		{"baseline beacon API without metrics", func(c *WatchConfig) {
			c.BaselineBeaconAPI = "http://127.0.0.1:6052"
		}, "must be set together"},
		{"baseline metrics without beacon API", func(c *WatchConfig) {
			c.BaselineMetricsAPI = "http://127.0.0.1:6054/metrics"
		}, "must be set together"},
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
