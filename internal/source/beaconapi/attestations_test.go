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

// TestAttestationPublishedRetriesThroughTransientFetchError guards the same
// regression class as heads_test.go's TestHeadUpdatedRetriesThroughTransientFetchError:
// AttestationPublished used to abort on the first failed pool poll instead of
// tolerating it like "not found yet" — found by inspection while investigating
// why local.vc_slow's own cgroup_cpu fault against Prysm kept bisecting straight
// from local.vc_disconnected to healthy with no clean local.vc_slow in between.
// A node under that exact CPU pressure can answer one poll too slowly without
// being unable to answer the next one 500ms later; losing that single poll must
// not make a real late publish look like the validator never attested at all.
func TestAttestationPublishedRetriesThroughTransientFetchError(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		data, err := os.ReadFile("testdata/pool_attestations.json")
		if err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data) //nolint:errcheck // test helper
	}))
	defer srv.Close()

	d := AttesterDuty{
		Duty:                    domain.Duty{Slot: 3619, ValidatorIndex: 7, CommitteeIndex: 0},
		ValidatorCommitteeIndex: 0,
	}
	obs, found, err := NewClient(srv.URL, 0).AttestationPublished(context.Background(), d, time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("AttestationPublished: %v", err)
	}
	if !found || obs.Kind != domain.ObsAttestationPublished {
		t.Fatalf("observation = %+v found=%t, want attestation_published after retrying past two failed polls", obs, found)
	}
	if got := requests.Load(); got < 3 {
		t.Fatalf("server saw %d requests, want at least 3 (it must have retried past the two failures)", got)
	}
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
