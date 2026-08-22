package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// obs builds a valid observation for the timeline tests. Offsets are expressed
// relative to the slot start because that is how the timing model reads.
func obs(slot domain.Slot, kind domain.ObservationKind, offset time.Duration) domain.Observation {
	source := domain.SourceBeaconAPI
	switch kind {
	case domain.ObsSlotStart:
		source = domain.SourceDerived
	case domain.ObsClockSampled:
		source = domain.SourceClock
	case domain.ObsHostSampled:
		source = domain.SourceHostMetrics
	case domain.ObsEngineCall, domain.ObsPeerCountSampled:
		source = domain.SourcePromScrape
	}
	return domain.Observation{Slot: slot, Kind: kind, At: at.Add(offset), Source: source}
}

// validTimeline is the happy path every rejection case mutates one field of.
func validTimeline() domain.Timeline {
	return domain.Timeline{
		Slot:      100,
		SlotStart: at,
		Schedule:  domain.MainnetPreEPBS(),
		Duty: &domain.Duty{
			Kind: domain.DutyAttester, Slot: 100, ValidatorIndex: 987654,
		},
		Observations: []domain.Observation{
			obs(100, domain.ObsSlotStart, 0),
			obs(100, domain.ObsBlockSeen, 1200*time.Millisecond),
			obs(100, domain.ObsHeadUpdated, 1800*time.Millisecond),
			obs(100, domain.ObsAttestationPublished, 2100*time.Millisecond),
		},
	}
}

func TestNewTimelineValid(t *testing.T) {
	t.Parallel()

	got, err := domain.NewTimeline(validTimeline())
	if err != nil {
		t.Fatalf("NewTimeline() error = %v, want nil", err)
	}
	if len(got.Observations) != 4 {
		t.Errorf("len(Observations) = %d, want 4", len(got.Observations))
	}
}

// TestNewTimelineCopies proves the engine cannot be handed a timeline the assembler
// is still mutating. A rule reasoning about a slice that changes underneath it would
// break determinism in a way no golden test could reproduce.
func TestNewTimelineCopies(t *testing.T) {
	t.Parallel()

	draft := validTimeline()
	tl, err := domain.NewTimeline(draft)
	if err != nil {
		t.Fatalf("NewTimeline() error = %v", err)
	}

	draft.Observations[0] = obs(100, domain.ObsReorg, 0)
	draft.Duty.ValidatorIndex = 1

	if tl.Observations[0].Kind != domain.ObsSlotStart {
		t.Errorf("observation 0 = %q after caller mutated the draft, want slot_start",
			tl.Observations[0].Kind)
	}
	if tl.Duty.ValidatorIndex != 987654 {
		t.Errorf("duty validator index = %d after caller mutated the draft, want 987654",
			tl.Duty.ValidatorIndex)
	}
}

func TestNewTimelineRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*domain.Timeline)
		want   error
	}{
		{
			name:   "zero slot start",
			mutate: func(tl *domain.Timeline) { tl.SlotStart = time.Time{} },
			want:   domain.ErrMissingTimestamp,
		},
		{
			name: "slot start not in utc",
			mutate: func(tl *domain.Timeline) {
				tl.SlotStart = at.In(time.FixedZone("CET", 3600))
			},
			want: domain.ErrNotUTC,
		},
		{
			name: "observations out of order",
			mutate: func(tl *domain.Timeline) {
				tl.Observations[1], tl.Observations[2] = tl.Observations[2], tl.Observations[1]
			},
			want: domain.ErrUnsorted,
		},
		{
			name: "observation belongs to another slot",
			mutate: func(tl *domain.Timeline) {
				tl.Observations[1].Slot = 101
			},
			want: domain.ErrSlotMismatch,
		},
		{
			name:   "duty belongs to another slot",
			mutate: func(tl *domain.Timeline) { tl.Duty.Slot = 101 },
			want:   domain.ErrSlotMismatch,
		},
		{
			name: "duty kind not attributed in v1",
			mutate: func(tl *domain.Timeline) {
				tl.Duty.Kind = "sync_committee"
			},
			want: domain.ErrInvalidKind,
		},
		{
			name: "baseline belongs to another slot",
			mutate: func(tl *domain.Timeline) {
				tl.Network = &domain.NetworkBaseline{
					Slot: 999, BlockArrivalP50: time.Second, BlockArrivalP90: 2 * time.Second,
					SampleCount: 10, Source: domain.SourceXatu,
				}
			},
			want: domain.ErrSlotMismatch,
		},
		{
			name: "invalid observation is rejected in place",
			mutate: func(tl *domain.Timeline) {
				tl.Observations[2].Kind = "invented"
			},
			want: domain.ErrInvalidKind,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tl := validTimeline()
			tc.mutate(&tl)

			_, err := domain.NewTimeline(tl)
			if !errors.Is(err, tc.want) {
				t.Fatalf("NewTimeline() error = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestNewTimelineRejectsBadSchedule covers the deadline ordering constraints, which
// would otherwise fail silently by producing impossible attributions.
func TestNewTimelineRejectsBadSchedule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		schedule domain.SlotSchedule
	}{
		{"zero schedule", domain.SlotSchedule{}},
		{
			"attestation deadline after aggregation",
			domain.SlotSchedule{
				SecondsPerSlot: 12 * time.Second, AttestationDeadline: 9 * time.Second,
				AggregationDeadline: 8 * time.Second,
			},
		},
		{
			"aggregation deadline past the end of the slot",
			domain.SlotSchedule{
				SecondsPerSlot: 12 * time.Second, AttestationDeadline: 4 * time.Second,
				AggregationDeadline: 13 * time.Second,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tl := validTimeline()
			tl.Schedule = tc.schedule

			if _, err := domain.NewTimeline(tl); err == nil {
				t.Fatal("NewTimeline() error = nil, want a schedule rejection")
			}
		})
	}
}

func TestTimelineLookups(t *testing.T) {
	t.Parallel()

	tl := validTimeline()
	tl.Observations = append(tl.Observations, obs(100, domain.ObsBlockSeen, 5*time.Second))

	t.Run("first returns the earliest", func(t *testing.T) {
		t.Parallel()

		got, ok := tl.First(domain.ObsBlockSeen)
		if !ok {
			t.Fatal("First(block_seen) not found")
		}
		if want := 1200 * time.Millisecond; got.Offset(tl.SlotStart) != want {
			t.Errorf("offset = %s, want %s", got.Offset(tl.SlotStart), want)
		}
	})

	t.Run("last returns the latest", func(t *testing.T) {
		t.Parallel()

		got, ok := tl.Last(domain.ObsBlockSeen)
		if !ok {
			t.Fatal("Last(block_seen) not found")
		}
		if want := 5 * time.Second; got.Offset(tl.SlotStart) != want {
			t.Errorf("offset = %s, want %s", got.Offset(tl.SlotStart), want)
		}
	})

	t.Run("absence is reported, not zero-valued", func(t *testing.T) {
		t.Parallel()

		if _, ok := tl.First(domain.ObsAttestationIncluded); ok {
			t.Error("First(attestation_included) = found, want absent")
		}
		if tl.Has(domain.ObsReorg) {
			t.Error("Has(reorg) = true, want false")
		}
	})

	t.Run("count", func(t *testing.T) {
		t.Parallel()

		if got := tl.Count(domain.ObsBlockSeen); got != 2 {
			t.Errorf("Count(block_seen) = %d, want 2", got)
		}
	})
}

func TestTimelineMaxAbsClockOffset(t *testing.T) {
	t.Parallel()

	t.Run("reports the worst magnitude regardless of sign", func(t *testing.T) {
		t.Parallel()

		tl := validTimeline()
		tl.Observations[1].ClockOffset = 30 * time.Millisecond
		tl.Observations[2].ClockOffset = -180 * time.Millisecond

		got, ok := tl.MaxAbsClockOffset()
		if !ok {
			t.Fatal("MaxAbsClockOffset() reported no observations")
		}
		if want := 180 * time.Millisecond; got != want {
			t.Errorf("MaxAbsClockOffset() = %s, want %s", got, want)
		}
	})

	t.Run("empty timeline reports nothing measured", func(t *testing.T) {
		t.Parallel()

		tl := validTimeline()
		tl.Observations = nil

		if _, ok := tl.MaxAbsClockOffset(); ok {
			t.Error("MaxAbsClockOffset() = measured, want unmeasured on an empty timeline")
		}
	})
}

func TestTimelineSampleValue(t *testing.T) {
	t.Parallel()

	tl := validTimeline()
	tl.Samples = []domain.MetricSample{
		{At: at, Component: domain.ComponentEL, Name: "engine_newpayload_ms", Value: 240, Source: domain.SourcePromScrape},
		{At: at.Add(time.Second), Component: domain.ComponentEL, Name: "engine_newpayload_ms", Value: 2780, Source: domain.SourcePromScrape},
		{At: at, Component: domain.ComponentHost, Name: "iowait_pct", Value: 23.4, Source: domain.SourceHostMetrics},
	}

	if got, ok := tl.SampleValue(domain.ComponentEL, "engine_newpayload_ms"); !ok || got != 2780 {
		t.Errorf("SampleValue(el) = %v, %v, want 2780, true", got, ok)
	}
	if got, ok := tl.SampleValue(domain.ComponentHost, "iowait_pct"); !ok || got != 23.4 {
		t.Errorf("SampleValue(host) = %v, %v, want 23.4, true", got, ok)
	}
	if _, ok := tl.SampleValue(domain.ComponentVC, "iowait_pct"); ok {
		t.Error("SampleValue() matched a metric on the wrong component")
	}
}

func TestTimelineAttestationDeadline(t *testing.T) {
	t.Parallel()

	tl := validTimeline()
	if got, want := tl.AttestationDeadline(), at.Add(4*time.Second); !got.Equal(want) {
		t.Errorf("AttestationDeadline() = %v, want %v", got, want)
	}
}

// TestTimelineLastAbsent covers the branch where Last scans the whole slice and
// finds nothing — distinct from finding the last of several matches.
func TestTimelineLastAbsent(t *testing.T) {
	t.Parallel()

	tl := validTimeline()
	if _, ok := tl.Last(domain.ObsReorg); ok {
		t.Error("Last(reorg) = found, want absent")
	}
}

// TestNewTimelineWithNetworkBaseline covers the baseline clone path in NewTimeline
// and the baseline validation path inside Timeline.Validate, neither of which the
// duty-only fixture above exercises.
func TestNewTimelineWithNetworkBaseline(t *testing.T) {
	t.Parallel()

	draft := validTimeline()
	draft.Network = &domain.NetworkBaseline{
		Slot: 100, BlockArrivalP50: time.Second, BlockArrivalP90: 2 * time.Second,
		SampleCount: 40, Source: domain.SourceXatu,
	}

	tl, err := domain.NewTimeline(draft)
	if err != nil {
		t.Fatalf("NewTimeline() error = %v, want nil", err)
	}

	draft.Network.SampleCount = 999
	if tl.Network.SampleCount == 999 {
		t.Error("Network baseline changed after the caller mutated the draft")
	}
}

func TestNewTimelineRejectsInvalidNetworkBaseline(t *testing.T) {
	t.Parallel()

	tl := validTimeline()
	tl.Network = &domain.NetworkBaseline{Slot: 100, Source: domain.SourceXatu} // SampleCount 0

	if _, err := domain.NewTimeline(tl); err == nil {
		t.Fatal("NewTimeline() error = nil, want a baseline validation failure")
	}
}

func TestNewTimelineRejectsInvalidSample(t *testing.T) {
	t.Parallel()

	tl := validTimeline()
	tl.Samples = []domain.MetricSample{{Name: "engine_newpayload_ms"}} // missing everything else

	if _, err := domain.NewTimeline(tl); err == nil {
		t.Fatal("NewTimeline() error = nil, want a sample validation failure")
	}
}
