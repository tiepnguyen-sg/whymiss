package rules

import (
	"testing"
	"time"

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
		if len(v.Evidence) != 4 {
			t.Fatalf("Evidence count = %d, want gap plus three unavailable stages", len(v.Evidence))
		}
		completed, _ := tl.First(domain.ObsCollectionCompleted)
		if !v.Evidence[0].At.Equal(completed.At) {
			t.Errorf("gap evidence time = %s, want collection completion %s", v.Evidence[0].At, completed.At)
		}
	})

	t.Run("reports every known duration and share", func(t *testing.T) {
		tl := timelineWith(t,
			mustObs(t, domain.ObsBlockSeen, offset(time.Second), nil),
			mustObs(t, domain.ObsHeadUpdated, offset(2*time.Second), nil),
			mustObs(t, domain.ObsAttestationPublished, offset(3*time.Second), nil),
		)
		v, _ := CatchAll{}.Evaluate(tl, defaultCfg)
		if len(v.Evidence) != 7 {
			t.Fatalf("Evidence count = %d, want gap plus duration/share for three stages", len(v.Evidence))
		}
		for i := 1; i < len(v.Evidence); i++ {
			if v.Evidence[i].Comparison == nil {
				t.Fatalf("Evidence %d has no machine-checkable comparison", i)
			}
		}
	})
}

func TestOrder(t *testing.T) {
	order := Order()
	if len(order) == 0 {
		t.Fatal("Order must not be empty")
	}
	if _, ok := order[len(order)-1].(CatchAll); !ok {
		t.Errorf("last rule in Order = %T, want CatchAll (must terminate the sequence)", order[len(order)-1])
	}
	seen := make(map[string]bool)
	for _, r := range order {
		if seen[r.ID()] {
			t.Errorf("duplicate rule ID %q in Order", r.ID())
		}
		seen[r.ID()] = true
	}

	order[0] = CatchAll{}
	if Order()[0].ID() != "R-010" {
		t.Error("mutating a returned order changed process-wide analyzer state")
	}
}
