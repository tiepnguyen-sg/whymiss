package beaconapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
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
	if d.ValidatorIndex != 0 || d.Slot != 3627 || d.CommitteeIndex != 0 || d.ValidatorCommitteeIndex != 0 || d.CommitteeLength != 2 || d.CommitteesAtSlot != 1 {
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

func TestFetchAttesterDutiesRejectsUnboundedOrUntrustedAssignments(t *testing.T) {
	t.Parallel()

	t.Run("too many requested validators", func(t *testing.T) {
		t.Parallel()
		validators := make([]domain.ValidatorIndex, maxAttesterDutyValidators+1)
		if _, err := NewClient("http://127.0.0.1", 0).FetchAttesterDuties(t.Context(), testEpoch, validators); err == nil {
			t.Fatal("FetchAttesterDuties: want request size rejection")
		}
	})

	for _, tc := range []struct {
		name   string
		mutate func([]map[string]any)
	}{
		{"duplicate validator", func(data []map[string]any) { data[1]["validator_index"] = data[0]["validator_index"] }},
		{"unrequested validator", func(data []map[string]any) { data[0]["validator_index"] = "999" }},
		{"slot outside epoch", func(data []map[string]any) { data[0]["slot"] = "3650" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := serveMutatedDutyFixture(t, "duties_attester.json", tc.mutate)
			defer srv.Close()
			_, err := NewClient(srv.URL, 0).FetchAttesterDuties(t.Context(), testEpoch, []domain.ValidatorIndex{0, 1, 2, 3, 4})
			if err == nil {
				t.Fatal("FetchAttesterDuties: want invalid response rejection")
			}
		})
	}
}

func TestFetchProposerDutiesRejectsDuplicateSlot(t *testing.T) {
	t.Parallel()
	srv := serveMutatedDutyFixture(t, "duties_proposer.json", func(data []map[string]any) {
		data[1]["slot"] = data[0]["slot"]
	})
	defer srv.Close()
	if _, err := NewClient(srv.URL, 0).FetchProposerDuties(t.Context(), testEpoch); err == nil {
		t.Fatal("FetchProposerDuties: want duplicate slot rejection")
	}
}

func serveMutatedDutyFixture(t *testing.T, name string, mutate func([]map[string]any)) *httptest.Server {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	mutate(envelope.Data)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(envelope); err != nil {
			t.Errorf("encode mutated real fixture: %v", err)
		}
	}))
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
