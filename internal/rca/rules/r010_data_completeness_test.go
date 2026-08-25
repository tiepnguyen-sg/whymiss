package rules

import (
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func TestDataCompleteness(t *testing.T) {
	t.Run("blocks attribution while the collection window is open", func(t *testing.T) {
		tl := timelineWith(t, mustObs(t, domain.ObsBlockSeen, offset(time.Second), nil))
		tl.CollectionComplete = false
		v, ok := DataCompleteness{}.Evaluate(tl, defaultCfg)
		if !ok || v.Cause != domain.CauseInsufficientData {
			t.Fatalf("verdict = %+v, matched = %v; want insufficient_data", v, ok)
		}
	})

	t.Run("blocks attribution when elapsed time has no positive completion record", func(t *testing.T) {
		tl := timelineWith(t, mustObs(t, domain.ObsBlockSeen, offset(time.Second), nil))
		tl.Observations = tl.Observations[:len(tl.Observations)-1]
		tl.CollectionComplete = true
		v, ok := DataCompleteness{}.Evaluate(tl, defaultCfg)
		if !ok || v.Cause != domain.CauseInsufficientData {
			t.Fatalf("verdict = %+v, matched = %v; want insufficient_data", v, ok)
		}
	})

	t.Run("does not require block_seen for a skipped proposer slot", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsAttestationIncluded, offset(4*time.Second), map[domain.AttrKey]string{
				domain.AttrInclusionDelay: "1", domain.AttrHeadCorrect: "true", domain.AttrTargetCorrect: "true",
			}),
		)
		if _, ok := (DataCompleteness{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("does not match when block_seen also exists", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsBlockSeen, offset(1*time.Second), nil),
			mustObs(t, domain.ObsAttestationIncluded, offset(4*time.Second), map[domain.AttrKey]string{
				domain.AttrInclusionDelay: "1", domain.AttrHeadCorrect: "true", domain.AttrTargetCorrect: "true",
			}),
		)
		if _, ok := (DataCompleteness{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("matches when canonical reward evidence is missing", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsBlockSeen, offset(time.Second), nil),
			mustObs(t, domain.ObsAttestationIncluded, offset(4*time.Second), map[domain.AttrKey]string{domain.AttrInclusionDelay: "1"}),
		)
		v, ok := (DataCompleteness{}).Evaluate(tl, defaultCfg)
		if !ok || v.Cause != domain.CauseInsufficientData {
			t.Fatalf("verdict=%+v matched=%t, want insufficient_data", v, ok)
		}
	})

	t.Run("does not match on published without block_seen", func(t *testing.T) {
		// A validator client can attest to a head it already has even when
		// this node's own collector never saw this slot's block — normal
		// behaviour, not inconsistent data. R-100 is what explains that
		// shape when the slot really was skipped; R-010 must not pre-empt it
		// by calling the missing block_seen incomplete data.
		tl := timelineWith(t,
			mustObs(t, domain.ObsAttestationPublished, offset(2*time.Second), nil),
		)
		if _, ok := (DataCompleteness{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("does not match when neither block_seen nor attestation_included exist", func(t *testing.T) {
		tl := timelineWith(t)
		if _, ok := (DataCompleteness{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})
}
