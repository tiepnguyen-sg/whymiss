package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
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

// TestLoadSwitchesToPostEPBSTimingByConfigurationAlone is the test BUILD_PROMPT
// task 5.4 asks for: a fork that moves the timing model must be a configuration
// change, not a code change.
//
// It loads the same binary twice. With no schedule block the result is the
// pre-ePBS mainnet default and carries no payload deadline at all; with a
// schedule block naming the two ePBS deadlines the result is a post-ePBS
// schedule whose deadlines resolve against the slot start. Nothing between the
// two runs differs but the file.
//
// The durations here are illustrative, not spec constants. The point being
// proven is that the values come from configuration — which is exactly why
// internal/domain ships no post-ePBS default for a test to assert against.
func TestLoadSwitchesToPostEPBSTimingByConfigurationAlone(t *testing.T) {
	t.Parallel()

	noEnv := func(string) (string, bool) { return "", false }

	preEPBS, err := Load("", noEnv)
	if err != nil {
		t.Fatalf("Load with no configuration: %v", err)
	}
	if preEPBS.Schedule.IsPostEPBS() {
		t.Fatal("the default schedule reports itself as post-ePBS")
	}
	if preEPBS.Schedule != domain.MainnetPreEPBS() {
		t.Errorf("default schedule = %+v, want %+v", preEPBS.Schedule, domain.MainnetPreEPBS())
	}

	path := filepath.Join(t.TempDir(), "whymiss.yaml")
	data := []byte(`schedule:
  seconds_per_slot: 12s
  attestation_deadline: 3s
  aggregation_deadline: 8s
  payload_reveal_deadline: 6s
  ptc_deadline: 9s
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	postEPBS, err := Load(path, noEnv)
	if err != nil {
		t.Fatalf("Load with a post-ePBS schedule: %v", err)
	}
	if !postEPBS.Schedule.IsPostEPBS() {
		t.Fatal("a schedule configured with a payload-reveal deadline does not report itself as post-ePBS")
	}
	if got, want := postEPBS.Schedule.PayloadRevealDeadline, 6*time.Second; got != want {
		t.Errorf("payload_reveal_deadline = %s, want %s", got, want)
	}
	if got, want := postEPBS.Schedule.PTCDeadline, 9*time.Second; got != want {
		t.Errorf("ptc_deadline = %s, want %s", got, want)
	}

	slotStart := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	if got, ok := postEPBS.Schedule.PayloadRevealDeadlineAt(slotStart); !ok || !got.Equal(slotStart.Add(6*time.Second)) {
		t.Errorf("PayloadRevealDeadlineAt() = %v, %v", got, ok)
	}
	if got, ok := postEPBS.Schedule.PTCDeadlineAt(slotStart); !ok || !got.Equal(slotStart.Add(9*time.Second)) {
		t.Errorf("PTCDeadlineAt() = %v, %v", got, ok)
	}
}

// A half-configured ePBS schedule must fail at load. Reaching the rules with a
// PTC deadline and no payload deadline would mean attributing lateness against a
// boundary nobody set.
func TestLoadRejectsAPTCDeadlineWithoutAPayloadDeadline(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "whymiss.yaml")
	data := []byte(`schedule:
  ptc_deadline: 9s
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, func(string) (string, bool) { return "", false }); err == nil {
		t.Fatal("Load accepted a ptc_deadline with no payload_reveal_deadline")
	}
}

// The environment reaches these two the same way it reaches every other setting.
func TestLoadReadsEPBSDeadlinesFromTheEnvironment(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"WHYMISS_PAYLOAD_REVEAL_DEADLINE": "7s",
		"WHYMISS_PTC_DEADLINE":            "10s",
	}
	cfg, err := Load("", func(key string) (string, bool) { value, ok := env[key]; return value, ok })
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.Schedule.PayloadRevealDeadline, 7*time.Second; got != want {
		t.Errorf("payload_reveal_deadline = %s, want %s", got, want)
	}
	if got, want := cfg.Schedule.PTCDeadline, 10*time.Second; got != want {
		t.Errorf("ptc_deadline = %s, want %s", got, want)
	}
}
