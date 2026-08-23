package exporter_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/exporter"
)

func TestRecord_LabelsCauseAndOutcome(t *testing.T) {
	e := exporter.New()
	e.Record(domain.Verdict{
		Outcome:    domain.OutcomeMissed,
		Cause:      domain.CauseP2PDegraded,
		Confidence: domain.ConfidenceMedium,
	})
	e.Record(domain.Verdict{
		Outcome:    domain.OutcomeMissed,
		Cause:      domain.CauseP2PDegraded,
		Confidence: domain.ConfidenceMedium,
	})

	body := scrape(t, e)
	if !strings.Contains(body, `whymiss_duty_verdicts_total{cause="local.p2p_degraded",outcome="missed"} 2`) {
		t.Errorf("expected 2 recorded verdicts for local.p2p_degraded/missed, got:\n%s", body)
	}
}

func TestRecord_ReportedCausePrefersSubCause(t *testing.T) {
	e := exporter.New()
	e.Record(domain.Verdict{
		Outcome:    domain.OutcomeDegraded,
		Cause:      domain.CauseELSlow,
		SubCause:   domain.CauseELSlowDiskSaturation,
		Confidence: domain.ConfidenceHigh,
	})

	body := scrape(t, e)
	if !strings.Contains(body, `whymiss_duty_verdicts_total{cause="local.el_slow.disk_saturation",outcome="degraded"} 1`) {
		t.Errorf("expected the sub-cause to be the label value, got:\n%s", body)
	}
}

func TestRecord_CauselessVerdictsUseCauseNone(t *testing.T) {
	for _, outcome := range []domain.Outcome{domain.OutcomeNoDuty, domain.OutcomeOK} {
		t.Run(string(outcome), func(t *testing.T) {
			e := exporter.New()
			e.Record(domain.Verdict{Outcome: outcome})

			body := scrape(t, e)
			want := `whymiss_duty_verdicts_total{cause="none",outcome="` + string(outcome) + `"} 1`
			if !strings.Contains(body, want) {
				t.Errorf("expected cause=\"none\" for a causeless %s verdict, got:\n%s", outcome, body)
			}
		})
	}
}

func TestHandler_ServesPrometheusTextFormat(t *testing.T) {
	e := exporter.New()
	e.Record(domain.Verdict{Outcome: domain.OutcomeOK})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/metrics", nil)
	e.Handler().ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "# HELP whymiss_duty_verdicts_total") {
		t.Errorf("missing HELP line, got:\n%s", body)
	}
	if !strings.Contains(body, "# TYPE whymiss_duty_verdicts_total counter") {
		t.Errorf("missing TYPE line, got:\n%s", body)
	}
}

func scrape(t *testing.T, e *exporter.Exporter) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), "GET", "/metrics", nil)
	e.Handler().ServeHTTP(rec, req)
	return rec.Body.String()
}
