package rules

import (
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// propagationDominantTL builds a timeline where block_seen (at blockSeenAt)
// is followed by attestation_published shortly after, so propagation's
// share of the two-stage total is large — needed since Stages.Dominant now
// requires at least two known stages to compare (see stages.go).
func propagationDominantTL(t *testing.T, blockSeenAt time.Duration, network *domain.NetworkBaseline) domain.Timeline {
	t.Helper()
	draft := domain.Timeline{
		Slot:      100,
		SlotStart: slotStart,
		Schedule:  domain.MainnetPreEPBS(),
		Duty:      &domain.Duty{Kind: domain.DutyAttester, Slot: 100, ValidatorIndex: 1},
		Observations: []domain.Observation{
			mustObs(t, domain.ObsBlockSeen, offset(blockSeenAt), nil),
			mustObs(t, domain.ObsAttestationPublished, offset(blockSeenAt+200*time.Millisecond), nil),
		},
		Network: network,
	}
	tl, err := domain.NewTimeline(draft)
	if err != nil {
		t.Fatalf("NewTimeline: %v", err)
	}
	return tl
}

func TestNetworkLateBlock(t *testing.T) {
	t.Run("returns insufficient data when no baseline exists", func(t *testing.T) {
		tl := propagationDominantTL(t, 5*time.Second, nil)
		v, ok := (NetworkLateBlock{}).Evaluate(tl, defaultCfg)
		if !ok || v.Cause != domain.CauseInsufficientData {
			t.Fatalf("verdict=%+v matched=%t, want insufficient_data", v, ok)
		}
	})

	t.Run("returns insufficient data for a lone late propagation stage", func(t *testing.T) {
		tl := timelineWith(t, mustObs(t, domain.ObsBlockSeen, offset(18*time.Second), nil))
		v, ok := (NetworkLateBlock{}).Evaluate(tl, defaultCfg)
		if !ok || v.Cause != domain.CauseInsufficientData {
			t.Fatalf("verdict=%+v matched=%t, want insufficient_data", v, ok)
		}
	})

	t.Run("matches at high confidence when deviation is small and sample count is adequate", func(t *testing.T) {
		tl := propagationDominantTL(t, 5*time.Second, &domain.NetworkBaseline{
			Slot: 100, BlockArrivalP50: 5100 * time.Millisecond, BlockArrivalP90: 6 * time.Second, SampleCount: 50, Source: domain.SourceXatu,
		})
		v, ok := NetworkLateBlock{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Cause != domain.CauseLateBlock {
			t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseLateBlock)
		}
		if v.Confidence != domain.ConfidenceHigh {
			t.Errorf("Confidence = %q, want high", v.Confidence)
		}
	})

	t.Run("matches at medium confidence when baseline sample count is thin", func(t *testing.T) {
		tl := propagationDominantTL(t, 5*time.Second, &domain.NetworkBaseline{
			Slot: 100, BlockArrivalP50: 5100 * time.Millisecond, BlockArrivalP90: 6 * time.Second, SampleCount: 3, Source: domain.SourceXatu,
		})
		v, ok := NetworkLateBlock{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Confidence != domain.ConfidenceMedium {
			t.Errorf("Confidence = %q, want medium", v.Confidence)
		}
	})

	t.Run("does not match when deviation exceeds threshold", func(t *testing.T) {
		tl := propagationDominantTL(t, 5*time.Second, &domain.NetworkBaseline{
			Slot: 100, BlockArrivalP50: 1 * time.Second, BlockArrivalP90: 2 * time.Second, SampleCount: 50, Source: domain.SourceXatu,
		})
		if _, ok := (NetworkLateBlock{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match (deviation too large), got a match")
		}
	})

	t.Run("does not match when the local block was timely", func(t *testing.T) {
		tl := propagationDominantTL(t, 3800*time.Millisecond, &domain.NetworkBaseline{
			Slot: 100, BlockArrivalP50: 4200 * time.Millisecond, BlockArrivalP90: 5 * time.Second, SampleCount: 50, Source: domain.SourceXatu,
		})
		if _, ok := (NetworkLateBlock{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match (local block was timely), got a match")
		}
	})

	t.Run("does not match when the network baseline was timely", func(t *testing.T) {
		tl := propagationDominantTL(t, 4200*time.Millisecond, &domain.NetworkBaseline{
			Slot: 100, BlockArrivalP50: 3800 * time.Millisecond, BlockArrivalP90: 5 * time.Second, SampleCount: 50, Source: domain.SourceXatu,
		})
		if _, ok := (NetworkLateBlock{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match (network baseline was timely), got a match")
		}
	})

	t.Run("does not match when propagation is not dominant", func(t *testing.T) {
		// block_seen just after slot start, published much later — validation
		// dominates instead.
		draft := domain.Timeline{
			Slot:      100,
			SlotStart: slotStart,
			Schedule:  domain.MainnetPreEPBS(),
			Duty:      &domain.Duty{Kind: domain.DutyAttester, Slot: 100, ValidatorIndex: 1},
			Observations: []domain.Observation{
				mustObs(t, domain.ObsBlockSeen, offset(200*time.Millisecond), nil),
				mustObs(t, domain.ObsAttestationPublished, offset(3*time.Second), nil),
			},
			Network: &domain.NetworkBaseline{Slot: 100, BlockArrivalP50: 200 * time.Millisecond, BlockArrivalP90: 400 * time.Millisecond, SampleCount: 50, Source: domain.SourceXatu},
		}
		tl, err := domain.NewTimeline(draft)
		if err != nil {
			t.Fatalf("NewTimeline: %v", err)
		}
		if _, ok := (NetworkLateBlock{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})
}
