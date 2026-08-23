package main

import (
	"testing"
	"time"

	appconfig "github.com/tiepnguyen-sg/whymiss/internal/config"
)

func TestApplyConfigFlagsPreservesExplicitFlag(t *testing.T) {
	t.Parallel()
	root := newRootCmd()
	watch, _, err := root.Find([]string{"watch"})
	if err != nil {
		t.Fatal(err)
	}
	if err := watch.Flags().Set("retention-max-age", "48h"); err != nil {
		t.Fatal(err)
	}

	cfg := appconfig.Default()
	cfg.DBPath = "configured.db"
	cfg.Watch.RetentionMaxAge = 72 * time.Hour
	if err := applyConfigFlags(watch, cfg); err != nil {
		t.Fatalf("applyConfigFlags: %v", err)
	}

	if got := watch.Flag("db").Value.String(); got != "configured.db" {
		t.Errorf("db = %q, want configured.db", got)
	}
	if got := watch.Flag("retention-max-age").Value.String(); got != "48h0m0s" {
		t.Errorf("retention max age = %q, want explicit flag value", got)
	}
}

func TestApplyConfigFlagsSetsSliceValues(t *testing.T) {
	t.Parallel()
	root := newRootCmd()
	watch, _, err := root.Find([]string{"watch"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := appconfig.Default()
	cfg.Watch.NTPServers = []string{"ntp1.example", "ntp2.example"}
	cfg.Watch.ValidatorIndices = []uint64{24, 40}
	if err := applyConfigFlags(watch, cfg); err != nil {
		t.Fatalf("applyConfigFlags: %v", err)
	}

	servers, err := watch.Flags().GetStringSlice("ntp-server")
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 2 || servers[0] != "ntp1.example" || servers[1] != "ntp2.example" {
		t.Fatalf("NTP servers = %v", servers)
	}
	indices, err := watch.Flags().GetUintSlice("validator-index")
	if err != nil {
		t.Fatal(err)
	}
	if len(indices) != 2 || indices[0] != 24 || indices[1] != 40 {
		t.Fatalf("validator indices = %v", indices)
	}
}

func TestApplyConfigFlagsSetsBaselineValues(t *testing.T) {
	t.Parallel()
	root := newRootCmd()
	watch, _, err := root.Find([]string{"watch"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := appconfig.Default()
	cfg.Watch.BaselineBeaconAPI = "http://127.0.0.1:6052"
	cfg.Watch.BaselineMetricsAPI = "http://127.0.0.1:6054/metrics"
	if err := applyConfigFlags(watch, cfg); err != nil {
		t.Fatalf("applyConfigFlags: %v", err)
	}
	if got := watch.Flag("baseline-beacon-api").Value.String(); got != cfg.Watch.BaselineBeaconAPI {
		t.Fatalf("baseline beacon API = %q", got)
	}
	if got := watch.Flag("baseline-metrics-api").Value.String(); got != cfg.Watch.BaselineMetricsAPI {
		t.Fatalf("baseline metrics API = %q", got)
	}
}

func TestRequestedValidator(t *testing.T) {
	if got, err := requestedValidator(nil); err != nil || got != nil {
		t.Fatalf("empty selector = %v, %v", got, err)
	}
	if got, err := requestedValidator([]uint{24}); err != nil || got == nil || *got != 24 {
		t.Fatalf("single selector = %v, %v", got, err)
	}
	if _, err := requestedValidator([]uint{24, 40}); err == nil {
		t.Fatal("multiple selectors were accepted")
	}
}
