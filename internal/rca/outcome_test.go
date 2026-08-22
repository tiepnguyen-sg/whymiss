package rca

import (
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

var outcomeSlotStart = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

func mustOutcomeObs(t *testing.T, kind domain.ObservationKind, at time.Time, attrs map[domain.AttrKey]string) domain.Observation {
	t.Helper()
	o, err := domain.NewObservation(domain.Observation{Slot: 100, Kind: kind, At: at, Source: domain.SourceBeaconAPI, Attrs: attrs})
	if err != nil {
		t.Fatalf("NewObservation: %v", err)
	}
	return o
}

func TestDeriveOutcome_NoDuty(t *testing.T) {
	tl, err := domain.NewTimeline(domain.Timeline{Slot: 100, SlotStart: outcomeSlotStart, Schedule: domain.MainnetPreEPBS()})
	if err != nil {
		t.Fatalf("NewTimeline: %v", err)
	}
	outcome, flags := deriveOutcome(tl)
	if outcome != domain.OutcomeNoDuty {
		t.Errorf("outcome = %q, want %q", outcome, domain.OutcomeNoDuty)
	}
	if flags != nil {
		t.Errorf("flags = %+v, want nil", flags)
	}
}

func TestDeriveOutcome_Proposer(t *testing.T) {
	tests := []struct {
		name string
		obs  []domain.Observation
		want domain.Outcome
	}{
		{"proposed", []domain.Observation{mustOutcomeObs(t, domain.ObsBlockProposed, outcomeSlotStart.Add(time.Second), nil)}, domain.OutcomeOK},
		{"not proposed", nil, domain.OutcomeMissed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tl, err := domain.NewTimeline(domain.Timeline{
				Slot: 100, SlotStart: outcomeSlotStart, Schedule: domain.MainnetPreEPBS(),
				Duty: &domain.Duty{Kind: domain.DutyProposer, Slot: 100, ValidatorIndex: 1}, Observations: tc.obs,
			})
			if err != nil {
				t.Fatalf("NewTimeline: %v", err)
			}
			outcome, flags := deriveOutcome(tl)
			if outcome != tc.want {
				t.Errorf("outcome = %q, want %q", outcome, tc.want)
			}
			if flags != nil {
				t.Errorf("flags = %+v, want nil (proposer duty carries no reward flags)", flags)
			}
		})
	}
}

func TestDeriveOutcome_Attester(t *testing.T) {
	tests := []struct {
		name        string
		obs         []domain.Observation
		wantOutcome domain.Outcome
		wantFlags   *domain.RewardFlags
	}{
		{
			name:        "not included",
			obs:         nil,
			wantOutcome: domain.OutcomeMissed,
			wantFlags:   nil,
		},
		{
			name: "included with delay 1 (timely head)",
			obs: []domain.Observation{
				mustOutcomeObs(t, domain.ObsAttestationIncluded, outcomeSlotStart.Add(5*time.Second), map[domain.AttrKey]string{domain.AttrInclusionDelay: "1"}),
			},
			wantOutcome: domain.OutcomeOK,
			wantFlags:   &domain.RewardFlags{TimelySource: true, TimelyTarget: true, TimelyHead: true},
		},
		{
			name: "included with delay 2 (degraded)",
			obs: []domain.Observation{
				mustOutcomeObs(t, domain.ObsAttestationIncluded, outcomeSlotStart.Add(5*time.Second), map[domain.AttrKey]string{domain.AttrInclusionDelay: "2"}),
			},
			wantOutcome: domain.OutcomeDegraded,
			wantFlags:   &domain.RewardFlags{TimelySource: true, TimelyTarget: true, TimelyHead: false},
		},
		{
			name: "included with unparseable delay defensively treated as not timely",
			obs: []domain.Observation{
				mustOutcomeObs(t, domain.ObsAttestationIncluded, outcomeSlotStart.Add(5*time.Second), nil),
			},
			wantOutcome: domain.OutcomeDegraded,
			wantFlags:   &domain.RewardFlags{TimelySource: true, TimelyTarget: true, TimelyHead: false},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tl, err := domain.NewTimeline(domain.Timeline{
				Slot: 100, SlotStart: outcomeSlotStart, Schedule: domain.MainnetPreEPBS(),
				Duty: &domain.Duty{Kind: domain.DutyAttester, Slot: 100, ValidatorIndex: 1}, Observations: tc.obs,
			})
			if err != nil {
				t.Fatalf("NewTimeline: %v", err)
			}
			outcome, flags := deriveOutcome(tl)
			if outcome != tc.wantOutcome {
				t.Errorf("outcome = %q, want %q", outcome, tc.wantOutcome)
			}
			switch {
			case tc.wantFlags == nil && flags != nil:
				t.Errorf("flags = %+v, want nil", flags)
			case tc.wantFlags != nil && flags == nil:
				t.Errorf("flags = nil, want %+v", tc.wantFlags)
			case tc.wantFlags != nil && *flags != *tc.wantFlags:
				t.Errorf("flags = %+v, want %+v", *flags, *tc.wantFlags)
			}
		})
	}
}
