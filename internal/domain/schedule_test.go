package domain_test

import (
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func TestMainnetPreEPBSIsValid(t *testing.T) {
	t.Parallel()

	if err := domain.MainnetPreEPBS().Validate(); err != nil {
		t.Fatalf("MainnetPreEPBS().Validate() = %v, want nil", err)
	}
}

func TestSlotScheduleValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		s       domain.SlotSchedule
		wantErr bool
	}{
		{"mainnet pre-epbs", domain.MainnetPreEPBS(), false},
		{
			"attestation deadline equals aggregation deadline",
			domain.SlotSchedule{SecondsPerSlot: 12 * time.Second, AttestationDeadline: 4 * time.Second, AggregationDeadline: 4 * time.Second},
			false,
		},
		{
			"aggregation deadline equals slot end",
			domain.SlotSchedule{SecondsPerSlot: 12 * time.Second, AttestationDeadline: 4 * time.Second, AggregationDeadline: 12 * time.Second},
			false,
		},
		{"zero seconds per slot", domain.SlotSchedule{AttestationDeadline: 4 * time.Second, AggregationDeadline: 8 * time.Second}, true},
		{"negative attestation deadline", domain.SlotSchedule{SecondsPerSlot: 12 * time.Second, AttestationDeadline: -1, AggregationDeadline: 8 * time.Second}, true},
		{"zero aggregation deadline", domain.SlotSchedule{SecondsPerSlot: 12 * time.Second, AttestationDeadline: 4 * time.Second}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.s.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestSlotScheduleSlotEndAt(t *testing.T) {
	t.Parallel()

	s := domain.MainnetPreEPBS()
	if got, want := s.SlotEndAt(at), at.Add(12*time.Second); !got.Equal(want) {
		t.Errorf("SlotEndAt() = %v, want %v", got, want)
	}
}
