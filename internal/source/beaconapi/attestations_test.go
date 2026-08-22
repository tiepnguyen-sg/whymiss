package beaconapi

import (
	"context"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// TestAttestationPublished is captured against a real devnet's attestation
// pool: an attestation for slot 3619, committee 0, aggregation_bits "0x05"
// — binary 00000101, so bit 0 is set and bit 1 is not (bit 2 is the SSZ
// Bitlist length sentinel). See testdata/pool_attestations.json.
func TestAttestationPublished(t *testing.T) {
	srv := serveTestdata(t, map[string]string{
		"/eth/v1/beacon/pool/attestations": "pool_attestations.json",
	})
	defer srv.Close()

	c := NewClient(srv.URL, 0)

	t.Run("validator at the set bit is found published", func(t *testing.T) {
		d := AttesterDuty{
			Duty:                    domain.Duty{Slot: 3619, ValidatorIndex: 7, CommitteeIndex: 0},
			ValidatorCommitteeIndex: 0,
		}
		obs, found, err := c.AttestationPublished(context.Background(), d, time.Now().Add(time.Second))
		if err != nil {
			t.Fatalf("AttestationPublished: %v", err)
		}
		if !found {
			t.Fatal("AttestationPublished: want found, got false")
		}
		if obs.Kind != domain.ObsAttestationPublished {
			t.Errorf("Kind = %q, want %q", obs.Kind, domain.ObsAttestationPublished)
		}
		if got := obs.Attrs[domain.AttrValidatorIndex]; got != "7" {
			t.Errorf("validator_index = %q, want %q", got, "7")
		}
	})

	t.Run("validator at an unset bit is not found published", func(t *testing.T) {
		d := AttesterDuty{
			Duty:                    domain.Duty{Slot: 3619, ValidatorIndex: 8, CommitteeIndex: 0},
			ValidatorCommitteeIndex: 1,
		}
		_, found, err := c.AttestationPublished(context.Background(), d, time.Now().Add(50*time.Millisecond))
		if err != nil {
			t.Fatalf("AttestationPublished: %v", err)
		}
		if found {
			t.Fatal("AttestationPublished: want not found for an unset bit, got found")
		}
	})
}

func TestAttestationPublished_PoolGone(t *testing.T) {
	srv := serveTestdata(t, map[string]string{})
	defer srv.Close()

	c := NewClient(srv.URL, 0)
	d := AttesterDuty{Duty: domain.Duty{Slot: 1, CommitteeIndex: 0}}
	_, found, err := c.AttestationPublished(context.Background(), d, time.Now().Add(50*time.Millisecond))
	if err != nil {
		t.Fatalf("AttestationPublished: %v", err)
	}
	if found {
		t.Fatal("AttestationPublished: want not found when the pool 404s, got found")
	}
}
