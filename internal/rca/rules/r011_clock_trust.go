package rules

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// ClockTrust is R-011: a bad clock invalidates every other duration
// measurement (I-9), so it fires early and — by virtue of first-match-wins
// ordering, no special-cased logic needed — suppresses every timing rule
// below it. Its own cause is local.host.clock_drift, not
// unknown.insufficient_data: docs/causes.md §6's table shorthand for this
// row ("unknown.insufficient_data") describes what happens to *other*
// rules once clock trust fails, not R-011's own reported cause — see its
// full entry in §7.
type ClockTrust struct{}

// ID returns R-011.
func (ClockTrust) ID() string { return "R-011" }

// Evaluate implements rca.Rule.
func (ClockTrust) Evaluate(tl domain.Timeline, cfg Config) (*domain.Verdict, bool) {
	var worstAt time.Time
	var worstOffset time.Duration
	for _, obs := range tl.Observations {
		if !requiresClockTrust(obs.Kind) {
			continue
		}
		if !obs.ClockMeasured {
			return insufficientClock(tl, obs.At,
				fmt.Sprintf("no clock offset was measured for %s, so timing-based attribution is unavailable", obs.Kind)), true
		}
		age := absDuration(obs.At.Sub(obs.ClockSampleAt))
		if cfg.ClockSampleMaxAge <= 0 || age > cfg.ClockSampleMaxAge {
			return insufficientClock(tl, obs.At,
				fmt.Sprintf("the clock sample attached to %s is %s old, exceeding the %s freshness limit", obs.Kind, age, cfg.ClockSampleMaxAge)), true
		}

		offset := absDuration(obs.ClockOffset)
		if worstAt.IsZero() || offset > worstOffset {
			worstAt, worstOffset = obs.ClockSampleAt, offset
		}
	}
	latest := make(map[string]domain.MetricSample)
	for _, sample := range tl.Samples {
		if sample.Source == domain.SourceDerived {
			continue
		}
		key := string(sample.Component) + "\x00" + string(sample.Name)
		if current, ok := latest[key]; !ok || sample.At.After(current.At) {
			latest[key] = sample
		}
	}
	keys := make([]string, 0, len(latest))
	for key := range latest {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		sample := latest[key]
		if !sample.ClockMeasured {
			return insufficientClock(tl, sample.At,
				fmt.Sprintf("no clock offset was measured for sample %s, so timing-based attribution is unavailable", sample.Name)), true
		}
		age := absDuration(sample.At.Sub(sample.ClockSampleAt))
		if cfg.ClockSampleMaxAge <= 0 || age > cfg.ClockSampleMaxAge {
			return insufficientClock(tl, sample.At,
				fmt.Sprintf("the clock sample attached to metric %s is %s old, exceeding the %s freshness limit", sample.Name, age, cfg.ClockSampleMaxAge)), true
		}
		offset := absDuration(sample.ClockOffset)
		if worstAt.IsZero() || offset > worstOffset {
			worstAt, worstOffset = sample.ClockSampleAt, offset
		}
	}

	// A timeline with no timing-bearing observation can continue into
	// non-timing rules (for example a canonically confirmed skipped slot).
	if worstAt.IsZero() || worstOffset <= cfg.ClockOffsetMax {
		return nil, false
	}

	return &domain.Verdict{
		Cause:      domain.CauseHostClockDrift,
		Confidence: domain.ConfidenceHigh,
		Evidence: []domain.Evidence{{
			At:        worstAt,
			Statement: fmt.Sprintf("measured clock offset of %s exceeds the %s trust threshold; all downstream timing-based reasoning is unavailable", worstOffset, cfg.ClockOffsetMax),
			Source:    domain.SourceClock,
			Comparison: &domain.Comparison{
				Label:    "clock offset",
				Observed: worstOffset.Seconds() * 1000,
				Expected: cfg.ClockOffsetMax.Seconds() * 1000,
				Unit:     domain.UnitMilliseconds,
			},
		}},
		Remediation: []string{
			"install and enable chrony or systemd-timesyncd",
			"verify with `chronyc tracking`",
		},
	}, true
}

func absDuration(value time.Duration) time.Duration {
	if value == time.Duration(math.MinInt64) {
		return time.Duration(math.MaxInt64)
	}
	if value < 0 {
		return -value
	}
	return value
}

// requiresClockTrust reports whether an observation's timestamp participates in
// cause attribution. Slot start is derived from chain genesis, and duty assignment
// merely records that work was owed; neither is a locally timed stage boundary.
func requiresClockTrust(kind domain.ObservationKind) bool {
	return kind != domain.ObsSlotStart && kind != domain.ObsDutyAssigned && kind != domain.ObsBlockSkipped && kind != domain.ObsClockSampled && kind != domain.ObsCollectionCompleted && kind != domain.ObsNetworkBaselineSampled
}

func insufficientClock(tl domain.Timeline, at time.Time, statement string) *domain.Verdict {
	if at.IsZero() {
		at = tl.SlotStart
	}
	return &domain.Verdict{
		Cause:      domain.CauseInsufficientData,
		Confidence: domain.ConfidenceLow,
		Evidence: []domain.Evidence{{
			At:        at,
			Statement: statement,
			Source:    domain.SourceDerived,
		}},
		Remediation: []string{
			"configure whymiss watch with an NTP server and verify that clock samples remain fresh",
		},
	}
}
