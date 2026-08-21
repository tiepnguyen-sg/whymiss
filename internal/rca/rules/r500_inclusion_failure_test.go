package rules

import (
	"testing"
	"time"

	"github.com/CHANGEME/whymiss/internal/domain"
)

func TestInclusionFailure(t *testing.T) {
	t.Run("matches at medium confidence when published on time with no inclusion and no reorg", func(t *testing.T) {
		tl := timelineWith(t, mustObs(t, domain.ObsAttestationPublished, offset(2*time.Second), nil))
		v, ok := InclusionFailure{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Cause != domain.CauseInclusionFailure {
			t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseInclusionFailure)
		}
		if v.Confidence != domain.ConfidenceMedium {
			t.Errorf("Confidence = %q, want medium", v.Confidence)
		}
	})

	t.Run("matches at high confidence when a reorg was observed in the window", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsAttestationPublished, offset(2*time.Second), nil),
			mustObs(t, domain.ObsReorg, offset(3*time.Second), nil),
		)
		v, ok := InclusionFailure{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Confidence != domain.ConfidenceHigh {
			t.Errorf("Confidence = %q, want high", v.Confidence)
		}
	})

	t.Run("does not match when attestation_included exists", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsAttestationPublished, offset(2*time.Second), nil),
			mustObs(t, domain.ObsAttestationIncluded, offset(30*time.Second), map[domain.AttrKey]string{domain.AttrInclusionDelay: "1"}),
		)
		if _, ok := (InclusionFailure{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("does not match when published after the deadline", func(t *testing.T) {
		tl := timelineWith(t, mustObs(t, domain.ObsAttestationPublished, offset(5*time.Second), nil))
		if _, ok := (InclusionFailure{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("does not match when attestation_published never happened", func(t *testing.T) {
		tl := timelineWith(t)
		if _, ok := (InclusionFailure{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})
}
