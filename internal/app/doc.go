// Package app is whymiss's composition root: the only package that wires a
// concrete internal/source adapter, internal/store, and (from Phase 3)
// internal/rca engine together (BUILD_PROMPT.md §4). Every other package
// depends on interfaces it defines itself, never on this one.
//
// cmd/whymiss stays thin (parse, wire, run, exit code) by delegating each
// command's real work to a function here.
package app
