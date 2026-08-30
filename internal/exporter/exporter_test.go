package exporter_test

import (
	"context"
	"net/http"
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

// TestVerdictSeriesCardinalityIsBounded enforces the bound ADR-0009 states and
// Phase 4's DoD requires ("Prometheus `cause` label cardinality is bounded and
// documented"). The bound was documented and arithmetic-checked but nothing held
// it: a cause added to the taxonomy, or a `sub_cause` promoted to its own label,
// would have widened the operator's time-series count with no test objecting.
//
// The assertion is behavioural rather than arithmetic. It drives Record with
// every (cause, outcome) pair the domain can produce — including the empty cause
// that becomes "none" — and then counts the series the registry actually
// exposes, so it fails for a widened label set and for a new label alike.
func TestVerdictSeriesCardinalityIsBounded(t *testing.T) {
	t.Parallel()

	// 19 cause IDs plus "none", times the four outcomes. Spelled out rather than
	// recomputed from the same expressions the code uses, so changing the
	// taxonomy forces a deliberate decision here instead of silently moving the
	// number this test claims to pin. It did exactly that when
	// network.payload_late was added: 19 -> 20 values, 76 -> 80 series.
	const wantCauseValues, wantOutcomes, wantBound = 20, 4, 80

	if got := len(domain.CauseIDs()) + 1; got != wantCauseValues {
		t.Fatalf("cause label can take %d values, ADR-0009 documents %d — update the ADR and this test together", got, wantCauseValues)
	}

	outcomes := []domain.Outcome{domain.OutcomeNoDuty, domain.OutcomeOK, domain.OutcomeDegraded, domain.OutcomeMissed}
	if len(outcomes) != wantOutcomes {
		t.Fatalf("outcome set is %d values, ADR-0009 documents %d", len(outcomes), wantOutcomes)
	}

	e := exporter.New()
	for _, cause := range append([]domain.CauseID{""}, domain.CauseIDs()...) {
		for _, outcome := range outcomes {
			e.Record(domain.Verdict{Cause: cause, Outcome: outcome})
		}
	}

	series := countSeries(t, e, "whymiss_duty_verdicts_total")
	if series > wantBound {
		t.Errorf("exporter emitted %d series for whymiss_duty_verdicts_total, ADR-0009 bounds it at %d", series, wantBound)
	}
	t.Logf("emitted %d of the %d series the bound allows", series, wantBound)
}

// countSeries scrapes the exporter's own handler and counts the sample lines for
// one metric, which is what an operator's Prometheus would actually store.
func countSeries(t *testing.T, e *exporter.Exporter, metric string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	e.Handler().ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics handler returned %d", rec.Code)
	}
	n := 0
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if strings.HasPrefix(line, metric+"{") {
			n++
		}
	}
	return n
}
