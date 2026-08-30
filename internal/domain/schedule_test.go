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

// postEPBS builds a mainnet-shaped schedule with the two ePBS deadlines set, so
// the table below varies only what it is testing.
func postEPBS(payloadReveal, ptc time.Duration) domain.SlotSchedule {
	s := domain.MainnetPreEPBS()
	s.PayloadRevealDeadline = payloadReveal
	s.PTCDeadline = ptc
	return s
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

		// The ePBS pair. Values here are illustrative timings, not spec constants:
		// what is being tested is the ordering the schedule enforces, and the
		// package deliberately ships no post-ePBS default to be mistaken for one.
		{"post-epbs schedule", postEPBS(6*time.Second, 9*time.Second), false},
		{"payload reveal at slot end", postEPBS(12*time.Second, 0), false},
		{"payload reveal before the attestation deadline", postEPBS(3*time.Second, 9*time.Second), true},
		{"payload reveal equal to the attestation deadline", postEPBS(4*time.Second, 9*time.Second), true},
		{"payload reveal past slot end", postEPBS(13*time.Second, 0), true},
		{"ptc equal to payload reveal", postEPBS(6*time.Second, 6*time.Second), true},
		{"ptc before payload reveal", postEPBS(6*time.Second, 5*time.Second), true},
		{"ptc past slot end", postEPBS(6*time.Second, 13*time.Second), true},
		{"ptc without payload reveal", postEPBS(0, 9*time.Second), true},
		{"negative payload reveal", postEPBS(-1, 0), true},
		{"negative ptc", postEPBS(6*time.Second, -1), true},
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

func TestSlotScheduleEPBSDeadlinesAt(t *testing.T) {
	t.Parallel()

	t.Run("pre-epbs has neither instant", func(t *testing.T) {
		t.Parallel()

		s := domain.MainnetPreEPBS()
		if s.IsPostEPBS() {
			t.Error("IsPostEPBS() = true for the mainnet pre-ePBS schedule")
		}
		// The bool is what stops slotStart being read as a deadline that has
		// already passed on every slot of a fork that has no such deadline.
		if got, ok := s.PayloadRevealDeadlineAt(at); ok || !got.IsZero() {
			t.Errorf("PayloadRevealDeadlineAt() = %v, %v, want zero time and false", got, ok)
		}
		if got, ok := s.PTCDeadlineAt(at); ok || !got.IsZero() {
			t.Errorf("PTCDeadlineAt() = %v, %v, want zero time and false", got, ok)
		}
	})

	t.Run("post-epbs resolves both against the slot start", func(t *testing.T) {
		t.Parallel()

		s := postEPBS(6*time.Second, 9*time.Second)
		if !s.IsPostEPBS() {
			t.Error("IsPostEPBS() = false for a schedule carrying a payload-reveal deadline")
		}
		got, ok := s.PayloadRevealDeadlineAt(at)
		if !ok || !got.Equal(at.Add(6*time.Second)) {
			t.Errorf("PayloadRevealDeadlineAt() = %v, %v, want %v, true", got, ok, at.Add(6*time.Second))
		}
		got, ok = s.PTCDeadlineAt(at)
		if !ok || !got.Equal(at.Add(9*time.Second)) {
			t.Errorf("PTCDeadlineAt() = %v, %v, want %v, true", got, ok, at.Add(9*time.Second))
		}
	})

	t.Run("payload reveal without a ptc deadline", func(t *testing.T) {
		t.Parallel()

		s := postEPBS(6*time.Second, 0)
		if _, ok := s.PayloadRevealDeadlineAt(at); !ok {
			t.Error("PayloadRevealDeadlineAt() reported false with the deadline set")
		}
		if _, ok := s.PTCDeadlineAt(at); ok {
			t.Error("PTCDeadlineAt() reported true with no PTC deadline configured")
		}
	})
}
