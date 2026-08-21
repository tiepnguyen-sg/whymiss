package rules

import (
	"testing"
	"time"

	"github.com/CHANGEME/whymiss/internal/domain"
)

func peerCountSample(value float64) domain.MetricSample {
	return domain.MetricSample{At: slotStart, Component: domain.ComponentCL, Name: metricCLPeerCount, Value: value, Source: domain.SourcePromScrape}
}

func TestP2PDegraded(t *testing.T) {
	t.Run("matches at medium confidence when block_seen alone is far past the deadline (no published or included at all)", func(t *testing.T) {
		// R-400 defers here specifically because block_seen arrived after
		// the deadline — a validator client giving up on a duty entirely
		// once the head was too stale, not evidence of disconnection.
		// R-200 is the correct landing spot.
		tl := timelineWith(t, mustObs(t, domain.ObsBlockSeen, offset(18*time.Second), nil))
		v, ok := (P2PDegraded{}).Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Cause != domain.CauseP2PDegraded {
			t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseP2PDegraded)
		}
		if v.Confidence != domain.ConfidenceMedium {
			t.Errorf("Confidence = %q, want medium", v.Confidence)
		}
	})

	t.Run("does not match when block_seen does not exist at all", func(t *testing.T) {
		tl := timelineWith(t)
		if _, ok := (P2PDegraded{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("matches at medium confidence: single known stage, block_seen far past the deadline, no peer sample", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsBlockSeen, offset(18*time.Second), nil),
			mustObs(t, domain.ObsAttestationIncluded, offset(19*time.Second), map[domain.AttrKey]string{domain.AttrInclusionDelay: "1"}),
		)
		v, ok := P2PDegraded{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Cause != domain.CauseP2PDegraded {
			t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseP2PDegraded)
		}
		if v.Confidence != domain.ConfidenceMedium {
			t.Errorf("Confidence = %q, want medium", v.Confidence)
		}
	})

	t.Run("matches at high confidence when a low peer count sample corroborates (live-collection MetricSample form)", func(t *testing.T) {
		draft := domain.Timeline{
			Slot: 100, SlotStart: slotStart, Schedule: domain.MainnetPreEPBS(),
			Duty: &domain.Duty{Kind: domain.DutyAttester, Slot: 100, ValidatorIndex: 1},
			Observations: []domain.Observation{
				mustObs(t, domain.ObsBlockSeen, offset(18*time.Second), nil),
				mustObs(t, domain.ObsAttestationIncluded, offset(19*time.Second), map[domain.AttrKey]string{domain.AttrInclusionDelay: "1"}),
			},
			Samples: []domain.MetricSample{peerCountSample(5)},
		}
		tl, err := domain.NewTimeline(draft)
		if err != nil {
			t.Fatalf("NewTimeline: %v", err)
		}
		v, ok := P2PDegraded{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Confidence != domain.ConfidenceHigh {
			t.Errorf("Confidence = %q, want high", v.Confidence)
		}
	})

	t.Run("matches at high confidence when a low peer count sample corroborates (corpus Observation form)", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsBlockSeen, offset(18*time.Second), nil),
			mustObs(t, domain.ObsAttestationIncluded, offset(19*time.Second), map[domain.AttrKey]string{domain.AttrInclusionDelay: "1"}),
			mustObs(t, domain.ObsPeerCountSampled, offset(19*time.Second), map[domain.AttrKey]string{domain.AttrPeerCount: "1"}),
		)
		v, ok := P2PDegraded{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Confidence != domain.ConfidenceHigh {
			t.Errorf("Confidence = %q, want high", v.Confidence)
		}
		if v.Evidence[0].Comparison == nil || v.Evidence[0].Comparison.Observed != 1 {
			t.Errorf("evidence comparison = %+v, want observed=1", v.Evidence[0].Comparison)
		}
	})

	t.Run("does not match when peer count is at or above the minimum", func(t *testing.T) {
		draft := domain.Timeline{
			Slot: 100, SlotStart: slotStart, Schedule: domain.MainnetPreEPBS(),
			Duty: &domain.Duty{Kind: domain.DutyAttester, Slot: 100, ValidatorIndex: 1},
			Observations: []domain.Observation{
				mustObs(t, domain.ObsBlockSeen, offset(18*time.Second), nil),
				mustObs(t, domain.ObsAttestationIncluded, offset(19*time.Second), map[domain.AttrKey]string{domain.AttrInclusionDelay: "1"}),
			},
			Samples: []domain.MetricSample{peerCountSample(80)},
		}
		tl, err := domain.NewTimeline(draft)
		if err != nil {
			t.Fatalf("NewTimeline: %v", err)
		}
		if _, ok := (P2PDegraded{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("does not match when block_seen is comfortably within the attestation budget (cl-slow-cpu shape)", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsBlockSeen, offset(1400*time.Millisecond), nil),
			mustObs(t, domain.ObsAttestationIncluded, offset(35*time.Second), map[domain.AttrKey]string{domain.AttrInclusionDelay: "2"}),
		)
		if _, ok := (P2PDegraded{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("matches via the two-stage share path when validation is also known", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsBlockSeen, offset(5*time.Second), nil),
			mustObs(t, domain.ObsAttestationPublished, offset(5200*time.Millisecond), nil),
		)
		v, ok := P2PDegraded{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Confidence != domain.ConfidenceMedium {
			t.Errorf("Confidence = %q, want medium", v.Confidence)
		}
	})
}
