// Package exporter turns domain.Verdict values into Prometheus metrics —
// the "alert on causes, not symptoms" surface BUILD_PROMPT.md §12.2 calls
// Phase 4's headline feature. See docs/adr/0009-prometheus-exporter.md for
// the metric name, label set, and cardinality bound.
package exporter
