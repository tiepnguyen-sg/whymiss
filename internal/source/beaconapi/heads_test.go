package beaconapi

import (
	"context"
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
