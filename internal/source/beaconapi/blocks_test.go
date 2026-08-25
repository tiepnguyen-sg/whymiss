package beaconapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// Captured against a real devnet: slot 3631's block header — see
// testdata/block_header_by_slot.json.
func TestBlockSeen(t *testing.T) {
	srv := serveTestdata(t, map[string]string{
		"/eth/v1/beacon/headers/3631": "block_header_by_slot.json",
	})
	defer srv.Close()

	c := NewClient(srv.URL, 0)
	obs, found, err := c.BlockSeen(context.Background(), 3631, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("BlockSeen: %v", err)
	}
	if !found {
		t.Fatal("BlockSeen: want found, got false")
	}
	if obs.Kind != domain.ObsBlockSeen {
		t.Errorf("Kind = %q, want %q", obs.Kind, domain.ObsBlockSeen)
	}
	if got := obs.Attrs[domain.AttrProposerIndex]; got != "3" {
		t.Errorf("proposer_index = %q, want %q", got, "3")
	}
	if got := obs.Attrs[domain.AttrBlockRoot]; got != "0xdad5f67ca5b4f18937666c9e85e3622ad2d817ca6bf9cecc8bf554be3b870b73" {
		t.Errorf("block_root = %q, want the real captured root", got)
	}
}

// TestBlockSeenRetriesThroughTransientFetchError guards the same regression
// class as heads_test.go's TestHeadUpdatedRetriesThroughTransientFetchError
// and attestations_test.go's TestAttestationPublishedRetriesThroughTransientFetchError.
func TestBlockSeenRetriesThroughTransientFetchError(t *testing.T) {
	fixture, err := os.ReadFile("testdata/block_header_by_slot.json")
	if err != nil {
		t.Fatal(err)
	}
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(fixture) //nolint:errcheck // test helper
	}))
	defer srv.Close()

	obs, found, err := NewClient(srv.URL, 0).BlockSeen(context.Background(), 3631, time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("BlockSeen: %v", err)
	}
	if !found || obs.Kind != domain.ObsBlockSeen {
		t.Fatalf("observation = %+v found=%t, want block_seen after retrying past two failed polls", obs, found)
	}
	if requests < 3 {
		t.Fatalf("server saw %d requests, want at least 3 (it must have retried past the two failures)", requests)
	}
}

// The syncing fixture was captured from the real Prysm devnet at head slot 789.
// With no header at 788, a fully synced node already past that slot can confirm
// the canonical slot was skipped.
func TestBlockSeen_ConfirmsSkippedSlot(t *testing.T) {
	srv := serveTestdata(t, map[string]string{
		"/eth/v1/node/syncing": "node_syncing.json",
	})
	defer srv.Close()

	c := NewClient(srv.URL, 0)
	obs, found, err := c.BlockSeen(context.Background(), 788, time.Now().Add(50*time.Millisecond))
	if err != nil {
		t.Fatalf("BlockSeen: %v", err)
	}
	if !found {
		t.Fatal("BlockSeen: want a confirmed skipped slot, got found=false")
	}
	if obs.Kind != domain.ObsBlockSkipped {
		t.Fatalf("Kind = %q, want %q", obs.Kind, domain.ObsBlockSkipped)
	}
	if obs.Source != domain.SourceBeaconAPI {
		t.Errorf("Source = %q, want %q", obs.Source, domain.SourceBeaconAPI)
	}
}

func TestBlockSeen_DoesNotCallCurrentHeadSkipped(t *testing.T) {
	srv := serveTestdata(t, map[string]string{
		"/eth/v1/node/syncing": "node_syncing.json",
	})
	defer srv.Close()

	// A budget this small still exercises the give-up path: the fixture
	// reports the same not-yet-past-slot status every time, so the loop
	// never sees recovery and the deadline has already passed.
	c := NewClient(srv.URL, 0)
	c.blockRecoveryBudget = time.Nanosecond
	_, found, err := c.BlockSeen(context.Background(), 789, time.Now().Add(50*time.Millisecond))
	if found {
		t.Fatal("BlockSeen: current head slot is not positive skipped-slot evidence")
	}
	if err == nil {
		t.Fatal("BlockSeen: inconclusive current-head status must prevent collection completion")
	}
}

// TestBlockSeen_WaitsForNodeRecovery guards a real regression: blockStatusAtDeadline
// used to sample the node's sync status exactly once and fail permanently if it
// wasn't ready that instant, discarding the slot's evidence even when the node
// was only transiently lagging (e.g. right after the network fault or load spike
// that made BlockSeen's own poll miss its deadline in the first place). Found
// live against the real public Hoodi gateway during the Phase 2 soak test.
func TestBlockSeen_WaitsForNodeRecovery(t *testing.T) {
	var statusReads int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/eth/v1/node/syncing" {
			http.NotFound(w, r)
			return
		}
		statusReads++
		w.Header().Set("Content-Type", "application/json")
		if statusReads == 1 {
			w.Write([]byte(`{"data":{"head_slot":"790","sync_distance":"0","is_syncing":false,"is_optimistic":false,"el_offline":true}}`)) //nolint:errcheck // test helper
			return
		}
		w.Write([]byte(`{"data":{"head_slot":"790","sync_distance":"0","is_syncing":false,"is_optimistic":false,"el_offline":false}}`)) //nolint:errcheck // test helper
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 0)
	c.blockRecoveryBudget = time.Minute
	obs, found, err := c.BlockSeen(context.Background(), 788, time.Now().Add(-time.Second))
	if err != nil {
		t.Fatalf("BlockSeen: %v", err)
	}
	if !found || obs.Kind != domain.ObsBlockSkipped {
		t.Fatalf("observation = %+v found=%t, want a confirmed skipped slot after the node recovered", obs, found)
	}
	if statusReads < 2 {
		t.Fatalf("status reads = %d, want the degraded state to be retried", statusReads)
	}
}

// TestCheckInclusion is captured against the real GCP devnet: slot 651's block
// contains one attestation for slot 650, committee 0. The two header fixtures
// prove the voted head and target are canonical.
func TestCheckInclusion(t *testing.T) {
	srv := serveTestdata(t, map[string]string{
		"/eth/v2/beacon/blocks/651/attestations": "block_attestations.json",
		"/eth/v1/beacon/headers/650":             "block_header_650.json",
		"/eth/v1/beacon/headers/640":             "block_header_640.json",
		"/eth/v1/beacon/headers/head":            "block_header_by_slot.json",
	})
	defer srv.Close()

	c := NewClient(srv.URL, 0)

	t.Run("validator at the set bit is found included", func(t *testing.T) {
		d := AttesterDuty{
			Duty:                    domain.Duty{Slot: 650, ValidatorIndex: 41, CommitteeIndex: 0},
			ValidatorCommitteeIndex: 1,
			CommitteeLength:         2,
			CommitteesAtSlot:        1,
		}
		obs, found, err := c.CheckInclusion(context.Background(), 650, d, 651, time.Now().Add(time.Second))
		if err != nil {
			t.Fatalf("CheckInclusion: %v", err)
		}
		if !found {
			t.Fatal("CheckInclusion: want found, got false")
		}
		if got := obs.Attrs[domain.AttrInclusionDelay]; got != "1" {
			t.Errorf("inclusion_delay = %q, want %q", got, "1")
		}
		if got := obs.Attrs[domain.AttrHeadCorrect]; got != "true" {
			t.Errorf("head_correct = %q, want true", got)
		}
		if got := obs.Attrs[domain.AttrTargetCorrect]; got != "true" {
			t.Errorf("target_correct = %q, want true", got)
		}
	})

	t.Run("validator at an unset bit is not found included", func(t *testing.T) {
		d := AttesterDuty{
			Duty:                    domain.Duty{Slot: 650, ValidatorIndex: 99, CommitteeIndex: 1},
			ValidatorCommitteeIndex: 0,
			CommitteeLength:         2,
			CommitteesAtSlot:        2,
		}
		_, found, err := c.CheckInclusion(context.Background(), 650, d, 651, time.Now().Add(50*time.Millisecond))
		if err != nil {
			t.Fatalf("CheckInclusion: %v", err)
		}
		if found {
			t.Fatal("CheckInclusion: want not found for an unset bit, got found")
		}
	})
}

func validBlockAttestation() blockAttestation {
	var att blockAttestation
	att.AggregationBits = "0x03"
	att.Data.Slot = "650"
	att.Data.Index = "0"
	att.Data.BeaconBlockRoot = "0x" + strings.Repeat("1", 64)
	att.Data.Target.Epoch = "20"
	att.Data.Target.Root = "0x" + strings.Repeat("2", 64)
	return att
}

func TestValidateBlockAttestations(t *testing.T) {
	t.Run("accepts bounded pre-Electra and Electra forms", func(t *testing.T) {
		pre := validBlockAttestation()
		electra := validBlockAttestation()
		electra.CommitteeBits = "0x0100000000000000"
		if err := validateBlockAttestations([]blockAttestation{pre}); err != nil {
			t.Fatalf("pre-Electra: %v", err)
		}
		if err := validateBlockAttestations([]blockAttestation{electra}); err != nil {
			t.Fatalf("Electra: %v", err)
		}
	})

	tests := []struct {
		name string
		atts func() []blockAttestation
	}{
		{"too many pre-Electra attestations", func() []blockAttestation {
			return makeFilledAttestations(maxPreElectraAttestations+1, validBlockAttestation())
		}},
		{"too many Electra attestations", func() []blockAttestation {
			att := validBlockAttestation()
			att.CommitteeBits = "0x0100000000000000"
			return makeFilledAttestations(maxElectraAttestations+1, att)
		}},
		{"mixed fork forms", func() []blockAttestation {
			pre, electra := validBlockAttestation(), validBlockAttestation()
			electra.CommitteeBits = "0x0100000000000000"
			return []blockAttestation{pre, electra}
		}},
		{"invalid numeric field", func() []blockAttestation {
			att := validBlockAttestation()
			att.Data.Slot = "-1"
			return []blockAttestation{att}
		}},
		{"invalid root", func() []blockAttestation {
			att := validBlockAttestation()
			att.Data.Target.Root = "0x01"
			return []blockAttestation{att}
		}},
		{"zero-length aggregation bitlist", func() []blockAttestation {
			att := validBlockAttestation()
			att.AggregationBits = "0x01"
			return []blockAttestation{att}
		}},
		{"oversized aggregation bitlist", func() []blockAttestation {
			att := validBlockAttestation()
			att.AggregationBits = "0x" + strings.Repeat("ff", (maxValidatorsPerCommittee+1+7)/8)
			return []blockAttestation{att}
		}},
		{"Electra nonzero data index", func() []blockAttestation {
			att := validBlockAttestation()
			att.CommitteeBits = "0x0100000000000000"
			att.Data.Index = "1"
			return []blockAttestation{att}
		}},
		{"malformed committee bits", func() []blockAttestation {
			att := validBlockAttestation()
			att.CommitteeBits = "0xzz00000000000000"
			return []blockAttestation{att}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateBlockAttestations(tc.atts()); err == nil {
				t.Fatal("validateBlockAttestations accepted malformed input")
			}
		})
	}
}

func makeFilledAttestations(count int, att blockAttestation) []blockAttestation {
	atts := make([]blockAttestation, count)
	for i := range atts {
		atts[i] = att
	}
	return atts
}

func TestFetchBlockBody_LegacyFallback(t *testing.T) {
	srv := serveTestdata(t, map[string]string{
		"/eth/v2/beacon/blocks/3631": "block.json",
	})
	defer srv.Close()

	c := NewClient(srv.URL, 0)
	atts, found, err := c.fetchBlockBody(context.Background(), 3631)
	if err != nil || !found || len(atts) == 0 {
		t.Fatalf("fetchBlockBody: attestations=%d found=%t err=%v", len(atts), found, err)
	}
}
