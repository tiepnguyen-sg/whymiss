package beaconapi

import (
	"context"
	"testing"

	"github.com/CHANGEME/whymiss/internal/domain"
)

// Captured against a real devnet at epoch 113 — see
// testdata/duties_attester.json and testdata/duties_proposer.json.
const testEpoch = domain.Epoch(113)

func TestFetchAttesterDuties(t *testing.T) {
	srv := serveTestdata(t, map[string]string{
		"/eth/v1/validator/duties/attester/113": "duties_attester.json",
	})
	defer srv.Close()

	c := NewClient(srv.URL, 0)
	got, err := c.FetchAttesterDuties(context.Background(), testEpoch, []domain.ValidatorIndex{0, 1, 2, 3, 4})
	if err != nil {
		t.Fatalf("FetchAttesterDuties: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d duties, want 5", len(got))
	}

	// validator_index 0: slot 3627, committee_index 0, validator_committee_index 0.
	d := got[0]
	if d.ValidatorIndex != 0 || d.Slot != 3627 || d.CommitteeIndex != 0 || d.ValidatorCommitteeIndex != 0 {
		t.Errorf("duty[0] = %+v, want validator 0 at slot 3627, committee 0, position 0", d)
	}
	if d.Kind != domain.DutyAttester {
		t.Errorf("duty[0].Kind = %q, want %q", d.Kind, domain.DutyAttester)
	}

	// validator_index 2: slot 3632, validator_committee_index 1.
	d = got[2]
	if d.ValidatorIndex != 2 || d.Slot != 3632 || d.ValidatorCommitteeIndex != 1 {
		t.Errorf("duty[2] = %+v, want validator 2 at slot 3632, position 1", d)
	}
}

func TestFetchProposerDuties(t *testing.T) {
	srv := serveTestdata(t, map[string]string{
		"/eth/v1/validator/duties/proposer/113": "duties_proposer.json",
	})
	defer srv.Close()

	c := NewClient(srv.URL, 0)
	got, err := c.FetchProposerDuties(context.Background(), testEpoch)
	if err != nil {
		t.Fatalf("FetchProposerDuties: %v", err)
	}
	if len(got) != 32 {
		t.Fatalf("got %d proposer duties, want 32 (one per slot in the epoch)", len(got))
	}

	// First entry: validator 62 proposes slot 3616.
	if got[0].ValidatorIndex != 62 || got[0].Slot != 3616 || got[0].Kind != domain.DutyProposer {
		t.Errorf("duty[0] = %+v, want validator 62 at slot 3616, kind proposer", got[0])
	}
}

func TestFetchAttesterDuties_NotFound(t *testing.T) {
	srv := serveTestdata(t, map[string]string{})
	defer srv.Close()

	c := NewClient(srv.URL, 0)
	_, err := c.FetchAttesterDuties(context.Background(), testEpoch, []domain.ValidatorIndex{0})
	requireContains(t, err, "not found")
}
