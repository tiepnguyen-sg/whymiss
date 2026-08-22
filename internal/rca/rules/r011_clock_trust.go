package rules

import (
	"fmt"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/rca"
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
func (ClockTrust) Evaluate(tl domain.Timeline, cfg rca.Config) (*domain.Verdict, bool) {
	offset, ok := tl.MaxAbsClockOffset()
	if !ok || offset <= cfg.ClockOffsetMax {
		return nil, false
	}

	return &domain.Verdict{
		Cause:      domain.CauseHostClockDrift,
		Confidence: domain.ConfidenceHigh,
		Evidence: []domain.Evidence{{
			At:        tl.SlotStart,
			Statement: fmt.Sprintf("measured clock offset of %s exceeds the %s trust threshold; all downstream timing-based reasoning is unavailable", offset, cfg.ClockOffsetMax),
			Source:    domain.SourceClock,
			Comparison: &domain.Comparison{
				Label:    "clock offset",
				Observed: offset.Seconds() * 1000,
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
