# ADR-0007 · SQLite store implementation

- **Status:** accepted
- **Date:** 2026-08-21
- **Deciders:** maintainers
- **Supersedes:** —

## Context

ADR-0002 already decided the storage engine — SQLite via `modernc.org/sqlite`,
WAL mode, retention by age and bytes — as part of Phase 1's architecture
planning. ADR-0004's dependency policy requires every dependency in
BUILD_PROMPT §3's pre-approved list to still get its own ADR "when first
added, recording the version and the removal path." This is that ADR:
`internal/store` (task 2.5) is the first code to actually import
`modernc.org/sqlite`.

## Decision

**`modernc.org/sqlite v1.57.0`.**

Everything about *why* SQLite and *why the pure-Go driver* is ADR-0002's
decision, not relitigated here. What this ADR adds:

- Pinned at `v1.57.0` (latest stable at the time `internal/store` was
  written). Bumped only with a `go.sum` diff reviewed like any other
  dependency change (ADR-0004 rule 6).
- `internal/store` is the only package that imports it. Every other package
  reaches storage through the narrow interfaces `internal/store` exposes,
  per ADR-0002's "the engine receives a fully materialised Timeline."

## Consequences

**Good**

- `CGO_ENABLED=0` survives contact with persistence (ADR-0001).
- No second SQLite ADR is needed later — this one already recorded the
  version and removal path ADR-0004 requires.

**Bad**

- One more line against the fewer-than-15-direct-dependencies budget
  (ADR-0004). Already budgeted for in ADR-0002; this ADR is bookkeeping,
  not a new decision to weigh.

**Removal path.** Unchanged from ADR-0002: `internal/store`'s consumer-defined
interfaces are the only surface other packages see, so replacing the driver
means a new implementation of those interfaces plus a migration tool — no
other package changes.

## Alternatives considered

See ADR-0002 — the engine-selection trade-offs (bbolt/Badger/Pebble, flat
JSONL, DuckDB, in-memory) were already weighed there and are not reopened
here.
