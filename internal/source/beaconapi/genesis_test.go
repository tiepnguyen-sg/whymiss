package beaconapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// serveTestdata returns an httptest.Server that serves the exact bytes of
// testdata/<file> for path, and 404 for anything else — a real captured
// response replayed verbatim, never a hand-written one (BUILD_PROMPT.md §8).
func serveTestdata(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		data, err := os.ReadFile("testdata/" + file)
		if err != nil {
			t.Fatalf("read testdata/%s: %v", file, err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data) //nolint:errcheck // test helper; a write failure would fail the test via a mismatched response anyway
	}))
}

// TestFetchGenesis is captured against a real, live Kurtosis devnet
// (test/e2e/kurtosis) running Lighthouse — see testdata/genesis.json and
// testdata/spec.json.
func TestFetchGenesis(t *testing.T) {
	srv := serveTestdata(t, map[string]string{
		"/eth/v1/beacon/genesis": "genesis.json",
		"/eth/v1/config/spec":    "spec.json",
	})
	defer srv.Close()

	c := NewClient(srv.URL, 0)
	got, err := c.FetchGenesis(context.Background())
	if err != nil {
		t.Fatalf("FetchGenesis: %v", err)
	}

	wantTime := time.Unix(1787254828, 0).UTC()
	if !got.GenesisTime.Equal(wantTime) {
		t.Errorf("GenesisTime = %v, want %v", got.GenesisTime, wantTime)
	}
	if got.SecondsPerSlot != 12*time.Second {
		t.Errorf("SecondsPerSlot = %v, want 12s", got.SecondsPerSlot)
	}
}

func TestFetchGenesis_NotYetAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 0)
	if _, err := c.FetchGenesis(context.Background()); err == nil {
		t.Fatal("FetchGenesis: want error for a node with no genesis yet, got nil")
	}
}

func TestGenesisInfo_SlotStart(t *testing.T) {
	g := GenesisInfo{
		GenesisTime:    time.Unix(1787254828, 0).UTC(),
		SecondsPerSlot: 12 * time.Second,
	}
	got := g.SlotStart(10)
	want := time.Unix(1787254828+120, 0).UTC()
	if !got.Equal(want) {
		t.Errorf("SlotStart(10) = %v, want %v", got, want)
	}
}

// requireContains is a small assertion helper used by a couple of the
// package's error-path tests.
func requireContains(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), substr) {
		t.Errorf("error = %v, want it to contain %q", err, substr)
	}
}
