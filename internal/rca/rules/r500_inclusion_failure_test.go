package rules

import (
	"strings"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func TestInclusionFailure(t *testing.T) {
	rootA := "0x" + strings.Repeat("a", 64)
	rootB := "0x" + strings.Repeat("b", 64)
	t.Run("matches at medium confidence when published on time with no inclusion and no reorg", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsHeadUpdated, offset(time.Second), map[domain.AttrKey]string{domain.AttrBlockRoot: rootA}),
			mustObs(t, domain.ObsAttestationPublished, offset(2*time.Second), map[domain.AttrKey]string{domain.AttrBlockRoot: rootA}),
		)
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
		completed, _ := tl.First(domain.ObsCollectionCompleted)
		if got := v.Evidence[1].At; !got.Equal(completed.At) {
			t.Errorf("absence evidence time = %s, want collection completion %s", got, completed.At)
		}
	})

	t.Run("stays medium when an unlinked reorg was observed in the window", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsHeadUpdated, offset(time.Second), map[domain.AttrKey]string{domain.AttrBlockRoot: rootA}),
			mustObs(t, domain.ObsAttestationPublished, offset(2*time.Second), map[domain.AttrKey]string{domain.AttrBlockRoot: rootA}),
			mustObs(t, domain.ObsReorg, offset(3*time.Second), nil),
		)
		v, ok := InclusionFailure{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Confidence != domain.ConfidenceMedium {
			t.Errorf("Confidence = %q, want medium", v.Confidence)
		}
		if len(v.Evidence) != 4 {
			t.Fatalf("Evidence count = %d, want publish, completed absence, canonical head, and reorg context", len(v.Evidence))
		}
	})

	t.Run("reports a future-slot reorg as context without raising confidence", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsHeadUpdated, offset(time.Second), map[domain.AttrKey]string{domain.AttrBlockRoot: rootA}),
			mustObs(t, domain.ObsAttestationPublished, offset(2*time.Second), map[domain.AttrKey]string{domain.AttrBlockRoot: rootA}),
		)
		future := mustObs(t, domain.ObsReorg, offset(12*time.Second), nil)
		future.Slot = 101
		tl.Reorgs = []domain.Observation{future}
		v, ok := InclusionFailure{}.Evaluate(tl, defaultCfg)
		if !ok || v.Confidence != domain.ConfidenceMedium {
			t.Fatalf("Evaluate = %+v, %t, want medium inclusion failure", v, ok)
		}
		if len(v.Evidence) != 4 {
			t.Fatalf("Evidence count = %d, want future reorg context", len(v.Evidence))
		}
	})

	t.Run("reports insufficient data without canonical head-root evidence", func(t *testing.T) {
		tl := timelineWith(t, mustObs(t, domain.ObsAttestationPublished, offset(2*time.Second), nil))
		v, ok := InclusionFailure{}.Evaluate(tl, defaultCfg)
		if !ok || v.Cause != domain.CauseInsufficientData {
			t.Fatalf("Evaluate = %+v, %t, want insufficient_data", v, ok)
		}
	})

	t.Run("does not call a noncanonical vote an inclusion failure", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsHeadUpdated, offset(time.Second), map[domain.AttrKey]string{domain.AttrBlockRoot: rootA}),
			mustObs(t, domain.ObsAttestationPublished, offset(2*time.Second), map[domain.AttrKey]string{domain.AttrBlockRoot: rootB}),
		)
		if _, ok := (InclusionFailure{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("mismatched head roots are not canonical inclusion-failure evidence")
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
