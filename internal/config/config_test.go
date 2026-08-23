package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadPrecedence(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "whymiss.yaml")
	data := []byte(`beacon_api: http://file.example:5052
db: file.db
watch:
  min_request_interval: 300ms
  ntp_servers: [ntp1.example, ntp2.example]
  validator_indices: [24, 40]
schedule:
  seconds_per_slot: 6s
  attestation_deadline: 2s
  aggregation_deadline: 4s
thresholds:
  dominance: 0.6
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		"WHYMISS_BEACON_API":           "http://env.example:5052",
		"WHYMISS_MIN_REQUEST_INTERVAL": "400ms",
		"WHYMISS_VALIDATOR_INDICES":    "1,2,3",
	}
	cfg, err := Load(path, func(key string) (string, bool) { value, ok := env[key]; return value, ok })
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BeaconAPI != env["WHYMISS_BEACON_API"] || cfg.DBPath != "file.db" {
		t.Errorf("global config = %+v", cfg)
	}
	if cfg.Watch.MinRequestInterval != 400*time.Millisecond || len(cfg.Watch.ValidatorIndices) != 3 {
		t.Errorf("watch config = %+v", cfg.Watch)
	}
	if cfg.Schedule.SecondsPerSlot != 6*time.Second || cfg.RCA.Dominance != 0.6 {
		t.Errorf("schedule/RCA = %+v / %+v", cfg.Schedule, cfg.RCA)
	}
}

func TestLoadRejectsUnknownYAMLKey(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "whymiss.yaml")
	if err := os.WriteFile(path, []byte("telemetry: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, func(string) (string, bool) { return "", false }); err == nil {
		t.Fatal("Load: want unknown-key error")
	}
}

func TestLoadRejectsMalformedEnvironment(t *testing.T) {
	t.Parallel()
	if _, err := Load("", func(key string) (string, bool) {
		if key == "WHYMISS_VALIDATOR_INDICES" {
			return "24,invalid", true
		}
		return "", false
	}); err == nil {
		t.Fatal("Load: want malformed validator error")
	}
}

func TestLoadAcceptsSystemdEnvironment(t *testing.T) {
	t.Parallel()
	env := map[string]string{
		"WHYMISS_BEACON_API":           "http://127.0.0.1:5052",
		"WHYMISS_CL_METRICS_API":       "http://127.0.0.1:5054/metrics",
		"WHYMISS_BASELINE_BEACON_API":  "",
		"WHYMISS_BASELINE_METRICS_API": "",
		"WHYMISS_VALIDATOR_INDICES":    "24,187",
		"WHYMISS_NTP_SERVERS":          "pool.ntp.org",
		"WHYMISS_METRICS_ADDR":         "127.0.0.1:9101",
	}
	cfg, err := Load("", func(key string) (string, bool) { value, ok := env[key]; return value, ok })
	if err != nil {
		t.Fatalf("Load systemd environment: %v", err)
	}
	if cfg.BeaconAPI != env["WHYMISS_BEACON_API"] || cfg.Watch.CLMetricsAPI != env["WHYMISS_CL_METRICS_API"] {
		t.Fatalf("endpoints = %q / %q", cfg.BeaconAPI, cfg.Watch.CLMetricsAPI)
	}
	if len(cfg.Watch.ValidatorIndices) != 2 || len(cfg.Watch.NTPServers) != 1 || cfg.Watch.MetricsAddr != env["WHYMISS_METRICS_ADDR"] {
		t.Fatalf("watch config = %+v", cfg.Watch)
	}
}

func TestLoadRejectsMultipleYAMLDocuments(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "whymiss.yaml")
	if err := os.WriteFile(path, []byte("db: first.db\n---\ndb: second.db\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, func(string) (string, bool) { return "", false }); err == nil {
		t.Fatal("Load: want multiple-document error")
	}
}

func TestLoadRejectsDuplicateYAMLKeys(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "whymiss.yaml")
	if err := os.WriteFile(path, []byte("db: first.db\ndb: second.db\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, func(string) (string, bool) { return "", false }); err == nil {
		t.Fatal("Load: want duplicate-key error")
	}
}

func TestLoadRejectsOversizedConfig(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "whymiss.yaml")
	if err := os.WriteFile(path, []byte(strings.Repeat("#", maxConfigBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, func(string) (string, bool) { return "", false }); err == nil {
		t.Fatal("Load: want oversized-file error")
	}
}
