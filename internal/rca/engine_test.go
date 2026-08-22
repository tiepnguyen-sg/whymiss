package rca_test

import (
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/rca"
	"github.com/tiepnguyen-sg/whymiss/internal/rca/rules"
)

var slotStart = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

func attesterTimeline(t *testing.T, obs ...domain.Observation) domain.Timeline {
	t.Helper()
	tl, err := domain.NewTimeline(domain.Timeline{
		Slot:         100,
		SlotStart:    slotStart,
		Schedule:     domain.MainnetPreEPBS(),
		Duty:         &domain.Duty{Kind: domain.DutyAttester, Slot: 100, ValidatorIndex: 1},
		Observations: obs,
	})
	if err != nil {
		t.Fatalf("NewTimeline: %v", err)
	}
	return tl
}

// fakeRule is a minimal rca.Rule for testing Analyze in isolation from the
// real rule set.
type fakeRule struct {
	id      string
	verdict *domain.Verdict
	match   bool
	called  *bool
}

func (f fakeRule) ID() string { return f.id }

func (f fakeRule) Evaluate(domain.Timeline, rca.Config) (*domain.Verdict, bool) {
	if f.called != nil {
		*f.called = true
	}
	return f.verdict, f.match
}

// withOrder installs a temporary rule sequence for the duration of the
// test, restoring rules.Order (the real sequence) afterward — rca.order is
// package-global state, so tests that mutate it must not leak that
// mutation into other tests in the same binary (including TestGolden_Corpus
// and any test that runs after this one).
func withOrder(t *testing.T, rs []rca.Rule) {
	t.Helper()
	rca.SetOrder(rs)
	t.Cleanup(func() { rca.SetOrder(rules.Order) })
}

func TestAnalyze_NoDuty(t *testing.T) {
	tl, err := domain.NewTimeline(domain.Timeline{
		Slot: 100, SlotStart: slotStart, Schedule: domain.MainnetPreEPBS(),
	})
	if err != nil {
		t.Fatalf("NewTimeline: %v", err)
	}

	v := rca.Analyze(tl, rca.DefaultConfig())
	if v.Outcome != domain.OutcomeNoDuty {
		t.Errorf("Outcome = %q, want %q", v.Outcome, domain.OutcomeNoDuty)
	}
	if v.Cause != "" {
		t.Errorf("Cause = %q, want empty (no_duty forbids a cause)", v.Cause)
	}
	if v.Flags != nil {
		t.Errorf("Flags = %+v, want nil", v.Flags)
	}
}

func TestAnalyze_FirstMatchWins(t *testing.T) {
	var secondCalled bool
	firstVerdict := &domain.Verdict{
		Cause:      domain.CauseProposerMissed,
		Confidence: domain.ConfidenceHigh,
		Evidence:   []domain.Evidence{{At: slotStart, Statement: "first rule matched", Source: domain.SourceDerived}},
	}
	withOrder(t, []rca.Rule{
		fakeRule{id: "R-FIRST", verdict: firstVerdict, match: true},
		fakeRule{id: "R-SECOND", match: true, called: &secondCalled},
	})

	tl := attesterTimeline(t, mustEngineTestObs(t, domain.ObsAttestationPublished, slotStart.Add(2*time.Second)))
	v := rca.Analyze(tl, rca.DefaultConfig())

	if secondCalled {
		t.Error("second rule was evaluated even though the first rule matched")
	}
	if v.Cause != domain.CauseProposerMissed {
		t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseProposerMissed)
	}
	if v.Slot != 100 {
		t.Errorf("Slot = %d, want 100", v.Slot)
	}
	if v.EngineVersion != rca.EngineVersion {
		t.Errorf("EngineVersion = %q, want %q", v.EngineVersion, rca.EngineVersion)
	}
	if v.TaxonomyVersion != domain.TaxonomyVersion {
		t.Errorf("TaxonomyVersion = %q, want %q", v.TaxonomyVersion, domain.TaxonomyVersion)
	}
}

func TestAnalyze_FallsThroughOnNoMatch(t *testing.T) {
	withOrder(t, []rca.Rule{
		fakeRule{id: "R-NOPE", match: false},
	})

	tl := attesterTimeline(t, mustEngineTestObs(t, domain.ObsAttestationPublished, slotStart.Add(2*time.Second)))
	v := rca.Analyze(tl, rca.DefaultConfig())

	// No unconditional catch-all installed — Analyze's own defensive
	// fallback must be what terminates the loop, never a panic (I-15).
	if v.Cause != domain.CauseNoRuleMatched {
		t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseNoRuleMatched)
	}
	if v.Confidence != domain.ConfidenceLow {
		t.Errorf("Confidence = %q, want low", v.Confidence)
	}
}

func TestAnalyze_SafeFallbackOnInvalidDraft(t *testing.T) {
	// A rule that matches but returns a structurally invalid draft (no
	// Evidence, which domain.Verdict.Validate requires) must not panic —
	// Analyze falls back to a hand-built, always-valid verdict instead.
	withOrder(t, []rca.Rule{
		fakeRule{id: "R-BROKEN", verdict: &domain.Verdict{Cause: domain.CauseProposerMissed, Confidence: domain.ConfidenceHigh}, match: true},
	})

	tl := attesterTimeline(t, mustEngineTestObs(t, domain.ObsAttestationPublished, slotStart.Add(2*time.Second)))
	v := rca.Analyze(tl, rca.DefaultConfig())

	if v.Cause != domain.CauseNoRuleMatched {
		t.Errorf("Cause = %q, want %q (safe fallback)", v.Cause, domain.CauseNoRuleMatched)
	}
	if len(v.Evidence) == 0 {
		t.Error("safe fallback verdict must still carry evidence (I-7)")
	}
	if v.EngineVersion == "" || v.TaxonomyVersion == "" {
		t.Error("safe fallback verdict must still be fully stamped")
	}
}

func mustEngineTestObs(t *testing.T, kind domain.ObservationKind, at time.Time) domain.Observation {
	t.Helper()
	o, err := domain.NewObservation(domain.Observation{Slot: 100, Kind: kind, At: at, Source: domain.SourceBeaconAPI})
	if err != nil {
		t.Fatalf("NewObservation: %v", err)
	}
	return o
}
