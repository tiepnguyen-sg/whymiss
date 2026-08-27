package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/store"
)

func TestDoctorRejectsIncompleteConfigWithoutNetworkIO(t *testing.T) {
	checks := Doctor(context.Background(), DoctorConfig{})
	if len(checks) != 1 || checks[0].Name != "config" || checks[0].Err == nil {
		t.Fatalf("checks = %+v, want one failed config check", checks)
	}
}

func TestCheckDBPath(t *testing.T) {
	dir := t.TempDir()
	newPath := filepath.Join(dir, "new.db")
	if err := checkDBPath(context.Background(), newPath); err != nil {
		t.Fatalf("new path: %v", err)
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatalf("doctor created database %s", newPath)
	}

	existing := filepath.Join(dir, "existing.db")
	st, err := store.Open(context.Background(), existing)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if err := checkDBPath(context.Background(), existing); err != nil {
		t.Fatalf("existing path: %v", err)
	}
	if err := checkDBPath(context.Background(), dir); err == nil {
		t.Error("directory path: want error")
	}

	corrupt := filepath.Join(dir, "corrupt.db")
	if err := os.WriteFile(corrupt, []byte("not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkDBPath(context.Background(), corrupt); err == nil {
		t.Error("corrupt database: want error")
	}
}

func TestDoctorRejectsInvalidBeaconURLBeforeNetworkIO(t *testing.T) {
	checks := Doctor(context.Background(), DoctorConfig{
		BeaconAPI:          "://invalid",
		DBPath:             filepath.Join(t.TempDir(), "whymiss.db"),
		MinRequestInterval: time.Millisecond,
		ClockOffsetMax:     100 * time.Millisecond,
	})
	if len(checks) != 1 || checks[0].Name != "config" || checks[0].Err == nil {
		t.Fatalf("checks = %+v, want one failed config check", checks)
	}
}

// TestCheckCLMetricsWarnsRatherThanFailsWhenUnset covers the distinction the
// whole change rests on. Doctor used to pass silently on this configuration,
// which let an operator finish setup believing attribution worked; it must now
// say what becomes unreportable. But an unset endpoint is a legitimate choice,
// not a misconfiguration, so it must not fail the command either.
func TestCheckCLMetricsWarnsRatherThanFailsWhenUnset(t *testing.T) {
	check := checkCLMetrics(context.Background(), DoctorConfig{}, "Prysm/v5.0.0")
	if check.Err != nil {
		t.Fatalf("unset --cl-metrics-api reported as a failure: %v", check.Err)
	}
	if !check.Warn {
		t.Error("unset --cl-metrics-api produced neither a warning nor an error")
	}
	for _, want := range []string{"--cl-metrics-api", "local.cl_slow", "network.late_block"} {
		if !strings.Contains(check.Detail, want) {
			t.Errorf("warning does not name %q: %s", want, check.Detail)
		}
	}
}

// A configured endpoint that cannot be scraped is the operator asking for
// something that is not there, which does block.
func TestCheckCLMetricsFailsOnAConfiguredEndpointThatDoesNotServeTheGauge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	check := checkCLMetrics(context.Background(), DoctorConfig{CLMetricsAPI: server.URL}, "Prysm/v5.0.0")
	if check.Err == nil {
		t.Fatalf("unreachable metrics endpoint passed: %+v", check)
	}
	if check.Warn {
		t.Error("a configured-but-broken endpoint was downgraded to a warning")
	}
}

func TestCheckCLMetricsReportsAWorkingEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "block_arrival_latency_milliseconds_gauge 412\nbeacon_head_slot 2574\n")
	}))
	defer server.Close()

	check := checkCLMetrics(context.Background(), DoctorConfig{CLMetricsAPI: server.URL}, "Prysm/v5.0.0")
	if check.Err != nil {
		t.Fatalf("checkCLMetrics: %v", check.Err)
	}
	if check.Warn {
		t.Error("a working endpoint produced a warning")
	}
	if !strings.Contains(check.Detail, "2574") {
		t.Errorf("detail does not report the scraped head slot: %s", check.Detail)
	}
}

// An unsupported client cannot be scraped even though the endpoint answers, and
// saying "scraped" there would be worse than saying nothing.
func TestCheckCLMetricsFailsOnAnUnsupportedClient(t *testing.T) {
	check := checkCLMetrics(context.Background(), DoctorConfig{CLMetricsAPI: "http://127.0.0.1:1"}, "Teku/v24.1.0")
	if check.Err == nil {
		t.Fatal("unsupported client passed the metrics check")
	}
	if !strings.Contains(check.Err.Error(), "Teku") {
		t.Errorf("error does not name the client: %v", check.Err)
	}
}

func TestCheckBaselineWarnsRatherThanFailsWhenUnset(t *testing.T) {
	checks := checkBaseline(context.Background(), DoctorConfig{})
	if len(checks) != 1 {
		t.Fatalf("checks = %+v, want one", checks)
	}
	if checks[0].Err != nil || !checks[0].Warn {
		t.Fatalf("unset baseline: err=%v warn=%v, want a warning", checks[0].Err, checks[0].Warn)
	}
	for _, want := range []string{"--baseline-beacon-api", "network.late_block", "local.p2p_degraded"} {
		if !strings.Contains(checks[0].Detail, want) {
			t.Errorf("warning does not name %q: %s", want, checks[0].Detail)
		}
	}
}

// Both of these are misconfigurations watchConfig.validate already rejects, so
// doctor has to reject them too — otherwise doctor passes and `watch` refuses to
// start, which is the opposite of what doctor is for.
func TestCheckBaselineRejectsWhatWatchWouldReject(t *testing.T) {
	t.Run("metrics endpoint with no beacon API to name its node", func(t *testing.T) {
		checks := checkBaseline(context.Background(), DoctorConfig{BaselineMetricsAPI: "http://127.0.0.1:5054"})
		if len(checks) != 1 || checks[0].Err == nil {
			t.Fatalf("checks = %+v, want one failure", checks)
		}
	})

	t.Run("the watched node offered as its own baseline", func(t *testing.T) {
		checks := checkBaseline(context.Background(), DoctorConfig{
			BeaconAPI:         "http://127.0.0.1:5052",
			BaselineBeaconAPI: "http://127.0.0.1:5052",
		})
		if len(checks) != 1 || checks[0].Err == nil {
			t.Fatalf("checks = %+v, want one failure", checks)
		}
		if !strings.Contains(checks[0].Err.Error(), "exonerate") {
			t.Errorf("error does not say why this is unsafe: %v", checks[0].Err)
		}
	})
}
