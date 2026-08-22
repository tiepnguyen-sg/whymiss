package rules

import (
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func obsWithOffset(t *testing.T, at time.Time, clockOffset time.Duration) domain.Observation {
	t.Helper()
	o, err := domain.NewObservation(domain.Observation{
		Slot: 100, Kind: domain.ObsBlockSeen, At: at, ClockOffset: clockOffset, Source: domain.SourceBeaconAPI,
	})
	if err != nil {
		t.Fatalf("NewObservation: %v", err)
	}
	return o
}

func TestClockTrust(t *testing.T) {
	t.Run("matches when offset exceeds threshold", func(t *testing.T) {
		tl := timelineWith(t, obsWithOffset(t, offset(time.Second), 250*time.Millisecond))
		v, ok := ClockTrust{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Cause != domain.CauseHostClockDrift {
			t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseHostClockDrift)
		}
		if v.Confidence != domain.ConfidenceHigh {
			t.Errorf("Confidence = %q, want high", v.Confidence)
		}
	})

	t.Run("matches on negative offset beyond threshold (absolute value)", func(t *testing.T) {
		tl := timelineWith(t, obsWithOffset(t, offset(time.Second), -250*time.Millisecond))
		if _, ok := (ClockTrust{}).Evaluate(tl, defaultCfg); !ok {
			t.Fatal("want match, got no match")
		}
	})

	t.Run("does not match within threshold", func(t *testing.T) {
		tl := timelineWith(t, obsWithOffset(t, offset(time.Second), 50*time.Millisecond))
		if _, ok := (ClockTrust{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("does not match when no observation carries a clock offset", func(t *testing.T) {
		tl := timelineWith(t)
		if _, ok := (ClockTrust{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})
}
