// Package rca is whymiss's root-cause-analysis engine — the product itself
// (BUILD_PROMPT.md §11, "everything before was plumbing; everything after
// is packaging").
//
// Analyze is a pure function: no I/O, no clock reads, no randomness, no
// goroutines (I-6, ADR-0003). It may import only the standard library and
// internal/domain — enforced by .golangci.yml's depguard rca-purity rule
// and belt-and-braces by `make check.purity`. Every fact a rule could ever
// need must already be on the domain.Timeline it's given; a rule that finds
// itself wanting to fetch something is a bug in internal/timeline, not a
// license to add an import here.
package rca
