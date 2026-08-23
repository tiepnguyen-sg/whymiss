package rules

import (
	"fmt"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// VCSlow is R-410: the validator client received a valid head in time but
// published the attestation after the deadline.
//
// A client-metric block arrival is required; a later REST poll is not a
// propagation timestamp.
type VCSlow struct{}

// ID returns R-410.
func (VCSlow) ID() string { return "R-410" }

// Evaluate implements rca.Rule.
func (VCSlow) Evaluate(tl domain.Timeline, cfg Config) (*domain.Verdict, bool) {
	blockSeen, hasBlockSeen := timedBlockSeen(tl)
	head, hasHead := tl.First(domain.ObsHeadUpdated)
	published, hasPublished := tl.First(domain.ObsAttestationPublished)
	if !hasBlockSeen || !hasHead || !hasPublished {
		return nil, false
	}

	deadline := tl.AttestationDeadline()
	if !blockSeen.At.Before(deadline) || !head.At.Before(deadline) || !published.At.After(deadline) {
		return nil, false
	}
	stages := ComputeStages(tl)
	dominant, ok := stages.Dominant(cfg)
	if !ok || dominant != domain.StageSigning {
		return nil, false
	}

	headOffset := head.At.Sub(tl.SlotStart)
	publishOffset := published.At.Sub(tl.SlotStart)
	return &domain.Verdict{
		Cause:      domain.CauseVCSlow,
		Confidence: domain.ConfidenceMedium, // high requires a remote signer's own latency corroborating it — not collected by this build.
		Evidence: []domain.Evidence{{
			At:        published.At,
			Statement: fmt.Sprintf("the canonical head updated at +%s, before the attestation deadline (+%s), but the attestation was not published until +%s, after it", headOffset, tl.Schedule.AttestationDeadline, publishOffset),
			Source:    domain.SourceBeaconAPI,
		}},
		Remediation: []string{
			"if using a remote signer (Web3Signer or similar), measure its latency — it is frequently the culprit and rarely monitored",
			"check for CPU contention on the validator client host",
			"confirm validator client and beacon node clocks agree",
		},
	}, true
}
