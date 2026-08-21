package rules

import (
	"testing"
	"time"

	"github.com/CHANGEME/whymiss/internal/domain"
	"github.com/CHANGEME/whymiss/internal/rca"
)

var slotStart = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

// obsSource is the observation-kind-to-source mapping every real collector
// in this codebase follows (mirrors internal/domain's own test helper).
func obsSource(kind domain.ObservationKind) domain.SourceID {
	switch kind {
	case domain.ObsHostSampled:
		return domain.SourceHostMetrics
	case domain.ObsEngineCall:
		return domain.SourcePromScrape
	default:
		return domain.SourceBeaconAPI
	}
}

func mustObs(t *testing.T, kind domain.ObservationKind, at time.Time, attrs map[domain.AttrKey]string) domain.Observation {
	t.Helper()
	o, err := domain.NewObservation(domain.Observation{
		Slot: 100, Kind: kind, At: at, Source: obsSource(kind), Attrs: attrs,
	})
	if err != nil {
		t.Fatalf("NewObservation(%s): %v", kind, err)
	}
	return o
}

// timelineWith builds a valid attester-duty Timeline for slot 100 out of the
// given observations (must already be in ascending time order, per
// domain.Timeline.Validate).
func timelineWith(t *testing.T, obs ...domain.Observation) domain.Timeline {
	t.Helper()
	tl, err := domain.NewTimeline(domain.Timeline{
		Slot:         100,
		SlotStart:    slotStart,
		Schedule:     domain.MainnetPreEPBS(),
		Duty:         &domain.Duty{Kind: domain.DutyAttester, Slot: 100, ValidatorIndex: 1},
		Observations: obs,
	})
	if err != nil {
		t.Fatalf("NewTimeline: %v", err)
	}
	return tl
}

func offset(d time.Duration) time.Time { return slotStart.Add(d) }

var defaultCfg = rca.DefaultConfig()
