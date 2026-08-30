# ADR-0009 · `prometheus/client_golang` for the metrics exporter

- **Status:** accepted
- **Date:** 2026-08-22
- **Deciders:** maintainers
- **Supersedes:** —

## Context

BUILD_PROMPT §3 locks `prometheus/client_golang` for metrics — "what
operators already run" — the pre-approved choice under ADR-0004's "each
still lands in its own ADR when first added." Task 4.1 is the first code
that needs it: `internal/exporter`, exposing `domain.Verdict` outcomes as
Prometheus metrics, called out in BUILD_PROMPT §12.2 as Phase 4's
"headline feature — alert on causes, not symptoms."

## Decision

**`github.com/prometheus/client_golang`**, specifically the `prometheus`
package for the metric type and a dedicated `*prometheus.Registry` (not
the global default registry — `internal/exporter` owns its own registry
so it stays independently testable and never accumulates metrics no
caller registered) and `promhttp` for the `/metrics` HTTP handler.

**One metric**: `whymiss_duty_verdicts_total`, a `CounterVec` labelled
`cause` and `outcome`. `cause` is `string(domain.Verdict.ReportedCause())`
(the sub-cause where one matched, otherwise the cause — matches how a
human reads a verdict), or the literal string `"none"` when
`Outcome == OutcomeNoDuty` (the taxonomy has no cause ID for "nothing was
owed," and `ReportedCause()` returns `""` there — an empty Prometheus
label value is legal but confusing to alert on, so it's spelled out).

**Cardinality bound**, per BUILD_PROMPT §12.3's DoD ("Prometheus `cause`
label cardinality is bounded and documented"): `cause` has exactly
`len(domain.CauseIDs())` + 1 = 20 possible values (`domain.CauseIDs()` is
a closed, compile-time-fixed slice — see `internal/domain/verdict.go`);
`outcome` has exactly 4 (`domain.Outcome`'s closed set). 20 × 4 = 80
label combinations at most, and most of those never actually occur (e.g.
`outcome="no_duty"` only ever pairs with `cause="none"`) — one whymiss
process watching any number of validators still emits at most 80 time
series for this metric, never growing with validator count or wall-clock
time.

`sub_cause` is deliberately **not** a separate label — folding it into
`cause` via `ReportedCause()` keeps a second, more-specific-but-lower-
cardinality axis from doubling the bound for a distinction task 4.1's own
framing ("alert on causes, not symptoms") doesn't ask an operator to
alert on separately.

## Consequences

**Good**

- `promhttp.HandlerFor` is a battle-tested, spec-correct exposition-format
  implementation — no reason to hand-roll `# HELP`/`# TYPE`/metric-line
  formatting for a wire format this widely consumed.
- A dedicated `*prometheus.Registry` per `*exporter.Exporter` means
  `internal/exporter`'s tests construct one, record into it, and assert on
  `Handler()`'s output with zero global state to reset between tests.

**Bad**

- One more direct dependency against ADR-0004's fewer-than-15 budget —
  already budgeted for in BUILD_PROMPT §3.
- `client_golang` pulls in `beorn7/perks`, `cespare/xxhash/v2`,
  `prometheus/client_model`, `prometheus/common`, `prometheus/procfs`, and
  `google.golang.org/protobuf` as transitive dependencies. Reviewed and
  accepted: this is the standard, expected dependency footprint of the
  locked choice — every one of these ships inside `client_golang` itself
  for any Go binary exposing Prometheus metrics, not something specific to
  this project's usage of it.

**Removal path.** Confined to `internal/exporter` and the HTTP-server
wiring in `internal/app/watch.go`'s `MetricsAddr` branch. `internal/rca`
never imports it (purity, I-6) — `internal/exporter.Exporter.Record`
takes a `domain.Verdict` value, the only coupling point.

## Alternatives considered

**Hand-rolled text exposition format.** No new dependency, and the format
itself is simple enough (`# HELP`, `# TYPE`, `name{labels} value`) for one
counter. Rejected: BUILD_PROMPT §3 already locked `client_golang` by name
for exactly this task, and reopening a locked technical decision needs a
stop-and-ask per §8, not a unilateral "smaller diff" call — especially
since the bound cardinality here means the dependency's actual runtime
cost (memory, registration overhead) is negligible regardless.

**OpenTelemetry metrics with a Prometheus exporter shim.** Heavier
dependency surface for a project emitting exactly one counter to one
consumer (Prometheus scrape); no other telemetry backend is in scope
anywhere in BUILD_PROMPT. Rejected as disproportionate to the actual need.
