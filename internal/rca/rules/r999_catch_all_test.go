package rules

import (
	"testing"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func TestCatchAll(t *testing.T) {
	t.Run("always matches", func(t *testing.T) {
		tl := timelineWith(t)
		v, ok := CatchAll{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Cause != domain.CauseNoRuleMatched {
			t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseNoRuleMatched)
		}
		if v.Confidence != domain.ConfidenceLow {
			t.Errorf("Confidence = %q, want low", v.Confidence)
		}
	})
}

func TestOrder(t *testing.T) {
	if len(Order) == 0 {
		t.Fatal("Order must not be empty")
	}
	if _, ok := Order[len(Order)-1].(CatchAll); !ok {
		t.Errorf("last rule in Order = %T, want CatchAll (must terminate the sequence)", Order[len(Order)-1])
	}
	seen := make(map[string]bool)
	for _, r := range Order {
		if seen[r.ID()] {
			t.Errorf("duplicate rule ID %q in Order", r.ID())
		}
		seen[r.ID()] = true
	}
}
