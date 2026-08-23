package report_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/report"
)

func verdictFor(t *testing.T, draft domain.Verdict) domain.Verdict {
	t.Helper()
	draft.EngineVersion = "0.1.0"
	v, err := domain.NewVerdict(draft)
	if err != nil {
		t.Fatalf("NewVerdict: %v", err)
	}
	return v
}

func TestMarkdown_DegradedWithComparison(t *testing.T) {
	at := time.Date(2026, 8, 20, 12, 0, 2, 500_000_000, time.UTC)
	v := verdictFor(t, domain.Verdict{
		Slot:       12345678,
		Outcome:    domain.OutcomeDegraded,
		Flags:      &domain.RewardFlags{TimelySource: true, TimelyTarget: true, TimelyHead: false},
		Cause:      domain.CauseELSlow,
		SubCause:   domain.CauseELSlowDiskSaturation,
		Confidence: domain.ConfidenceHigh,
		Evidence: []domain.Evidence{{
			At:        at,
			Offset:    2500 * time.Millisecond,
			Statement: "execution client's engine_newPayloadV3 call took 2500.00ms",
			Source:    domain.SourcePromScrape,
			Comparison: &domain.Comparison{
				Label: "engine_newPayloadV3 duration", Observed: 2500, Expected: 720, Unit: domain.UnitMilliseconds,
			},
		}},
		Remediation: []string{"this box needs a faster NVMe drive"},
	})

	got := report.Markdown(v)

	for _, want := range []string{
		"Slot 12345678",
		"local.el_slow.disk_saturation",
		"degraded (lost: timely_head)",
		"high",
		"[+2.5s]",
		"execution client's engine_newPayloadV3 call took 2500.00ms",
		"2500 vs 720 ms",
		"1. this box needs a faster NVMe drive",
		"Engine 0.1.0 · Taxonomy " + domain.TaxonomyVersion,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Markdown output missing %q\n---\n%s", want, got)
		}
	}
}

func TestMarkdown_NoDuty(t *testing.T) {
	v := verdictFor(t, domain.Verdict{
		Slot:       100,
		Outcome:    domain.OutcomeNoDuty,
		Confidence: domain.ConfidenceHigh,
		Evidence: []domain.Evidence{{
			At:        time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC),
			Statement: "no attester or proposer duty was assigned for this slot",
			Source:    domain.SourceDerived,
		}},
	})

	got := report.Markdown(v)
	if !strings.Contains(got, "no duty") {
		t.Errorf("Markdown output missing headline %q\n---\n%s", "no duty", got)
	}
	if strings.Contains(got, "## Remediation") {
		t.Error("Markdown output should not render a Remediation section when there is none")
	}
}

// TestMarkdown_HealthyDuty covers the verdict rca.Analyze produces for a
// duty that went entirely right: outcome ok, no cause at all. Without a
// headline for that case the report would open with a bare "Slot 100 — ".
func TestMarkdown_HealthyDuty(t *testing.T) {
	v := verdictFor(t, domain.Verdict{
		Slot:       100,
		Outcome:    domain.OutcomeOK,
		Confidence: domain.ConfidenceHigh,
		Flags:      &domain.RewardFlags{TimelySource: true, TimelyTarget: true, TimelyHead: true},
		Evidence: []domain.Evidence{{
			At:        time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC),
			Statement: "duty fulfilled with every reward flag earned, and no rule found a problem",
			Source:    domain.SourceDerived,
		}},
	})

	got := report.Markdown(v)
	if !strings.Contains(got, "# Slot 100 — healthy\n") {
		t.Errorf("Markdown output missing healthy headline\n---\n%s", got)
	}
	if strings.Contains(got, "## Remediation") {
		t.Error("Markdown output should not render a Remediation section for a healthy duty")
	}
}

func TestMarkdown_NoFlagsUsesBareOutcome(t *testing.T) {
	v := verdictFor(t, domain.Verdict{
		Slot:       100,
		Outcome:    domain.OutcomeMissed,
		Cause:      domain.CauseProposerMissed,
		Confidence: domain.ConfidenceHigh,
		Evidence: []domain.Evidence{{
			At:        time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC),
			Statement: "no block was observed for this slot",
			Source:    domain.SourceDerived,
		}},
	})

	got := report.Markdown(v)
	if !strings.Contains(got, "**Outcome:** missed\n") {
		t.Errorf("Markdown output missing bare outcome line\n---\n%s", got)
	}
}
