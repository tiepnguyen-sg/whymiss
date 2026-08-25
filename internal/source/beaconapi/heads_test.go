package beaconapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func TestHeadUpdated(t *testing.T) {
	srv := serveTestdata(t, map[string]string{
		"/eth/v1/beacon/headers/head": "block_header_by_slot.json",
	})
	defer srv.Close()

	obs, found, err := NewClient(srv.URL, 0).HeadUpdated(context.Background(), 3631, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("HeadUpdated: %v", err)
	}
	if !found || obs.Kind != domain.ObsHeadUpdated || obs.Slot != 3631 {
		t.Fatalf("observation = %+v found=%t, want head_updated for slot 3631", obs, found)
	}
}

// TestHeadUpdatedRetriesThroughTransientFetchError guards a real regression:
// headUpdatedUncached used to abort on the first failed poll instead of
// tolerating it the same way it already tolerates "not found yet" — found
// running local.cl_slow's own cgroup_cpu fault against a real node, whose
// REST API occasionally answers too slowly under that exact CPU pressure
// ("timeout awaiting response headers"), aborting the whole call and losing
// the head_updated observation the corpus scenario needed. A single failed
// poll must not give up before the deadline as long as ctx itself is fine.
func TestHeadUpdatedRetriesThroughTransientFetchError(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		data, err := os.ReadFile("testdata/block_header_by_slot.json")
		if err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data) //nolint:errcheck // test helper
	}))
	defer srv.Close()

	obs, found, err := NewClient(srv.URL, 0).HeadUpdated(context.Background(), 3631, time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("HeadUpdated: %v", err)
	}
	if !found || obs.Kind != domain.ObsHeadUpdated || obs.Slot != 3631 {
		t.Fatalf("observation = %+v found=%t, want head_updated for slot 3631 after retrying past two failed polls", obs, found)
	}
	if got := requests.Load(); got < 3 {
		t.Fatalf("server saw %d requests, want at least 3 (it must have retried past the two failures)", got)
	}
}

func TestHeadUpdatedDoesNotBackdateMissedTransition(t *testing.T) {
	srv := serveTestdata(t, map[string]string{
		"/eth/v1/beacon/headers/head": "block_header_by_slot.json",
	})
	defer srv.Close()

	_, found, err := NewClient(srv.URL, 0).HeadUpdated(context.Background(), 3630, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("HeadUpdated: %v", err)
	}
	if found {
		t.Fatal("HeadUpdated invented a timestamp after the head had already advanced")
	}
}
