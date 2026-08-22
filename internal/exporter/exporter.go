package exporter

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// causeNone is the cause label value recorded for domain.OutcomeNoDuty,
// where domain.Verdict.ReportedCause() returns "" (the taxonomy has no
// cause ID for "nothing was owed") — an empty Prometheus label value is
// legal but confusing to write an alert against, so it's spelled out.
const causeNone = "none"

// Exporter turns Record calls into a Prometheus counter, one time series
// per (cause, outcome) pair — see docs/adr/0009-prometheus-exporter.md for
// why this is the only metric and why the label set is bounded at 19 × 4
// possible combinations regardless of how many validators or how long a
// whymiss process runs.
type Exporter struct {
	registry *prometheus.Registry
	verdicts *prometheus.CounterVec
}

// New builds an Exporter with its own registry — never the global default
// registry, so callers (and tests) never share state with anything else
// that might register Prometheus collectors in the same process.
func New() *Exporter {
	registry := prometheus.NewRegistry()
	verdicts := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "whymiss_duty_verdicts_total",
		Help: "Count of RCA verdicts by cause and outcome. cause is bounded to domain.CauseIDs() plus \"none\" (no duty); outcome to domain.Outcome's four values — see docs/adr/0009-prometheus-exporter.md.",
	}, []string{"cause", "outcome"})
	registry.MustRegister(verdicts)
	return &Exporter{registry: registry, verdicts: verdicts}
}

// Record increments the counter for v's (cause, outcome) pair.
func (e *Exporter) Record(v domain.Verdict) {
	cause := string(v.ReportedCause())
	if cause == "" {
		cause = causeNone
	}
	e.verdicts.WithLabelValues(cause, string(v.Outcome)).Inc()
}

// Handler serves the registry's metrics in Prometheus text exposition
// format — mount it at /metrics.
func (e *Exporter) Handler() http.Handler {
	return promhttp.HandlerFor(e.registry, promhttp.HandlerOpts{})
}
