package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func evidence() domain.Evidence {
	return domain.Evidence{
		At:        at.Add(3900 * time.Millisecond),
		Offset:    3900 * time.Millisecond,
		Statement: "engine_newPayload takes 2.78s, 11x the rolling p99",
		Source:    domain.SourcePromScrape,
		Comparison: &domain.Comparison{
			Label: "engine_newPayload duration", Observed: 2780, Expected: 240,
			Unit: domain.UnitMilliseconds,
		},
	}
}

func validVerdict() domain.Verdict {
	return domain.Verdict{
		Slot:          100,
		Outcome:       domain.OutcomeMissed,
		Cause:         domain.CauseELSlow,
		SubCause:      domain.CauseELSlowDiskSaturation,
		Confidence:    domain.ConfidenceHigh,
		Evidence:      []domain.Evidence{evidence()},
		Remediation:   []string{"replace the consumer SATA SSD with an NVMe drive"},
		EngineVersion: "0.1.0",
	}
}

func TestNewVerdictValid(t *testing.T) {
	t.Parallel()

	got, err := domain.NewVerdict(validVerdict())
	if err != nil {
		t.Fatalf("NewVerdict() error = %v, want nil", err)
	}
	if got.TaxonomyVersion != domain.TaxonomyVersion {
		t.Errorf("TaxonomyVersion = %q, want it stamped as %q", got.TaxonomyVersion, domain.TaxonomyVersion)
	}
	if got.ReportedCause() != domain.CauseELSlowDiskSaturation {
		t.Errorf("ReportedCause() = %q, want the sub-cause", got.ReportedCause())
	}
}

// TestNewVerdictRequiresEvidence is I-7 stated as a test: a verdict with nothing
// behind it must not be constructible, which is what makes an honest unknown the
// cheaper path for a rule author.
func TestNewVerdictRequiresEvidence(t *testing.T) {
	t.Parallel()

	for _, evs := range [][]domain.Evidence{nil, {}} {
		v := validVerdict()
		v.Evidence = evs

		if _, err := domain.NewVerdict(v); !errors.Is(err, domain.ErrNoEvidence) {
			t.Fatalf("NewVerdict() error = %v, want %v", err, domain.ErrNoEvidence)
		}
	}
}

func TestNewVerdictRejectsCauses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*domain.Verdict)
		want   error
	}{
		{
			name:   "cause outside the taxonomy",
			mutate: func(v *domain.Verdict) { v.Cause, v.SubCause = "local.el_broken", "" },
			want:   domain.ErrInvalidCause,
		},
		{
			name:   "sub-cause outside the taxonomy",
			mutate: func(v *domain.Verdict) { v.SubCause = "local.el_slow.compaction" },
			want:   domain.ErrInvalidCause,
		},
		{
			name:   "sub-cause of a different parent",
			mutate: func(v *domain.Verdict) { v.Cause = domain.CauseCLSlow },
			want:   domain.ErrInvalidCause,
		},
		{
			name: "no_duty may not carry a cause",
			mutate: func(v *domain.Verdict) {
				v.Outcome, v.SubCause = domain.OutcomeNoDuty, ""
			},
			want: domain.ErrInvalidCause,
		},
		{
			name:   "empty cause on a real outcome",
			mutate: func(v *domain.Verdict) { v.Cause, v.SubCause = "", "" },
			want:   domain.ErrInvalidCause,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			v := validVerdict()
			tc.mutate(&v)

			if _, err := domain.NewVerdict(v); !errors.Is(err, tc.want) {
				t.Fatalf("NewVerdict() error = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestNewVerdictRejectsContradictoryFlags covers the case that would mislead an
// operator most directly: a verdict whose outcome and reward flags disagree about
// whether anything was actually lost.
func TestNewVerdictRejectsContradictoryFlags(t *testing.T) {
	t.Parallel()

	all := domain.RewardFlags{TimelySource: true, TimelyTarget: true, TimelyHead: true}
	none := domain.RewardFlags{}
	headLost := domain.RewardFlags{TimelySource: true, TimelyTarget: true}

	tests := []struct {
		name    string
		outcome domain.Outcome
		flags   *domain.RewardFlags
		wantErr bool
	}{
		{"ok with every flag earned", domain.OutcomeOK, &all, false},
		{"ok with a flag lost", domain.OutcomeOK, &headLost, true},
		{"degraded with a flag lost", domain.OutcomeDegraded, &headLost, false},
		{"degraded with every flag earned", domain.OutcomeDegraded, &all, true},
		{"degraded with no flag earned is missed", domain.OutcomeDegraded, &none, true},
		{"degraded without flags", domain.OutcomeDegraded, nil, true},
		{"missed with no flag earned", domain.OutcomeMissed, &none, false},
		{"missed carrying an earned flag", domain.OutcomeMissed, &headLost, true},
		{"missed without flags", domain.OutcomeMissed, nil, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			v := validVerdict()
			v.Outcome, v.Flags = tc.outcome, tc.flags
			if tc.outcome == domain.OutcomeOK {
				v.Cause, v.SubCause = domain.CauseNoRuleMatched, ""
			}

			_, err := domain.NewVerdict(v)
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("NewVerdict() error = %v, want error = %v", err, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, domain.ErrInvalidOutcome) {
				t.Fatalf("NewVerdict() error = %v, want %v", err, domain.ErrInvalidOutcome)
			}
		})
	}
}

func TestNewVerdictNoDuty(t *testing.T) {
	t.Parallel()

	v := domain.Verdict{
		Slot:       100,
		Outcome:    domain.OutcomeNoDuty,
		Confidence: domain.ConfidenceHigh,
		Evidence: []domain.Evidence{{
			At: at, Statement: "no attester or proposer duty is assigned for this slot",
			Source: domain.SourceBeaconAPI,
		}},
		EngineVersion: "0.1.0",
	}

	got, err := domain.NewVerdict(v)
	if err != nil {
		t.Fatalf("NewVerdict() error = %v, want nil", err)
	}
	if got.Cause != "" {
		t.Errorf("Cause = %q, want empty — the taxonomy has no id for no_duty", got.Cause)
	}
	if got.IsUnknown() {
		t.Error("IsUnknown() = true for no_duty, want false — nothing was owed, nothing is unexplained")
	}
}

func TestNewVerdictRejectsMissingVersions(t *testing.T) {
	t.Parallel()

	v := validVerdict()
	v.EngineVersion = ""

	if _, err := domain.NewVerdict(v); !errors.Is(err, domain.ErrMissingVersion) {
		t.Fatalf("NewVerdict() error = %v, want %v", err, domain.ErrMissingVersion)
	}
}

func TestNewVerdictRejectsBadEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*domain.Evidence)
		want   error
	}{
		{"blank statement", func(e *domain.Evidence) { e.Statement = "   " }, domain.ErrEmptyStatement},
		{"zero timestamp", func(e *domain.Evidence) { e.At = time.Time{} }, domain.ErrMissingTimestamp},
		{
			"timestamp not in utc",
			func(e *domain.Evidence) { e.At = at.In(time.FixedZone("CET", 3600)) },
			domain.ErrNotUTC,
		},
		{"unattributed", func(e *domain.Evidence) { e.Source = "" }, domain.ErrMissingSource},
		{
			"comparison without a label",
			func(e *domain.Evidence) { e.Comparison.Label = "" },
			domain.ErrEmptyStatement,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ev := evidence()
			comparison := *ev.Comparison
			ev.Comparison = &comparison
			tc.mutate(&ev)

			v := validVerdict()
			v.Evidence = []domain.Evidence{ev}

			if _, err := domain.NewVerdict(v); !errors.Is(err, tc.want) {
				t.Fatalf("NewVerdict() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestNewVerdictCopies(t *testing.T) {
	t.Parallel()

	draft := validVerdict()
	v, err := domain.NewVerdict(draft)
	if err != nil {
		t.Fatalf("NewVerdict() error = %v", err)
	}

	draft.Evidence[0].Statement = "rewritten after the fact"
	draft.Remediation[0] = "rewritten after the fact"

	if v.Evidence[0].Statement == "rewritten after the fact" {
		t.Error("evidence statement changed after the caller mutated the draft")
	}
	if v.Remediation[0] == "rewritten after the fact" {
		t.Error("remediation changed after the caller mutated the draft")
	}
}

func TestCauseHierarchy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		child, parent domain.CauseID
		want          bool
	}{
		{domain.CauseELSlowSnapshot, domain.CauseELSlow, true},
		{domain.CauseELSlowDiskSaturation, domain.CauseELSlow, true},
		{domain.CauseELSlow, domain.CauseELSlow, false},
		{domain.CauseELSlow, domain.CauseELSlowSnapshot, false},
		{domain.CauseCLSlow, domain.CauseELSlow, false},
		{domain.CauseHostDiskIO, "local.host", true},
		{domain.CauseELSlowSnapshot, "", false},
	}

	for _, tc := range tests {
		t.Run(string(tc.child)+" under "+string(tc.parent), func(t *testing.T) {
			t.Parallel()

			if got := tc.child.IsSubCauseOf(tc.parent); got != tc.want {
				t.Errorf("IsSubCauseOf() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRewardFlagsLost(t *testing.T) {
	t.Parallel()

	flags := domain.RewardFlags{TimelySource: true}
	want := []string{"timely_target", "timely_head"}

	got := flags.Lost()
	if len(got) != len(want) {
		t.Fatalf("Lost() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Lost()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if flags.AllEarned() {
		t.Error("AllEarned() = true, want false")
	}
	if !flags.AnyEarned() {
		t.Error("AnyEarned() = false, want true")
	}
}

func TestVerdictIsUnknown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		cause domain.CauseID
		want  bool
	}{
		{domain.CauseInsufficientData, true},
		{domain.CauseNoRuleMatched, true},
		{domain.CauseELSlow, false},
		{domain.CauseProposerMissed, false},
	}

	for _, tc := range tests {
		t.Run(string(tc.cause), func(t *testing.T) {
			t.Parallel()

			v := domain.Verdict{Cause: tc.cause}
			if got := v.IsUnknown(); got != tc.want {
				t.Errorf("IsUnknown() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOutcomeValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		o    domain.Outcome
		want bool
	}{
		{domain.OutcomeNoDuty, true},
		{domain.OutcomeOK, true},
		{domain.OutcomeDegraded, true},
		{domain.OutcomeMissed, true},
		{"partial", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := tc.o.Valid(); got != tc.want {
			t.Errorf("Outcome(%q).Valid() = %v, want %v", tc.o, got, tc.want)
		}
	}
}

func TestConfidenceValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		c    domain.Confidence
		want bool
	}{
		{domain.ConfidenceHigh, true},
		{domain.ConfidenceMedium, true},
		{domain.ConfidenceLow, true},
		{"certain", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := tc.c.Valid(); got != tc.want {
			t.Errorf("Confidence(%q).Valid() = %v, want %v", tc.c, got, tc.want)
		}
	}
}

func TestUnitValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		u    domain.Unit
		want bool
	}{
		{domain.UnitMilliseconds, true},
		{domain.UnitPercent, true},
		{domain.UnitCount, true},
		{domain.UnitRatio, true},
		{"seconds", false},
	}
	for _, tc := range tests {
		if got := tc.u.Valid(); got != tc.want {
			t.Errorf("Unit(%q).Valid() = %v, want %v", tc.u, got, tc.want)
		}
	}
}

func TestReportedCauseFallsBackToCause(t *testing.T) {
	t.Parallel()

	v := domain.Verdict{Cause: domain.CauseELSlow}
	if got := v.ReportedCause(); got != domain.CauseELSlow {
		t.Errorf("ReportedCause() = %q, want %q (no sub-cause set)", got, domain.CauseELSlow)
	}
}

func TestComparisonValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		c       domain.Comparison
		wantErr bool
	}{
		{"valid", domain.Comparison{Label: "iowait", Observed: 23.4, Expected: 20, Unit: domain.UnitPercent}, false},
		{"blank label", domain.Comparison{Label: "  ", Unit: domain.UnitPercent}, true},
		{"unknown unit", domain.Comparison{Label: "iowait", Unit: "seconds"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.c.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// TestRewardFlagsLostAllLost exercises the branch TestRewardFlagsLost above leaves
// untested: TimelySource itself unearned.
func TestRewardFlagsLostAllLost(t *testing.T) {
	t.Parallel()

	got := domain.RewardFlags{}.Lost()
	want := []string{"timely_source", "timely_target", "timely_head"}
	if len(got) != len(want) {
		t.Fatalf("Lost() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Lost()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNewVerdictRejectsInvalidConfidence(t *testing.T) {
	t.Parallel()

	v := validVerdict()
	v.Confidence = "certain"

	if _, err := domain.NewVerdict(v); !errors.Is(err, domain.ErrInvalidConfidence) {
		t.Fatalf("NewVerdict() error = %v, want %v", err, domain.ErrInvalidConfidence)
	}
}

func TestNewVerdictRejectsSubCauseWithoutParentValidCause(t *testing.T) {
	t.Parallel()

	// Invalid top-level cause, and a sub-cause too — validateCause must fail on the
	// parent before ever inspecting the sub-cause.
	v := validVerdict()
	v.Cause = "local.not_real"

	if _, err := domain.NewVerdict(v); !errors.Is(err, domain.ErrInvalidCause) {
		t.Fatalf("NewVerdict() error = %v, want %v", err, domain.ErrInvalidCause)
	}
}

// TestVerdictValidateEmptyCauseByOutcome pins down which outcomes may carry
// no cause at all. no_duty must (nothing was owed) and ok may (nothing went
// wrong to attribute) — but ok is permissive, not mandatory: a rule can
// still legitimately match on a duty that ended up ok, e.g. a validator
// client that was measurably slow yet beat the deadline.
func TestVerdictValidateEmptyCauseByOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		outcome  domain.Outcome
		cause    domain.CauseID
		subCause domain.CauseID
		flags    *domain.RewardFlags
		wantErr  bool
	}{
		{"ok without a cause", domain.OutcomeOK, "", "", nil, false},
		{"ok with a real cause", domain.OutcomeOK, domain.CauseVCSlow, "", nil, false},
		{"ok without a cause but with a sub-cause", domain.OutcomeOK, "", domain.CauseELSlowPruning, nil, true},
		{"degraded without a cause", domain.OutcomeDegraded, "", "", &domain.RewardFlags{TimelySource: true}, true},
		{"missed without a cause", domain.OutcomeMissed, "", "", nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			v := validVerdict()
			v.Outcome = tc.outcome
			v.Cause = tc.cause
			v.SubCause = tc.subCause
			v.Flags = tc.flags

			// NewVerdict, not Validate: it stamps TaxonomyVersion, which
			// validVerdict deliberately leaves empty, so the only error
			// this can surface is the cause rule under test.
			_, err := domain.NewVerdict(v)
			if (err != nil) != tc.wantErr {
				t.Errorf("NewVerdict() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr && err != nil && !errors.Is(err, domain.ErrInvalidCause) {
				t.Errorf("NewVerdict() error = %v, want %v", err, domain.ErrInvalidCause)
			}
		})
	}
}

func TestVerdictValidateRejectsInvalidOutcome(t *testing.T) {
	t.Parallel()

	v := validVerdict()
	v.Outcome = "partial"

	if _, err := domain.NewVerdict(v); !errors.Is(err, domain.ErrInvalidOutcome) {
		t.Fatalf("NewVerdict() error = %v, want %v", err, domain.ErrInvalidOutcome)
	}
}

// TestVerdictValidateRejectsMissingTaxonomyVersion calls Validate directly rather
// than through NewVerdict, because NewVerdict stamps the taxonomy version
// automatically — the only way to observe this branch is a verdict built by hand,
// which is exactly the shape a bug in some future caller might take.
func TestVerdictValidateRejectsMissingTaxonomyVersion(t *testing.T) {
	t.Parallel()

	v := validVerdict()
	v.TaxonomyVersion = ""

	if err := v.Validate(); !errors.Is(err, domain.ErrMissingVersion) {
		t.Fatalf("Validate() error = %v, want %v", err, domain.ErrMissingVersion)
	}
}

func TestNewVerdictRejectsNoDutyWithSubCauseOnly(t *testing.T) {
	t.Parallel()

	v := validVerdict()
	v.Outcome, v.Cause = domain.OutcomeNoDuty, ""
	// SubCause is left set from validVerdict() — Cause alone was cleared.

	if _, err := domain.NewVerdict(v); !errors.Is(err, domain.ErrInvalidCause) {
		t.Fatalf("NewVerdict() error = %v, want %v", err, domain.ErrInvalidCause)
	}
}

func TestNewVerdictRejectsNoDutyWithFlags(t *testing.T) {
	t.Parallel()

	flags := domain.RewardFlags{TimelySource: true, TimelyTarget: true, TimelyHead: true}
	v := validVerdict()
	v.Outcome, v.Cause, v.SubCause, v.Flags = domain.OutcomeNoDuty, "", "", &flags

	if _, err := domain.NewVerdict(v); !errors.Is(err, domain.ErrInvalidOutcome) {
		t.Fatalf("NewVerdict() error = %v, want %v", err, domain.ErrInvalidOutcome)
	}
}
