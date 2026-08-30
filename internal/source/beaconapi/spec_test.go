package beaconapi

import (
	"context"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// Both fixtures are real responses, recorded 2026-08-30: spec_gloas.json from a
// Glamsterdam devnet running EIP-7732, spec_pre_epbs.json from the public Hoodi
// gateway. They are the evidence for the trap this code exists to avoid.
func TestFetchSchedule(t *testing.T) {
	t.Parallel()

	t.Run("a post-ePBS chain yields the Gloas deadlines", func(t *testing.T) {
		t.Parallel()

		srv := serveTestdata(t, map[string]string{"/eth/v1/config/spec": "spec_gloas.json"})
		defer srv.Close()

		// GLOAS_FORK_EPOCH is 2 in the fixture and the chain has passed it.
		got, ok, err := NewClient(srv.URL, 0).FetchSchedule(context.Background(), 3)
		if err != nil || !ok {
			t.Fatalf("FetchSchedule = %v, %v, %v", got, ok, err)
		}
		want := domain.SlotSchedule{
			SecondsPerSlot:        12 * time.Second,
			AttestationDeadline:   3 * time.Second, // ATTESTATION_DUE_BPS_GLOAS 2500
			AggregationDeadline:   8 * time.Second, // AGGREGATE_DUE_BPS 6667
			PayloadRevealDeadline: 6 * time.Second, // PAYLOAD_DUE_BPS 5000
			PTCDeadline:           9 * time.Second, // PAYLOAD_ATTESTATION_DUE_BPS 7500
		}
		if got != want {
			t.Errorf("schedule = %+v, want %+v", got, want)
		}
	})

	t.Run("a pre-ePBS chain yields no payload deadlines despite publishing the keys", func(t *testing.T) {
		t.Parallel()

		srv := serveTestdata(t, map[string]string{"/eth/v1/config/spec": "spec_pre_epbs.json"})
		defer srv.Close()

		got, ok, err := NewClient(srv.URL, 0).FetchSchedule(context.Background(), 100000)
		if err != nil || !ok {
			t.Fatalf("FetchSchedule = %v, %v, %v", got, ok, err)
		}
		// The fixture carries PAYLOAD_DUE_BPS 7500 and ATTESTATION_DUE_BPS_GLOAS
		// 2500 even though GLOAS_FORK_EPOCH is the unscheduled sentinel. Reading
		// either would have produced a confident, wrong deadline here.
		if got.IsPostEPBS() {
			t.Errorf("pre-ePBS network reported as post-ePBS: %+v", got)
		}
		if got.PTCDeadline != 0 {
			t.Errorf("ptc deadline = %s, want zero", got.PTCDeadline)
		}
		if got.AttestationDeadline != 4*time.Second {
			t.Errorf("attestation deadline = %s, want 4s from ATTESTATION_DUE_BPS", got.AttestationDeadline)
		}
		if got != domain.MainnetPreEPBS() {
			t.Errorf("schedule = %+v, want the mainnet pre-ePBS schedule", got)
		}
	})

	t.Run("a scheduled fork the chain has not reached is still pre-ePBS", func(t *testing.T) {
		t.Parallel()

		srv := serveTestdata(t, map[string]string{"/eth/v1/config/spec": "spec_gloas.json"})
		defer srv.Close()

		got, ok, err := NewClient(srv.URL, 0).FetchSchedule(context.Background(), 1)
		if err != nil || !ok {
			t.Fatalf("FetchSchedule = %v, %v, %v", got, ok, err)
		}
		if got.IsPostEPBS() {
			t.Errorf("epoch 1 with GLOAS_FORK_EPOCH 2 reported as post-ePBS: %+v", got)
		}
	})
}

// Basis points are a fixed-point approximation of the deadlines docs/causes.md
// states exactly; without rounding, mainnet's 3333 bps would land 0.4ms early.
func TestDeadlineFromBPSLandsOnTheDocumentedConstants(t *testing.T) {
	t.Parallel()

	slot := 12 * time.Second
	for _, tc := range []struct {
		bps  uint64
		want time.Duration
	}{
		{3333, 4 * time.Second}, // ATTESTATION_DUE_BPS
		{6667, 8 * time.Second}, // AGGREGATE_DUE_BPS
		{2500, 3 * time.Second}, // ATTESTATION_DUE_BPS_GLOAS
		{5000, 6 * time.Second}, // PAYLOAD_DUE_BPS
		{7500, 9 * time.Second}, // PAYLOAD_ATTESTATION_DUE_BPS
		{10000, 12 * time.Second},
	} {
		if got := deadlineFromBPS(slot, tc.bps); got != tc.want {
			t.Errorf("deadlineFromBPS(12s, %d) = %s, want %s", tc.bps, got, tc.want)
		}
	}
}
