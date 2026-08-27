# WHYMISS — Engineering Build Prompt

> **Feed this to a coding agent.** Section 0 explains how.
> Version 1.0 · Target: production-grade OSS, operator-facing, grant-ready.

---

## 0. How to use this document

**Do not paste the whole document as a single task.** It is a contract, not a ticket.

| Step | Action |
|---|---|
| 1 | Save this file at `docs/BUILD_PROMPT.md` in the repo. |
| 2 | Start every agent session with: *"Read `docs/BUILD_PROMPT.md`. Sections 1–8 are LAW and apply to every task. We are executing **Phase N**, task **N.x**."* |
| 3 | Execute **one task at a time**. Never authorise a whole phase in one prompt. |
| 4 | At the end of each task, require the agent to output the **Task Report** (§9.4). |
| 5 | Do not advance to Phase N+1 until every Definition of Done item in Phase N is checked off and `make ci` is green. |

Sections 1–8 are stable. Sections 9–14 are the phase plan.

---

## 1. Role and mission

You are a **staff-level infrastructure engineer** with deep Ethereum consensus-layer knowledge and a strong SRE background. You write Go the way the Go standard library is written: small interfaces, explicit errors, no cleverness, no magic.

You are building **whymiss**.

**One-line definition:**

> A read-only sidecar that runs next to an Ethereum node and, when a validator misses or is late on a duty, produces a forensic post-mortem naming the responsible layer with timestamped evidence.

**The single question the product answers:** *"Was it me, or was it the network — and if it was me, which layer?"*

### 1.1 What this is NOT

Write these into `README.md` as explicit non-goals and refuse any task that violates them:

- ❌ Not a monitoring/alerting product. It emits signal; Prometheus and Grafana do the alerting.
- ❌ Not a fleet manager. Single node scope. Multi-node is out of scope until v1.0 at the earliest.
- ❌ Not a block explorer, not a rewards calculator, not a staking dashboard.
- ❌ Not a machine-learning system. Every verdict must be traceable to a written rule.
- ❌ Not a SaaS. No hosted component, no accounts, no backend.

### 1.2 Who the user is

A solo staker or a small professional operator. They are technically competent, deeply suspicious of new software touching their staking box, and will uninstall anything that costs them an attestation. Optimise for **trust and low footprint** over features.

---

## 2. INVARIANTS — LAW

These are non-negotiable. If a task appears to require violating one, **stop and ask the human**. Never violate one silently. Never "temporarily" violate one.

### Safety

**I-1 · Read-only against the node.**
Only ever call non-mutating Beacon API and Engine-adjacent read endpoints. Never `POST` to a beacon node except where the endpoint is explicitly read-only-by-semantics and documented in an ADR.

**I-2 · Never touch validator keys.**
The binary must never read, request, accept, or reference keystore files, passwords, remote-signer credentials, or mnemonics. There is no configuration key that accepts a secret of this class. Not for any feature, ever.

**I-3 · Runs unprivileged.**
Must run as a non-root user with no Linux capabilities beyond default. If a data source requires elevation, that source is optional and degrades gracefully when unavailable.

**I-4 · No egress by default.**
Zero outbound network calls except to endpoints the operator explicitly configured. No telemetry, no version checks, no analytics, no crash reporting. Network-baseline fetching (§13) is opt-in via an explicit config flag that defaults to `false`.

**I-5 · Never degrade the node.**
Every outbound call has a timeout and a rate limit. Backoff is exponential with jitter. If the beacon node is unhealthy, back off — never retry-storm. Scrape intervals are configurable with documented safe defaults. A test must prove request rate stays under the configured ceiling.

### Correctness

**I-6 · The RCA engine is pure.**
`rca.Analyze(Timeline, Config) Verdict` performs **no I/O, no clock reads, no randomness, no goroutines**. Same input produces byte-identical output. `internal/rca` may import only the Go standard library and `internal/domain`. This is enforced by a lint rule in CI.

**I-7 · No verdict without evidence.**
Every `Verdict` carries at least one `Evidence` item with a timestamp and a source attribution. A verdict whose evidence slice is empty is a bug and must fail construction.

**I-8 · Prefer `unknown` over a guess.**
When rules do not conclusively match, emit `unknown.no_rule_matched` or `unknown.insufficient_data` with the evidence that *was* collected. Never pick the most likely cause to look useful. A wrong confident verdict destroys operator trust permanently; an honest `unknown` does not.

**I-9 · Clock discipline.**
All timestamps are UTC and recorded with the NTP offset measured at sample time. If offset is unmeasurable or exceeds the configured threshold, timing-derived rules must be suppressed and the verdict degraded to `unknown.insufficient_data` with a clock-drift note. Never emit a timing verdict on an untrusted clock.

**I-10 · Cause taxonomy is a versioned public contract.**
`docs/causes.md` is the source of truth. A cause ID never changes meaning. Adding IDs is a minor version; renaming or re-scoping is a major version. Every `Verdict` embeds `taxonomy_version`.

### Structure

**I-11 · Client adapters do not leak.**
No package outside `internal/source/**` may import a client-specific type or branch on a client name. Adapters convert to `domain` types at their boundary. Adding a new client must require zero changes outside `internal/source/` and one registry line.

**I-12 · Bounded resources.**
Hard, documented, enforced ceilings on memory, disk, and open connections. Disk uses rolling retention with a byte cap, not just a time cap. A soak test asserts the ceilings hold. The tool must be safe to run on a Raspberry Pi 5 alongside a node.

**I-13 · Single static binary.**
`CGO_ENABLED=0`. Must cross-compile to `linux/amd64` and `linux/arm64`. No runtime dependency on Python, Node, or a system library. No external database process.

**I-14 · Dependency austerity.**
Adding a third-party module requires an ADR entry justifying it, naming the alternative considered, and stating the removal path. Prefer the standard library. Target: fewer than 15 direct dependencies at v1.0.

**I-15 · Errors are wrapped, panics are forbidden.**
`panic` is permitted only in `main` on unrecoverable startup configuration errors. All other code returns wrapped errors (`fmt.Errorf("...: %w", err)`). Library code never calls `os.Exit` or `log.Fatal`.

**I-16 · Documentation ships with the code.**
No feature is complete without: updated docs, a corpus fixture (where applicable), and a CHANGELOG entry. "Docs later" is not permitted.

---

## 3. Locked technical decisions

Do not re-litigate these. To change one, write an ADR and get human approval.

| Concern | Decision | Reason |
|---|---|---|
| Language | Go 1.23+ | Ecosystem match, single binary, operators trust it |
| CLI | `spf13/cobra` | De facto standard, good UX |
| Config | YAML file + env override, `knadh/koanf` | Small, no global state |
| Logging | `log/slog` (stdlib), JSON handler | Zero dependency, structured |
| Storage | SQLite via `modernc.org/sqlite` (pure Go) | Keeps `CGO_ENABLED=0` (I-13) |
| Metrics | `prometheus/client_golang` | What operators already run |
| Parquet | `parquet-go/parquet-go` (pure Go) | Xatu dataset access without CGO |
| Testing | stdlib `testing`, table-driven, golden files | No framework lock-in |
| Lint | `golangci-lint` with committed config | Reproducible |
| Release | `goreleaser` + SLSA provenance + cosign + SBOM | Supply-chain credibility |
| License | Apache-2.0 | Permissive with patent grant; grant-friendly |
| Commits | Conventional Commits | Automated CHANGELOG |
| Versioning | SemVer, `v0.x` until API stable | Honest signalling |

**Initial client support: Lighthouse and Prysm only.** Adding more before Phase 5 is scope creep and must be refused.

---

## 4. Canonical repository structure

Create exactly this. Do not invent directories. Do not use `pkg/` unless a type is genuinely consumed by external programs.

```
whymiss/
├── cmd/
│   └── whymiss/
│       └── main.go              # thin: parse, wire, run, exit code
├── internal/
│   ├── app/                     # composition root; the only place that wires
│   ├── config/                  # load, validate, defaults, redaction
│   ├── clock/                   # NTP-disciplined clock + offset tracking
│   ├── domain/                  # PURE types. No imports beyond stdlib.
│   ├── source/                  # inbound adapters — the only client-aware code
│   │   ├── beaconapi/           # SSE event stream + REST polling
│   │   ├── promscrape/          # EL/CL/VC Prometheus scraping + normalisation
│   │   ├── hostmetrics/         # disk io, cpu steal, memory, clock drift
│   │   ├── xatu/                # network baseline (Phase 5, opt-in)
│   │   └── registry.go          # the single place clients are named
│   ├── timeline/                # assembles Observations into a Timeline
│   ├── rca/                     # PURE engine (I-6)
│   │   ├── engine.go
│   │   ├── rules/               # one file per rule, one rule per file
│   │   └── testdata/golden/
│   ├── store/                   # SQLite, migrations, retention
│   ├── report/                  # markdown + json renderers
│   └── exporter/                # Prometheus metrics surface
├── test/
│   ├── corpus/                  # labelled failure scenarios (the crown jewels)
│   │   └── <scenario-id>/
│   │       ├── manifest.yaml    # expected cause, confidence, description
│   │       ├── observations.jsonl
│   │       └── README.md
│   └── e2e/                     # against Kurtosis devnet
├── tools/
│   └── faultinjector/           # reproducible fault scenarios
├── deploy/
│   ├── docker/
│   ├── systemd/
│   └── grafana/
├── docs/
│   ├── BUILD_PROMPT.md          # this file
│   ├── architecture.md
│   ├── causes.md                # THE taxonomy contract
│   ├── configuration.md
│   ├── runbook.md
│   ├── threat-model.md
│   └── adr/
│       └── NNNN-title.md
├── .github/workflows/
├── Makefile
├── LICENSE
├── SECURITY.md
├── CONTRIBUTING.md
├── CODE_OF_CONDUCT.md
├── CHANGELOG.md
└── README.md
```

---

## 5. Coding standards

**Formatting & lint.** `gofumpt`. `golangci-lint run` must pass with zero suppressions; every `//nolint` requires an inline justification comment and is reviewed.

**Interfaces.**
- Defined by the **consumer**, not the producer.
- One to three methods. An interface with five methods is a design smell — split it.
- Never return an interface from a constructor; return the concrete type.

**Dependencies.**
- Constructor injection only: `New(deps Deps) (*T, error)`.
- No global mutable state. No `init()` with side effects. No singletons.
- `internal/app` is the only package permitted to know how things are wired together.

**Context.**
- Every exported function performing I/O takes `ctx context.Context` as its first parameter.
- Never store a `Context` in a struct field.
- Every goroutine has a documented owner and a shutdown path. No goroutine leaks — verified by `go.uber.org/goleak` in tests.

**Types.**
- No `any` or `interface{}` in `internal/domain`.
- Named types over primitives for domain concepts: `type Slot uint64`, not `uint64`.
- Prefer value types; pointers only for optionality or genuine mutation.

**Errors.**
- Sentinel errors for conditions callers branch on: `var ErrNoDuty = errors.New(...)`.
- Wrap with context: `fmt.Errorf("scrape %s: %w", target, err)`.
- Error strings: lowercase, no trailing punctuation.

**Testing.**
- Table-driven. `t.Parallel()` wherever safe.
- Golden files updated only via `go test ./... -update`. Golden diffs are reviewed like code.
- **Never hand-write a mock beacon node response.** Record a real one into `testdata/` and replay it.
- Coverage is not a target, but `internal/rca` and `internal/timeline` must be near-exhaustively covered because they are pure and cheap to test.

**Naming.**
- Packages: short, lowercase, no underscores, no plurals (`source`, not `sources`).
- No stutter: `rca.Engine`, not `rca.RCAEngine`.
- Exported identifiers have doc comments starting with the identifier name.

**Concurrency.**
- `errgroup` for fan-out with cancellation.
- Channels for ownership transfer; mutexes for protecting state. Do not mix.
- Every buffered channel's capacity has a written justification.

---

## 6. Domain model

Define this in `internal/domain` in Phase 1. **Treat it as frozen after Phase 1** — changes require human approval, because the corpus fixtures depend on it.

```go
package domain

type Slot uint64
type Epoch uint64

// SourceID identifies which adapter produced an observation.
type SourceID string

// ObservationKind is a closed vocabulary. Adding a kind is a taxonomy change.
type ObservationKind string

const (
    ObsSlotStart            ObservationKind = "slot_start"
    ObsBlockSeen            ObservationKind = "block_seen"
    ObsHeadUpdated          ObservationKind = "head_updated"
    ObsAttestationPublished ObservationKind = "attestation_published"
    ObsAttestationIncluded  ObservationKind = "attestation_included"
    ObsReorg                ObservationKind = "reorg"
    // ... extend per docs/causes.md
)

// Observation is a single timestamped fact. Immutable once created.
type Observation struct {
    Slot        Slot
    Kind        ObservationKind
    At          time.Time      // UTC, clock-corrected
    ClockOffset time.Duration  // NTP offset measured at sample time (I-9)
    Source      SourceID
    Attrs       map[string]string // bounded, keys documented in docs/causes.md
}

// Timeline is the complete input to the RCA engine for one slot.
// It is self-contained: the engine needs nothing else. (I-6)
type Timeline struct {
    Slot         Slot
    SlotStart    time.Time
    Duty         *Duty            // nil if the validator had no duty this slot
    Observations []Observation    // sorted by At, ascending
    Samples      []MetricSample   // EL/CL/VC/host samples within the slot window
    Network      *NetworkBaseline // nil unless network baseline is enabled
}

// Evidence is a human-readable, machine-checkable justification.
type Evidence struct {
    At         time.Time
    Offset     time.Duration // relative to SlotStart — this is what humans read
    Statement  string        // one line, present tense, no speculation
    Source     SourceID
    Comparison *Comparison   // observed vs expected/baseline, when applicable
}

type Confidence string

const (
    ConfidenceHigh   Confidence = "high"
    ConfidenceMedium Confidence = "medium"
    ConfidenceLow    Confidence = "low"
)

// Verdict is the product's output. Constructing one with empty Evidence
// must return an error. (I-7)
type Verdict struct {
    Slot            Slot
    Outcome         Outcome    // ok | late | missed
    Cause           CauseID
    SubCause        CauseID    // empty if none
    Confidence      Confidence
    Evidence        []Evidence
    Remediation     []string   // actionable, specific, may be empty
    EngineVersion   string
    TaxonomyVersion string
}
```

---

## 7. Cause taxonomy v1

This is the product specification. `docs/causes.md` documents each entry with: definition, the rule that fires it, required evidence, confidence derivation, and remediation guidance.

```
network.proposer_missed        No block was proposed for this slot.
network.late_block             Block arrived late for the network as a whole.
network.inclusion_failure      Attestation published on time but not included.

local.p2p_degraded             Connected peer count insufficient.
local.cl_slow                  Consensus client processing exceeded budget.
local.el_slow                  Execution client responded slowly.
  ├─ local.el_slow.snapshot        EL generating state snapshots.
  ├─ local.el_slow.pruning         EL pruning.
  ├─ local.el_slow.disk_saturation Disk saturated during the window.
  └─ local.el_slow.syncing         EL not fully synced.
local.vc_slow                  Validator client published late.
local.vc_disconnected          Validator client not reachable from beacon node.

local.host.disk_io             Host disk I/O wait elevated.
local.host.cpu_steal           CPU steal time elevated (noisy neighbour / VPS).
local.host.memory_pressure     Memory pressure or swap activity.
local.host.clock_drift         Clock offset exceeded threshold.

unknown.insufficient_data      Required observations were unavailable.
unknown.no_rule_matched        Data complete, no rule matched. Report as a gap.
```

**Rule authoring contract.** One rule per file in `internal/rca/rules/`. Each rule implements:

```go
type Rule interface {
    ID() CauseID
    // Evaluate returns (nil, false) if the rule does not apply.
    Evaluate(t domain.Timeline, cfg Config) (*domain.Verdict, bool)
}
```

Rules are evaluated in a **declared, documented order**. The first match wins. The ordering lives in one file with a comment explaining each position. Ordering changes require an ADR.

---

## 8. Working agreement

**Stop and ask the human when:**
- A task appears to require violating an invariant.
- A locked technical decision (§3) seems wrong.
- A new third-party dependency is needed.
- The domain model (§6) needs to change after Phase 1.
- A cause taxonomy entry needs renaming or re-scoping.
- Requirements are ambiguous. **Do not guess and proceed.**

**Never:**
- Add a feature that was not requested.
- Leave `TODO` in merged code — open an issue instead and reference it.
- Commit commented-out code.
- Weaken a test to make it pass.
- Write a rule that produces a verdict the evidence does not support.
- Mark a Definition of Done item complete without running its verification command.

**Always:**
- Run `make ci` before declaring a task done.
- Update `CHANGELOG.md` in the same commit as the change.
- Write the ADR *before* the implementation it justifies.

**Task Report format** — output this at the end of every task:

```
TASK: <phase>.<number> — <title>
STATUS: complete | blocked | partial

CHANGED:
  <path> — <one-line reason>

DECISIONS:
  <any judgement call made, and why>

VERIFICATION:
  $ <command>
  <result>

INVARIANTS: confirmed no violation of I-1..I-16
  (or: I-N at risk because <reason> — human input required)

NEXT: <the single next task>
```

---

## 9. PHASE 1 — Foundation and failure corpus

> **Thesis:** Build the test data before the product. The corpus is the moat, the test suite, and the grant evidence simultaneously.

### 9.1 Objective

A repository that builds and lints cleanly, a frozen domain model, and a reproducible harness that generates labelled failure scenarios from a local devnet.

### 9.2 Tasks

| # | Task |
|---|---|
| 1.1 | Repo scaffold per §4, `Makefile` (`build test lint ci corpus.generate`), Apache-2.0, `SECURITY.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md` |
| 1.2 | CI: lint, test, race detector, `go vet`, cross-compile amd64+arm64, dependency review. Must run in under 5 minutes. |
| 1.3 | `internal/domain` per §6, with constructor validation and exhaustive unit tests |
| 1.4 | `internal/clock`: NTP offset measurement, degradation behaviour per I-9 |
| 1.5 | `tools/faultinjector`: declarative scenarios (`tc netem`, cgroup `io.max`, container pause, `libfaketime`, peer drop) against a Kurtosis devnet |
| 1.6 | Corpus format: `manifest.yaml` schema, `observations.jsonl` format, validator command `make corpus.validate` |
| 1.7 | Generate **≥20 labelled scenarios** covering at least 8 distinct causes. **Revised down to 9 scenarios / 6 causes** after real devnet work — see `CHANGELOG.md`'s Phase 1 corpus notes for which causes were judged not achievable with this project's devnet and toolchain (`local.host.cpu_steal` needs real hypervisor contention a cgroup cannot produce; `network.inclusion_failure`, `network.late_block`, `local.el_slow`, and `local.host.disk_io` were each attempted multiple times against real fault severities and never produced clean evidence, most likely because this two-node devnet's per-slot workload is too light for a passive resource cap to gate). **That diagnosis was wrong for three of the four causes, and 2026-08-26's work says so with measurements** (full detail in `CHANGELOG.md`). `network.late_block` was unreproducible because a two-node devnet has no third party: the proposer is always one of the two observers and a node records no gossip arrival for a block it produced, so one of the two measurements R-110 compares was always missing. The devnet has three participants now and the cause reproduces on the first attempt. `local.el_slow` was blocked by the corpus format, not the workload — R-300 reads its baseline as a `domain.MetricSample` and records carried only observations, so the rule could never fire on any record at any severity; records now carry an optional `samples.jsonl`. `local.host.disk_io` failed because every `io.max` cap tried sat in the MB/s range against a devnet writing 40.8 KB/s, so nothing was ever throttled; with load and a correct cap the fault produces 48% I/O pressure, though the cause still does not reproduce for a different and now-measured reason (geth's validation on a devnet-sized state is not disk-bound). Only `local.host.cpu_steal` stands as first written. The lesson worth keeping: "the fault had no measurable effect" and "the fault was never applied to anything" look identical from the outside |
| 1.8 | ADR-0001 language/runtime, ADR-0002 storage, ADR-0003 pure-engine architecture, ADR-0004 dependency policy, ADR-0005 cause taxonomy governance |

### 9.3 Definition of Done

- [x] `make ci` green on a clean checkout
- [x] Binary cross-compiles to `linux/amd64` and `linux/arm64`
- [x] `make corpus.generate SCENARIO=vc-frozen-lighthouse BEACON=cl-1-lighthouse-geth` reproduces the scenario end to end on a fresh machine (originally named `el-disk-stall` here; that scenario was removed from the corpus after real devnet runs showed cgroup `io.max` disk throttling has no measurable effect at any severity on this project's devnet workload — see `CHANGELOG.md`. The requirement is unchanged: any one committed scenario ID must reproduce end to end)
- [x] `make corpus.validate` passes for all committed scenarios — `corpusctl: 52 scenarios OK`. The note that used to sit here read "9, not the original ≥20 target — format-v2 regeneration is the current release blocker"; both halves are spent: every record is format v2, and the release gate's count of 50 is now met.
- [x] `internal/domain` imports nothing outside the standard library — enforced by a CI check
- [x] Five ADRs merged (six: ADR-0001 through ADR-0006, the last for `gopkg.in/yaml.v3`)
- [x] `docs/architecture.md` describes the pipeline with a diagram

### 9.4 Anti-goals for this phase

No RCA logic. No collector daemon. No packaging. No Prometheus exporter. If tempted, note it as a Phase 2/3 issue and move on.

---

## 10. PHASE 2 — Collector and timeline

> **Thesis:** Collect faithfully and cheaply. This phase earns the operator's trust or loses it.

### 10.1 Objective

A daemon that runs beside a node for days without being noticed, and can reconstruct a complete timeline for any slot within its retention window.

### 10.2 Tasks

| # | Task |
|---|---|
| 2.1 | `internal/source/beaconapi`: SSE event stream (`head`, `block`, `chain_reorg`, `attestation`) with reconnect, plus REST polling for duties and attestation inclusion |
| 2.2 | `internal/source/promscrape`: scrape EL/CL/VC Prometheus endpoints; normalise Lighthouse and Prysm metric names into `domain.MetricSample` |
| 2.3 | `internal/source/hostmetrics`: Linux PSI I/O pressure, CPU steal, memory pressure, clock drift — degrade gracefully when unavailable (I-3) |
| 2.4 | `internal/source/registry.go`: client detection and adapter selection; the only client-aware file outside adapters (I-11) |
| 2.5 | `internal/store`: SQLite schema, versioned migrations, rolling retention by **both** time and bytes (I-12) |
| 2.6 | `internal/timeline`: assemble observations + samples into `domain.Timeline`; deterministic ordering |
| 2.7 | CLI: `whymiss watch` (daemon), `whymiss timeline <slot> --format json` |
| 2.8 | Replay mode: rebuild timelines from corpus `observations.jsonl` — this is how Phase 3 tests run |
| 2.9 | Rate limiting and backoff per I-5, with a test asserting the request-rate ceiling |

### 10.3 Definition of Done

- [ ] 72-hour soak against Hoodi testnet: RSS stays under the documented ceiling, disk respects the byte cap, zero goroutine leaks (`goleak`) — **the goroutine half is already proven; the duration half is running.** `goleak` is not a soak assertion at all: `internal/app/main_test.go` calls `goleak.VerifyTestMain(m)`, so every test in the package runs under it, including `TestWatch_EveryCollectorShutsDownCleanly`, which enables every optional collector at once and asserts the whole daemon unwinds on cancellation. That passes in `make ci` today. What remains is wall-clock: the soak started 2026-08-27T01:44:21Z on `1d0bdd6d` (sha recorded in the run's own `BINARY.md`) and finishes ~2026-08-30T01:44Z. `test/soak/run.sh` decides pass/fail itself — it exits non-zero the moment RSS exceeds 262144 KiB or the database exceeds 104857600 bytes, and writes `result=PASS` to `summary.txt` only on success — so completing this item means reading that line, not eyeballing a graph. A watcher is installed on the soak host (`~/phase2_watch.sh`) that waits for the daemon to exit and writes the verdict to `~/PHASE2_STATUS.txt`, so closing this item is one `cat` of that file on whichever host ran the soak — the host itself is deliberately not named here, since this file is public. It prints `PHASE 2: CLOSED` with peak RSS, final database size, error count and binary sha when the soak passed, or `PHASE 2: STILL OPEN` with the summary tail when it did not.
- [x] `whymiss timeline <slot>` returns a complete timeline for any slot in retention — checked on 2026-08-26 against the live Hoodi soak's own store, on three slots spanning its retention window (oldest, middle, newest of 68 recorded verdicts). Each returned a timeline with the attester duty recovered; the same slots also render a verdict through `whymiss <slot>`
- [x] Replaying every corpus scenario produces **byte-identical** timelines across runs — `go test -run TestReplay_ByteIdenticalAcrossRuns ./internal/timeline`, and the test enumerates `test/corpus/` rather than naming scenarios, so new records are covered as they land
- [x] Request rate against the beacon node stays under the configured ceiling — proven by `TestRateLimiter_EnforcesCeiling` and three siblings in `internal/source/beaconapi/ratelimit_test.go`
- [x] Adding a hypothetical third client would touch only `internal/source/` — demonstrated in `docs/architecture.md`, and the walkthrough shrank in 2026-08-26's work: peer count left the list entirely once it came from the standardised Beacon API endpoint (ADR-0023), as did the network baseline without `--baseline-metrics-api` (ADR-0025)
- [x] Runs as non-root, no capabilities, verified in CI — `make check.nonroot`, inside `make check`, inside `make ci`
- [x] `docs/configuration.md` documents every option with defaults and safe ranges — all 14 CLI flags appear with a default and a constraint; checked mechanically against `cmd/whymiss` on 2026-08-26, which is also how two stale descriptions were found (`--cl-metrics-api` still claimed to govern peer sampling, which moved to the Beacon API in ADR-0023)

### 10.4 Anti-goals

No cause analysis. No opinions about what the data means. This phase only records facts.

---

## 11. PHASE 3 — The RCA engine

> **Thesis:** This is the product. Everything before was plumbing; everything after is packaging.

### 11.1 Objective

A pure, deterministic, auditable engine that turns a `Timeline` into a `Verdict` with evidence — and a measured accuracy number.

### 11.2 Tasks

| # | Task |
|---|---|
| 3.1 | `internal/rca/engine.go`: rule registry, declared ordering, verdict construction with evidence validation (I-7) |
| 3.2 | Implement rules for all `local.*` and `network.proposer_missed` causes — one rule per file |
| 3.3 | Confidence derivation: documented, rule-specific, never heuristic hand-waving |
| 3.4 | Remediation text per cause — specific and actionable, e.g. name the exact command, not "check your disk" |
| 3.5 | `internal/report`: markdown and JSON renderers; markdown output must be readable pasted into a forum post |
| 3.6 | CLI: bare `whymiss <slot>` is the primary verb — it explains that slot. `--format markdown\|json`. |
| 3.7 | Golden tests over every corpus scenario |
| 3.8 | `make eval`: precision/recall per cause across the corpus, output as a committed markdown report |
| 3.9 | Determinism test: analyse the same timeline 1,000 times, assert byte-identical output |
| 3.10 | Grow corpus to **≥50 scenarios**, including adversarial and ambiguous cases that *should* yield `unknown` |

### 11.3 Definition of Done

- [x] `internal/rca` imports only stdlib and `internal/domain` — enforced by `make check.purity`, inside `make check`, inside `make ci`
- [x] Determinism test passes — `TestAnalyze_Deterministic` re-analyses a real timeline 1000 times and asserts byte-identical output
- [x] **Top-1 accuracy ≥ 90%** on the corpus, reported per cause in `docs/evaluation.md` — 52/52 (100%) with zero false-high verdicts, verified by `make eval.check` on 2026-08-27, which enforces the count, the accuracy floor, the presence of ambiguous scenarios, and the false-high bar together.
- [x] **Zero false-confident verdicts**: no `ConfidenceHigh` verdict is wrong on any corpus scenario. This is a hard gate — treat a single violation as a release blocker (I-8). `docs/evaluation.md` reports 0, and `tools/eval --check` fails the build on any non-zero count — a gate that was silently unenforced until 2026-08-26, when `eval.check` was found discarding its exit status
- [x] Ambiguous scenarios correctly yield `unknown.*` — 11 of the 52 records expect an `unknown.*` label and all 11 match, including the `proposer-missed-concurrent-vc-pause*` recordings that ADR-0021 reclassified from a false exoneration
- [x] `docs/causes.md` complete: every cause has definition, rule, evidence requirements, confidence derivation, remediation — checked mechanically across all 14 entries on 2026-08-26, which is how `local.el_slow`'s missing generic-cause remediation was found
- [x] A sample markdown report is in the README and is genuinely readable — verified byte-identical to what the current engine produces for `test/corpus/vc-slow-cpu` on 2026-08-26, so it cannot drift into a claim the code no longer makes. Readable on task 3.5's own bar ("readable pasted into a forum post"): it names the cause, says which reward flag was lost, gives one evidence line carrying both measured timings and the deadline they violated, and three remediations specific enough to act on — a remote signer by name, CPU contention on the VC host, clock agreement

### 11.4 Anti-goals

No network baseline yet — that is Phase 5. No ML. No probabilistic scoring. No web UI.

---

## 12. PHASE 4 — Operator surface and v0.1.0

> **Thesis:** An operator must go from zero to running in under five minutes, reading only the README.

### 12.1 Objective

A release an operator can trust on a live staking box.

### 12.2 Tasks

| # | Task |
|---|---|
| 4.1 | `internal/exporter`: Prometheus metrics with a `cause` label — this is the headline feature. Alert on causes, not symptoms. |
| 4.2 | Grafana dashboard JSON in `deploy/grafana/`, tested against a real Grafana |
| 4.3 | Docker image: distroless, non-root, read-only rootfs, no shell. Multi-arch. |
| 4.4 | `deploy/docker/docker-compose.yml` and `deploy/systemd/whymiss.service` with hardening directives |
| 4.5 | `README.md`: what it is, a real sample report, 60-second quickstart, honest limitations section |
| 4.6 | `docs/runbook.md` and `docs/threat-model.md` (threat model is what convinces stakers to install) |
| 4.7 | `goreleaser` config: signed checksums, SBOM, SLSA provenance via GitHub Actions, cosign signatures |
| 4.8 | `SECURITY.md` with disclosure policy and response SLA |
| 4.9 | Fresh-machine install test in CI: README instructions only, no tribal knowledge |
| 4.10 | Tag and publish `v0.1.0` |

### 12.3 Definition of Done

- [ ] Fresh Linux box → running and producing verdicts in under 5 minutes following the README alone — **the running half is proven; the verdict half is unachievable as written, and that is a fault in this criterion rather than in the product.** `.github/workflows/fresh-install.yml` runs two jobs on `ubuntu-latest`, a genuinely fresh Linux box: `from-source` follows the README's "From source" path (`make build`) and then fails loudly if any flag the README's example names has been renamed, and `docker-compose` runs `make test.freshinstall`, which brings the stack up and asserts Prometheus is scraping the daemon. Both passed on 2026-08-27. What cannot be met is "producing verdicts in under 5 minutes". Measured on the Hoodi soak, a real Linux host against a real beacon node: the daemon started at 01:44:21Z and recorded its first verdict at 01:56:00Z — **11m39s** — with the second at 02:02:24Z, 6m24s later. That cadence is one epoch. An attester duty arrives once per epoch (32 slots x 12s = 6.4 minutes) and a verdict cannot exist until the duty's collection window closes, so the first verdict is bounded below by the chain, not by install speed. Rewrite this item as "running in under 5 minutes; first verdict within two epochs" before ticking it — ticking it as written would require either a false claim or a misreading of what was measured.
- [x] Container runs as non-root with a read-only root filesystem — asserted by `make test.freshinstall` (passed 2026-08-27) on the running Compose service, not on the image: `Config.User=65532:65532`, `ReadonlyRootfs=true`, `CapDrop=["ALL"]`, `SecurityOpt=["no-new-privileges:true"]`, `Memory=268435456`, `PidsLimit=64`, `PortBindings={}`. The same run also proves the image is distroless by requiring `docker run --entrypoint /bin/sh` to fail.
- [ ] Release artifacts carry provenance and signatures; verification steps documented and tested
- [x] Grafana dashboard imports cleanly and shows real data — proven in two halves on 2026-08-27, because no single environment could show both. `make test.freshinstall` proves the import path: Grafana comes up healthy, `whymiss-duty-causes` appears in `/api/search`, and a query through Grafana's own datasource proxy returns a live value. The Hoodi soak proves the data half, which the fresh-install mock cannot — its beacon serves only genesis and spec, so no duty is ever tracked. All four dashboard queries target `whymiss_duty_verdicts_total` with labels `cause` and `outcome`, and the running soak emits exactly that metric with those labels: `{cause="none",outcome="ok"} 33`, `{cause="unknown.insufficient_data",outcome="degraded"} 5`, `{cause="unknown.insufficient_data",outcome="ok"} 7`. The two queries filtered on `outcome=~"missed|degraded"` match five real series, so they return data rather than empty. What is still not shown is a rendered screenshot of those panels; the query-to-metric contract and the import both hold.
- [x] Prometheus `cause` label cardinality is bounded and documented — ADR-0009 states the bound (`len(domain.CauseIDs())` + 1 = 19 cause values x 4 outcomes = 76 series), and `TestVerdictSeriesCardinalityIsBounded` now enforces it behaviourally: it drives `Record` with every (cause, outcome) pair the domain can produce, scrapes the exporter's own handler, and counts the exposed series. Measured 2026-08-27: exactly 76, so the documented ceiling is the real one. The bound was arithmetic in an ADR until then — a cause added to the taxonomy, or `sub_cause` promoted to its own label, would have widened every operator's time-series count with no test objecting.
- [x] Uninstall is one documented command that leaves nothing behind — verified empirically on 2026-08-27 for the Compose path, which is the one `docs/runbook.md` presents as a single command. Bringing the stack up created three containers, the three named volumes the runbook lists (`whymiss-data`, `prometheus-data`, `grafana-data`), and one network; `docker compose down -v` returned 0 and left zero of them. The host's total volume and network counts returned to exactly their pre-install values (53 and 7), so nothing was orphaned outside the project label either. **The systemd path in the same runbook section is documented but not exercised here** — it needs a systemd host, and the soak VM runs the binary directly rather than through the unit. That half rests on `DynamicUser=yes` leaving no user to remove and on `StateDirectory=` being deleted explicitly by the documented commands; both are reasoning, not measurement.
- [x] Threat model explicitly addresses: no key access, no egress, no privilege, resource caps — `docs/threat-model.md` names the invariant behind each (I-2 keys, I-4 egress, I-3 unprivileged, I-12 bounded resources) and carries a "Verifying these claims yourself" section rather than asking to be believed. Those commands were run on 2026-08-27 and all pass: `make check.egress`, `make check.isolation`, `make check.nonroot`, and the key-material grep over `cmd/` and `internal/`, which returns zero matches outside tests. The container half — non-root identity, read-only rootfs, dropped capabilities, absence of a shell — is asserted at runtime by `make test.freshinstall`, which also passed that day.

### 12.4 Anti-goals

No hosted service. No auto-update. No accounts. No web UI.

---

## 13. PHASE 5 — Network baseline and ePBS readiness

> **Thesis:** The network baseline unlocks the product's core question. The fork-agnostic slot schedule makes the product survive protocol change.

### 13.1 Objective

Answer *"was it me or the network?"* with data, and make the timing model swappable via configuration rather than code — so Glamsterdam and later forks are a config change, not a rewrite.

### 13.2 Tasks

| # | Task |
|---|---|
| 5.1 | `internal/source/xatu`: read the public Xatu parquet dataset for network-wide block-arrival percentiles. Opt-in, cached locally, fully functional offline with graceful degradation (I-4) |
| 5.2 | `domain.NetworkBaseline` and the discrimination rules that separate `network.late_block` from `local.*` |
| 5.3 | Re-run `make eval` — quantify the accuracy improvement the baseline provides. This number is a headline grant metric. |
| 5.4 | **`SlotSchedule` as declarative config**: attestation deadline, aggregation window, and (post-ePBS) payload reveal deadline and PTC deadline expressed as data, not constants. Fork-specific schedules live in YAML. |
| 5.5 | ePBS support behind a feature flag: PTC duty tracking, payload-timeliness observations, split consensus/execution deadline rules |
| 5.6 | Glamsterdam devnet validation using the Phase 1 fault injector against an ePBS devnet |
| 5.7 | `docs/extending.md`: how to contribute a new client adapter and a new rule, with a worked example |
| 5.8 | **Case study**: reproduce the root cause of a publicly documented incident using only whymiss output. Publish as `docs/case-studies/`. This is the single most persuasive grant artifact. |
| 5.9 | Benchmark report: resource footprint, accuracy, coverage — `docs/evaluation.md` |

### 13.3 Definition of Done

- [ ] Network baseline is opt-in and defaults to off; tool is fully functional with it disabled
- [ ] Measured accuracy improvement documented with before/after numbers
- [ ] Switching between pre-ePBS and post-ePBS timing requires **only a config change** — proven by test
- [ ] ePBS rules validated against a Glamsterdam devnet
- [ ] Case study reproduces a known public root cause end to end
- [ ] `docs/extending.md` is complete enough that an external contributor can add an adapter unaided

### 13.4 Anti-goals

Do not couple the engine to Xatu. If Xatu disappears tomorrow, whymiss must still work — with reduced discrimination, clearly signalled to the user.

---

## 14. Cross-phase quality gates

Enforced in CI from Phase 1 onward. A failing gate blocks merge.

| Gate | Check |
|---|---|
| Purity | `internal/rca` and `internal/domain` import allowlist |
| Isolation | No client-specific identifier outside `internal/source/**` |
| Leaks | `goleak` on all daemon tests |
| Race | `go test -race ./...` |
| Determinism | RCA golden tests byte-identical |
| Resources | Soak test asserts memory and disk ceilings |
| Rate | Beacon-node request rate under configured ceiling |
| Privilege | Container and binary run as non-root |
| Egress | No outbound calls in default config — asserted by test |
| Supply chain | `govulncheck`, dependency review, pinned actions by SHA |
| Docs | CHANGELOG entry present; touched docs updated |

---

## 15. Definition of "hero"

The project has succeeded when:

1. An operator installs it, hits a real miss, reads the report, fixes the actual cause, and **did not need to ask anyone**.
2. A client team links to a whymiss report in an incident thread.
3. An external contributor lands a client adapter without maintainer hand-holding.
4. The evaluation report is credible enough to stand on its own in a funding application.

Optimise every decision against #1.
