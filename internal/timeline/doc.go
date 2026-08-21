// Package timeline assembles the observations and samples internal/source's
// adapters produce into a domain.Timeline for a slot (BUILD_PROMPT.md §4,
// task 2.6).
//
// This package does no I/O and holds no opinion about what a fact means —
// internal/rca (Phase 3) is the only place that reasons about causes.
// Assembler's entire job is turning a stream of arbitrarily-ordered,
// arbitrarily-sourced values into the deterministically-ordered,
// slot-scoped domain.Timeline domain.NewTimeline requires.
package timeline
