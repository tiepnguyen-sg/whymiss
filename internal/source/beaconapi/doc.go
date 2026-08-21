// Package beaconapi is the inbound adapter for a beacon node's standard HTTP
// API and SSE event stream (BUILD_PROMPT.md §4, task 2.1).
//
// Everything here talks to the *standard* Beacon API
// (github.com/ethereum/beacon-APIs) only — no client-specific endpoint. Client
// differences (Lighthouse vs. Prysm) live in internal/source/promscrape and
// internal/source/registry.go instead (I-11): this package does not know or
// care which client it is talking to.
//
// Every request is read-only (I-1), carries a bounded timeout, and is rate
// limited with backoff on failure (I-5) — see [Client] and [backoffCeiling].
// A caller feeds the resulting values to internal/timeline, which is what
// turns them into a domain.Timeline; this package produces domain.Observation
// and duty values, not verdicts.
package beaconapi
