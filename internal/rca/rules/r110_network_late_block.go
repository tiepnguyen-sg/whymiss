package rules

import (
	"fmt"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// NetworkLateBlock detects deadline misses shared by local and network timing.
type NetworkLateBlock struct{}

// ID returns R-110.
func (NetworkLateBlock) ID() string { return "R-110" }

// Evaluate implements rca.Rule.
func (NetworkLateBlock) Evaluate(tl domain.Timeline, cfg Config) (*domain.Verdict, bool) {
	stages, propagationLate := propagationOverspent(tl, cfg)
	if !propagationLate {
		return nil, false
	}
	if tl.Network == nil {
		return &domain.Verdict{
			Cause:      domain.CauseInsufficientData,
			Confidence: domain.ConfidenceLow,
			Evidence: []domain.Evidence{{
				At:        tl.SlotStart,
				Statement: fmt.Sprintf("local block arrival at +%s exhausted the attestation budget, but no network baseline exists to distinguish network-wide lateness from local propagation", stages.Propagation),
				Source:    domain.SourcePromScrape,
			}},
			Remediation: []string{"enable the opt-in network baseline to distinguish network-wide lateness from local propagation"},
		}, true
	}

	local := stages.Propagation
	networkP50 := tl.Network.BlockArrivalP50
	deadline := tl.AttestationDeadline().Sub(tl.SlotStart)
	if local <= deadline || networkP50 <= deadline {
		return nil, false
	}
	deviation := local - networkP50
	if deviation < 0 {
		deviation = -deviation
	}
	if deviation >= cfg.NetworkDeviation {
		return nil, false
	}

	confidence := domain.ConfidenceHigh
	if tl.Network.SampleCount < 10 {
		confidence = domain.ConfidenceMedium
	}
	return &domain.Verdict{
		Cause:      domain.CauseLateBlock,
		Confidence: confidence,
		Evidence: []domain.Evidence{
			{
				At:        tl.SlotStart.Add(local),
				Statement: fmt.Sprintf("local block arrival at +%s exceeded the +%s attestation deadline", local, deadline),
				Source:    domain.SourceDerived,
				Comparison: &domain.Comparison{
					Label:    "local block arrival vs attestation deadline",
					Observed: local.Seconds() * 1000,
					Expected: deadline.Seconds() * 1000,
					Unit:     domain.UnitMilliseconds,
				},
			},
			{
				At:        tl.SlotStart,
				Statement: fmt.Sprintf("network p50 was +%s (p90 +%s), within %s of local arrival", networkP50, tl.Network.BlockArrivalP90, deviation),
				Source:    tl.Network.Source,
				Comparison: &domain.Comparison{
					Label:    "local block arrival vs network p50",
					Observed: local.Seconds() * 1000,
					Expected: networkP50.Seconds() * 1000,
					Unit:     domain.UnitMilliseconds,
				},
			},
		},
	}, true
}
