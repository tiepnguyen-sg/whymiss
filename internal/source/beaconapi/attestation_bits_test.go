package beaconapi

import (
	"context"
	"testing"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func TestAttestationIncludesValidatorElectra(t *testing.T) {
	t.Run("single committee", func(t *testing.T) {
		att := apiAttestation{
			AggregationBits: "0x06",
			CommitteeBits:   "0x0100000000000000",
		}
		att.Data.Index = "0"
		duty := AttesterDuty{
			Duty:                    domain.Duty{CommitteeIndex: 0},
			ValidatorCommitteeIndex: 1,
			CommitteeLength:         2,
			CommitteesAtSlot:        1,
		}

		included, needCommittees, err := attestationIncludesValidator(att, duty, nil)
		if err != nil {
			t.Fatalf("attestationIncludesValidator: %v", err)
		}
		if !included || needCommittees {
			t.Fatalf("included=%t needCommittees=%t, want true false", included, needCommittees)
		}
	})

	t.Run("target committee absent", func(t *testing.T) {
		att := apiAttestation{
			AggregationBits: "0x06",
			CommitteeBits:   "0x0100000000000000",
		}
		att.Data.Index = "0"
		duty := AttesterDuty{Duty: domain.Duty{CommitteeIndex: 1}}

		included, needCommittees, err := attestationIncludesValidator(att, duty, nil)
		if err != nil {
			t.Fatalf("attestationIncludesValidator: %v", err)
		}
		if included || needCommittees {
			t.Fatalf("included=%t needCommittees=%t, want false false", included, needCommittees)
		}
	})

	t.Run("multi committee uses preceding committee lengths", func(t *testing.T) {
		att := apiAttestation{
			AggregationBits: "0x30",
			CommitteeBits:   "0x0a00000000000000",
		}
		att.Data.Index = "0"
		duty := AttesterDuty{
			Duty:                    domain.Duty{CommitteeIndex: 3},
			ValidatorCommitteeIndex: 1,
			CommitteeLength:         2,
			CommitteesAtSlot:        4,
		}

		included, needCommittees, err := attestationIncludesValidator(att, duty, nil)
		if err != nil {
			t.Fatalf("attestationIncludesValidator without lengths: %v", err)
		}
		if included || !needCommittees {
			t.Fatalf("included=%t needCommittees=%t, want false true", included, needCommittees)
		}

		lengths := map[domain.CommitteeIndex]uint64{1: 3, 3: 2}
		included, needCommittees, err = attestationIncludesValidator(att, duty, lengths)
		if err != nil {
			t.Fatalf("attestationIncludesValidator with lengths: %v", err)
		}
		if !included || needCommittees {
			t.Fatalf("included=%t needCommittees=%t, want true false", included, needCommittees)
		}
	})
}

func TestFetchCommitteeLengths(t *testing.T) {
	srv := serveTestdata(t, map[string]string{
		"/eth/v1/beacon/states/head/committees": "committees.json",
	})
	defer srv.Close()

	lengths, err := NewClient(srv.URL, 0).fetchCommitteeLengths(context.Background(), "head", 651, 1)
	if err != nil {
		t.Fatalf("fetchCommitteeLengths: %v", err)
	}
	if got := lengths[0]; got != 2 {
		t.Fatalf("committee 0 length = %d, want 2", got)
	}
}

func TestAttestationIncludesValidatorElectraRejectsMalformedData(t *testing.T) {
	duty := AttesterDuty{Duty: domain.Duty{CommitteeIndex: 0}}

	tests := []struct {
		name string
		att  apiAttestation
	}{
		{
			name: "nonzero data index",
			att: apiAttestation{
				AggregationBits: "0x03",
				CommitteeBits:   "0x0100000000000000",
			},
		},
		{
			name: "short committee bits",
			att: apiAttestation{
				AggregationBits: "0x03",
				CommitteeBits:   "0x01",
			},
		},
		{
			name: "empty committee bits",
			att: apiAttestation{
				AggregationBits: "0x03",
				CommitteeBits:   "0x0000000000000000",
			},
		},
	}
	tests[0].att.Data.Index = "1"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := attestationIncludesValidator(tt.att, duty, nil)
			if err == nil {
				t.Fatal("attestationIncludesValidator: want error, got nil")
			}
		})
	}
}
