// Package store is whymiss's persistence layer: a single SQLite file, no
// external database process, queryable by the operator with a tool they
// already have (BUILD_PROMPT.md §4, task 2.5; ADR-0002, ADR-0007).
//
// Store exposes narrow, consumer-defined methods, not a generic query
// interface — internal/rca never sees this package at all (ADR-0002: the
// engine receives a fully materialised domain.Timeline). Retention runs by
// both age and byte count (I-12): a degraded node emits far more
// observations per hour, so an age-only cap can silently balloon during
// the exact incident an operator most wants recorded.
package store
