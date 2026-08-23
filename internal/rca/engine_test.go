package rca

import (
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

var slotStart = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

func attesterTimeline(t *testing.T, obs ...domain.Observation) domain.Timeline {
	t.Helper()
	obs = append(obs, mustEngineTestObs(t, domain.ObsCollectionCompleted, slotStart.Add(15*time.Minute)))
	tl, err := domain.NewTimeline(domain.Timeline{
		Slot:               100,
		SlotStart:          slotStart,
		Schedule:           domain.MainnetPreEPBS(),
		Duty:               &domain.Duty{Kind: domain.DutyAttester, Slot: 100, ValidatorIndex: 1},
		Observations:       obs,
		CollectionComplete: true,
	})
	if err != nil {
		t.Fatalf("NewTimeline: %v", err)
	}
	return tl
}

// fakeRule is a minimal Rule for testing Analyze in isolation from the
// real rule set.
type fakeRule struct {
	id      string
	verdict *domain.Verdict
	match   bool
	called  *bool
}

func (f fakeRule) ID() string { return f.id }

func (f fakeRule) Evaluate(domain.Timeline, Config) (*domain.Verdict, bool) {
	if f.called != nil {
		*f.called = true
	}
	return f.verdict, f.match
}

func TestAnalyze_NoDuty(t *testing.T) {
	tl, err := domain.NewTimeline(domain.Timeline{
		Slot: 100, SlotStart: slotStart, Schedule: domain.MainnetPreEPBS(),
	})
	if err != nil {
		t.Fatalf("NewTimeline: %v", err)
	}

	v := Analyze(tl, DefaultConfig())
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
	ordered := []Rule{
		fakeRule{id: "R-FIRST", verdict: firstVerdict, match: true},
		fakeRule{id: "R-SECOND", match: true, called: &secondCalled},
	}

	tl := attesterTimeline(t, mustEngineTestObs(t, domain.ObsAttestationPublished, slotStart.Add(2*time.Second)))
	v := analyze(tl, DefaultConfig(), ordered)

	if secondCalled {
		t.Error("second rule was evaluated even though the first rule matched")
	}
	if v.Cause != domain.CauseProposerMissed {
		t.Errorf("Cause = %q, want %q", v.Cause, domain.CauseProposerMissed)
	}
	if v.Slot != 100 {
		t.Errorf("Slot = %d, want 100", v.Slot)
	}
	if v.EngineVersion != EngineVersion {
		t.Errorf("EngineVersion = %q, want %q", v.EngineVersion, EngineVersion)
	}
	if v.TaxonomyVersion != domain.TaxonomyVersion {
		t.Errorf("TaxonomyVersion = %q, want %q", v.TaxonomyVersion, domain.TaxonomyVersion)
	}
}

func TestAnalyzeMaterializesEvidenceOffsets(t *testing.T) {
	evidenceAt := slotStart.Add(3900 * time.Millisecond)
	ordered := []Rule{fakeRule{id: "R-OFFSET", match: true, verdict: &domain.Verdict{
		Cause: domain.CauseProposerMissed, Confidence: domain.ConfidenceHigh,
		Evidence: []domain.Evidence{{At: evidenceAt, Statement: "measured fact", Source: domain.SourceDerived}},
	}}}

	v := analyze(attesterTimeline(t), DefaultConfig(), ordered)
	if got, want := v.Evidence[0].Offset, 3900*time.Millisecond; got != want {
		t.Fatalf("evidence offset = %s, want %s", got, want)
	}
}

func TestAnalyze_FallsThroughOnNoMatch(t *testing.T) {
	ordered := []Rule{
		fakeRule{id: "R-NOPE", match: false},
	}

	tl := attesterTimeline(t, mustEngineTestObs(t, domain.ObsAttestationPublished, slotStart.Add(2*time.Second)))
	v := analyze(tl, DefaultConfig(), ordered)

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
	ordered := []Rule{
		fakeRule{id: "R-BROKEN", verdict: &domain.Verdict{Cause: domain.CauseProposerMissed, Confidence: domain.ConfidenceHigh}, match: true},
	}

	tl := attesterTimeline(t, mustEngineTestObs(t, domain.ObsAttestationPublished, slotStart.Add(2*time.Second)))
	v := analyze(tl, DefaultConfig(), ordered)

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

// healthyTimeline is a duty that went entirely right: the block arrived
// well inside the budget, validation and signing both finished early, and
// the attestation was included with inclusion_delay 1 (which is what makes
// deriveOutcome return OutcomeOK with every reward flag earned).
func healthyTimeline(t *testing.T) domain.Timeline {
	t.Helper()
	included, err := domain.NewObservation(domain.Observation{
		Slot:   100,
		Kind:   domain.ObsAttestationIncluded,
		At:     slotStart.Add(14 * time.Second),
		Source: domain.SourceBeaconAPI,
		Attrs: map[domain.AttrKey]string{
			domain.AttrInclusionDelay: "1",
			domain.AttrHeadCorrect:    "true",
			domain.AttrTargetCorrect:  "true",
		},
		ClockMeasured: true,
		ClockSampleAt: slotStart.Add(14 * time.Second),
	})
	if err != nil {
		t.Fatalf("NewObservation(attestation_included): %v", err)
	}
	return attesterTimeline(t,
		mustEngineTestObs(t, domain.ObsBlockSeen, slotStart.Add(500*time.Millisecond)),
		mustEngineTestObs(t, domain.ObsHeadUpdated, slotStart.Add(900*time.Millisecond)),
		mustEngineTestObs(t, domain.ObsAttestationPublished, slotStart.Add(1500*time.Millisecond)),
		included,
	)
}

// TestAnalyze_HealthyDutyCarriesNoCause covers Analyze's branch in
// isolation: the installed rule set matches only via an unconditional
// catch-all, exactly as the real R-999 does, and the outcome is ok.
func TestAnalyze_HealthyDutyCarriesNoCause(t *testing.T) {
	ordered := []Rule{
		fakeRule{id: "R-999-FAKE", match: true, verdict: &domain.Verdict{
			Cause:      domain.CauseNoRuleMatched,
			Confidence: domain.ConfidenceLow,
			Evidence: []domain.Evidence{{
				At: slotStart, Statement: "catch-all", Source: domain.SourceDerived,
			}},
		}},
	}

	v := analyze(healthyTimeline(t), DefaultConfig(), ordered)

	if v.Outcome != domain.OutcomeOK {
		t.Fatalf("Outcome = %q, want %q", v.Outcome, domain.OutcomeOK)
	}
	if v.Cause != "" {
		t.Errorf("Cause = %q, want empty — a healthy duty has nothing to attribute", v.Cause)
	}
	if v.Confidence != domain.ConfidenceHigh {
		t.Errorf("Confidence = %q, want high", v.Confidence)
	}
	if len(v.Evidence) == 0 {
		t.Error("verdict must still carry evidence (I-7)")
	}
	if len(v.Remediation) != 0 {
		t.Errorf("Remediation = %v, want none — there is nothing for the operator to fix", v.Remediation)
	}
	if v.Flags == nil || !v.Flags.AllEarned() {
		t.Errorf("Flags = %+v, want every reward flag earned", v.Flags)
	}
	completed, _ := healthyTimeline(t).First(domain.ObsCollectionCompleted)
	if !v.Evidence[0].At.Equal(completed.At) {
		t.Errorf("healthy evidence time = %s, want collection completion %s", v.Evidence[0].At, completed.At)
	}
}

// TestAnalyze_HealthyDutyThroughRealRules is the end-to-end half: the real
// rules.Order (not a fake) must actually reach that branch for a duty with
// nothing wrong, rather than some rule matching first.
func TestAnalyze_HealthyDutyThroughRealRules(t *testing.T) {
	v := Analyze(healthyTimeline(t), DefaultConfig())

	if v.Outcome != domain.OutcomeOK {
		t.Fatalf("Outcome = %q, want %q", v.Outcome, domain.OutcomeOK)
	}
	if v.Cause != "" {
		t.Errorf("Cause = %q, want empty — the real rule chain reported a healthy duty as a problem", v.Cause)
	}
	if v.IsUnknown() {
		t.Error("a healthy duty must not be reported as an unknown/taxonomy gap")
	}
}

func mustEngineTestObs(t *testing.T, kind domain.ObservationKind, at time.Time) domain.Observation {
	t.Helper()
	source := domain.SourceBeaconAPI
	if kind == domain.ObsCollectionCompleted {
		source = domain.SourceDerived
	}
	o, err := domain.NewObservation(domain.Observation{Slot: 100, Kind: kind, At: at, Source: source, ClockMeasured: true, ClockSampleAt: at})
	if err != nil {
		t.Fatalf("NewObservation: %v", err)
	}
	return o
}
