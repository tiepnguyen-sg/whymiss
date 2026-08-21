package rules

import (
	"testing"
	"time"

	"github.com/CHANGEME/whymiss/internal/domain"
)

func TestDataCompleteness(t *testing.T) {
	t.Run("matches when attestation_included exists with no block_seen", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsAttestationIncluded, offset(4*time.Second), map[domain.AttrKey]string{domain.AttrInclusionDelay: "1"}),
		)
		v, ok := DataCompleteness{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Cause != domain.CauseInsufficientData {
			t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseInsufficientData)
		}
	})

	t.Run("does not match when block_seen also exists", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsBlockSeen, offset(1*time.Second), nil),
			mustObs(t, domain.ObsAttestationIncluded, offset(4*time.Second), map[domain.AttrKey]string{domain.AttrInclusionDelay: "1"}),
		)
		if _, ok := (DataCompleteness{}).Evaluate(tl, defaultCfg); ok {
			t.Fatal("want no match, got a match")
		}
	})

	t.Run("does not match on published without block_seen (host-memory-pressure shape)", func(t *testing.T) {
		// A validator client can attest to a head it already has even when
		// this node's own collector never saw this slot's block — normal
		// behaviour, not inconsistent data. See r100_proposer_missed.go's
		// doc comment for the real corpus scenario this covers.
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
