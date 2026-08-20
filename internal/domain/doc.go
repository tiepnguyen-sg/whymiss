// Package domain defines the vocabulary of the product: the facts whymiss records
// about a slot, and the verdict it reaches about them.
//
// The package is pure. It imports only the Go standard library, holds no state, and
// performs no I/O — a constraint enforced by depguard in .golangci.yml and by
// `make check.purity`. Together with [internal/rca] it constitutes the product;
// everything else in the repository feeds it or renders its output (I-6, ADR-0003).
//
// # Frozen after Phase 1
//
// Every corpus scenario in test/corpus is recorded against these types, so a change
// here invalidates the accuracy benchmark. Changing an exported type requires
// maintainer agreement before the work starts, not at review time.
//
// # Construction is validation
//
// Values arrive from adapters that talk to a beacon node, a metrics endpoint, and the
// host, none of which are trustworthy in the shape they return. Every type in this
// package therefore has a constructor that rejects a malformed value, and the zero
// value of a type is never meaningful. Prefer NewObservation, NewTimeline, and
// NewVerdict over composite literals outside tests.
//
// The taxonomy in docs/causes.md is the contract these types encode. Cause IDs, the
// observation vocabulary, and the attribute keys are closed sets defined there and
// mirrored here; taxonomy_test.go asserts the two have not drifted.
package domain
