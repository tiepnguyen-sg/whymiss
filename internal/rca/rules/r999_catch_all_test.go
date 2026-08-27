package rules

import (
	"strings"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func TestCatchAll(t *testing.T) {
	// A lost duty with nothing timed is the default deployment's normal
	// shape — no --cl-metrics-api means block arrival is polled, not measured,
	// so no stage boundary exists and every timing rule declines. Two live
	// Hoodi verdicts landed here (slots 3791371 and 3791424) and were told they
	// had found a project bug. See ADR-0024.
	t.Run("names the missing measurement when no stage was timed", func(t *testing.T) {
		tl := timelineWith(t)
		v, ok := CatchAll{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Cause != domain.CauseInsufficientData {
			t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseInsufficientData)
		}
		if v.Confidence != domain.ConfidenceLow {
			t.Errorf("Confidence = %q, want low", v.Confidence)
		}
		var namesFlag bool
		for _, r := range v.Remediation {
			if strings.Contains(r, "--cl-metrics-api") {
				namesFlag = true
			}
		}
		if !namesFlag {
			t.Errorf("Remediation = %v, want the flag that would make this diagnosable", v.Remediation)
		}
		for _, e := range v.Evidence {
			if strings.Contains(e.Statement, "taxonomy gap") {
				t.Error("an unmeasured duty was reported as a project bug")
			}
		}
	})

	// The engine turns R-999's no_rule_matched into its clean-pass verdict, so
	// this branch must not swallow a healthy duty: nothing was lost, so nothing
	// needs attributing, measured or not.
	t.Run("leaves a healthy duty to the engine's clean pass", func(t *testing.T) {
		tl := timelineWith(t, mustObs(t, domain.ObsAttestationIncluded, offset(14*time.Second), map[domain.AttrKey]string{
			domain.AttrInclusionDelay: "1", domain.AttrHeadCorrect: "true", domain.AttrTargetCorrect: "true",
		}))
		v, ok := CatchAll{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Cause != domain.CauseNoRuleMatched {
			t.Errorf("Cause = %q, want %q so the engine reports it healthy", v.Cause, domain.CauseNoRuleMatched)
		}
	})

	t.Run("still reports a real taxonomy gap when stages were timed", func(t *testing.T) {
		tl := timelineWith(t, mustObs(t, domain.ObsBlockSeen, offset(time.Second), nil))
		v, ok := CatchAll{}.Evaluate(tl, defaultCfg)
		if !ok {
			t.Fatal("want match, got no match")
		}
		if v.Cause != domain.CauseNoRuleMatched {
			t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseNoRuleMatched)
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
