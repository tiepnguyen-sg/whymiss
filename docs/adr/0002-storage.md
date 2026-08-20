# ADR-0002 · Local storage

- **Status:** accepted
- **Date:** 2026-08-20
- **Deciders:** maintainers
- **Supersedes:** —

## Context

`whymiss watch` records observations continuously and must answer
`whymiss timeline <slot>` for any slot inside its retention window. The access
pattern is append-heavy writes with occasional bounded range reads keyed by slot.

The constraints are the hard part:

- **No external database process** (I-13). An operator will not install PostgreSQL to
  diagnose a missed attestation.
- **`CGO_ENABLED=0`** (ADR-0001). This eliminates the standard SQLite binding.
- **Retention by bytes, not only by time** (I-12). A time-based cap fails exactly when
  it matters: a degraded node emits far more observations per hour, so "keep 14 days"
  can silently become tens of gigabytes during the incident the operator most wants
  recorded. Disk exhaustion on a staking box is an outage whymiss would have caused.
- **Crash safety.** The process may be killed by the OOM killer or a reboot. A
  corrupted store must not require the operator to understand its internals.
- **Queryable by the operator directly.** Trust is the product. An operator who can
  open the file and read their own data with a tool they already have will believe
  the reports; one who must take our word for it will not.

## Decision

**SQLite, accessed through `modernc.org/sqlite` — a pure-Go transpilation of the
SQLite C source — with versioned migrations and rolling retention enforced by both
age and total byte count.**

Specifics that are part of the decision, not implementation detail:

- WAL journal mode, `synchronous=NORMAL`. Durability of the last few observations is
  worth less than not competing with the node for disk I/O (I-5).
- Migrations are numbered, forward-only, and applied at startup inside a
  transaction. The schema version lives in the database.
- Retention runs on a timer and deletes oldest-first until **both** the age limit and
  the byte limit are satisfied, then reclaims space. Both limits are configuration
  with documented safe defaults.
- The store package exposes narrow, consumer-defined interfaces. `internal/rca` never
  sees it — the engine receives a fully materialised `Timeline` (ADR-0003).

## Consequences

**Good**

- Single file, no daemon, no port, no credentials. Backup is `cp`.
- The operator can run `sqlite3 whymiss.db` and audit exactly what was collected
  about their node. This directly serves the threat-model argument that convinces
  stakers to install (Phase 4).
- Transactions give crash safety for free; a half-written batch never becomes a
  half-true timeline.
- SQL range queries over `(slot, timestamp)` are the natural expression of the read
  pattern, and indexes make them cheap.
- `modernc.org/sqlite` keeps `CGO_ENABLED=0` intact, so ADR-0001 survives contact
  with persistence.

**Bad**

- `modernc.org/sqlite` is a large dependency and a transpilation rather than the
  upstream C library. It is slower than `mattn/go-sqlite3` and its bug surface is its
  own, not SQLite's. Accepted because the write volume is a few hundred rows per
  slot — three orders of magnitude below where the difference matters.
- It is a single point of dependency risk: no comparable pure-Go SQLite alternative
  exists. **Removal path:** the store is reached only through consumer-defined
  interfaces, so replacing it means writing one new implementation of those
  interfaces and a migration tool. No other package changes.
- Concurrent writers need care. Resolved by design: exactly one goroutine owns the
  write path, readers use a separate connection.

## Alternatives considered

**Flat append-only files, JSONL per day.** Zero dependencies, trivially auditable
with `grep`, and the format already exists as the corpus `observations.jsonl`.
Rejected because byte-capped retention plus point lookups by slot means writing an
index, a compaction routine, and a crash-recovery path — a small database with none
of SQLite's twenty years of testing. Reconsider only if `modernc.org/sqlite` proves
unacceptable in the soak test.

**bbolt / Badger / Pebble.** Pure Go, embedded, good write throughput. Rejected on
the auditability requirement: the operator cannot inspect the file with a tool they
already have, and key-value ranges force us to hand-roll the slot queries SQL gives
us. Badger and Pebble additionally have memory profiles that fight I-12 on a Pi.

**DuckDB.** Excellent for the analytical shape of `make eval`. Rejected: CGO, and a
footprint aimed at analytics workstations rather than a staking box.

**In-memory only.** Simplest, and sufficient for `whymiss <slot>` on a recent slot.
Rejected because the product's value is post-mortem — the operator investigates
hours after the miss, often after restarting things.
