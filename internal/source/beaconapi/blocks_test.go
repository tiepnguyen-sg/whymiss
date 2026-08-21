package beaconapi

import (
	"context"
	"testing"
	"time"

	"github.com/CHANGEME/whymiss/internal/domain"
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

func TestBlockSeen_NeverAppears(t *testing.T) {
	srv := serveTestdata(t, map[string]string{})
	defer srv.Close()

	c := NewClient(srv.URL, 0)
	_, found, err := c.BlockSeen(context.Background(), 9999999, time.Now().Add(50*time.Millisecond))
	if err != nil {
		t.Fatalf("BlockSeen: %v", err)
	}
	if found {
		t.Fatal("BlockSeen: want not found before deadline, got found")
	}
}

// TestCheckInclusion is captured against a real devnet: slot 3631's block
// body contains one attestation for slot 3630, committee 0, with
// aggregation_bits "0x06" — binary 00000110, so bit 1 is the only set data
// bit (bit 2 is the SSZ Bitlist length sentinel). See testdata/block.json.
func TestCheckInclusion(t *testing.T) {
	srv := serveTestdata(t, map[string]string{
		"/eth/v2/beacon/blocks/3631": "block.json",
	})
	defer srv.Close()

	c := NewClient(srv.URL, 0)

	t.Run("validator at the set bit is found included", func(t *testing.T) {
		d := AttesterDuty{
			Duty:                    domain.Duty{Slot: 3630, ValidatorIndex: 1, CommitteeIndex: 0},
			ValidatorCommitteeIndex: 1,
		}
		obs, found, err := c.CheckInclusion(context.Background(), 3630, d, 3631, time.Now().Add(time.Second))
		if err != nil {
			t.Fatalf("CheckInclusion: %v", err)
		}
		if !found {
			t.Fatal("CheckInclusion: want found, got false")
		}
		if got := obs.Attrs[domain.AttrInclusionDelay]; got != "1" {
			t.Errorf("inclusion_delay = %q, want %q", got, "1")
		}
	})

	t.Run("validator at an unset bit is not found included", func(t *testing.T) {
		d := AttesterDuty{
			Duty:                    domain.Duty{Slot: 3630, ValidatorIndex: 99, CommitteeIndex: 0},
			ValidatorCommitteeIndex: 0,
		}
		_, found, err := c.CheckInclusion(context.Background(), 3630, d, 3631, time.Now().Add(50*time.Millisecond))
		if err != nil {
			t.Fatalf("CheckInclusion: %v", err)
		}
		if found {
			t.Fatal("CheckInclusion: want not found for an unset bit, got found")
		}
	})
}
