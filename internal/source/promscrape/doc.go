// Package promscrape is the inbound adapter for EL/CL/VC Prometheus metrics
// endpoints (BUILD_PROMPT.md §4, task 2.2).
//
// Client-specific metric names are normalised into domain.MetricName here —
// the only place outside internal/source/registry.go allowed to know a
// client's name (I-11). internal/rca never sees a raw Prometheus metric
// name, only the normalised domain.MetricSample values this package
// produces.
//
// Every request is read-only (I-1) and timeout-bounded (I-5).
package promscrape
