# Changelog

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). The
version stays at `v0.x` until the API is stable.

## [Unreleased]

### Fixed

- A duty that completed cleanly (`outcome: ok`, every reward flag earned) was
  reported as `unknown.no_rule_matched` — the catch-all rule, whose remediation
  text asks the operator to file an issue. Every healthy duty looked like a bug.
  `rca.Analyze` now returns a causeless, high-confidence verdict for that case,
  and `domain.Verdict` accepts an empty cause on `ok`, matching how `no_duty`
  already worked.

  The check runs after the rule loop rather than before it. No rule inspects
  `Outcome`, so a rule can legitimately match on a duty that still ended `ok` —
  a validator client that ran slow but beat the deadline should still report
  `local.vc_slow`. Only the catch-all is reinterpreted.

- Markdown reports render `healthy` as the headline for a causeless verdict
  instead of an empty one. Prometheus already mapped an empty cause to `none`,
  so healthy duties scrape as `{cause="none",outcome="ok"}` with no change to
  the documented cardinality bound.

## [0.1.0] - 2026-08-22

First release.

### Added

**Domain model.** `Observation`, `Timeline`, `Verdict`, `Duty`, `RewardFlags`,
and a closed cause taxonomy (`docs/causes.md`), versioned as a public contract:
cause IDs appear in Prometheus labels and machine-readable output, so they never
change meaning silently. Every verdict embeds the engine and taxonomy versions
that produced it, and construction fails without evidence.

**Beacon API adapter** (`internal/source/beaconapi`). REST polling and SSE
streaming against the standard Beacon API: genesis, attester and proposer duties,
block arrival, attestation publication, and on-chain inclusion. Every call is
rate-limited with exponential backoff and jitter — the tool must never degrade the
node it watches. Tests run against real captured responses in `testdata/`, not
hand-written mocks.

**Metrics and host adapters.** `internal/source/promscrape` scrapes Engine API
call durations from an execution client and peer counts from a consensus client,
normalising across client-specific metric names. `internal/source/hostmetrics`
reads disk and memory pressure from `/proc/pressure/*` and CPU steal from
`/proc/stat`. `internal/source/registry.go` maps a node's version string to a
known client; client-specific code lives only in this package tree.

**Clock discipline** (`internal/clock`). NTP offset measured over SNTP at sample
time. When the offset is unmeasurable or exceeds the configured threshold,
timing-derived rules are suppressed rather than producing a verdict on an
untrusted clock.

**Storage** (`internal/store`). SQLite via `modernc.org/sqlite`, keeping the
binary CGO-free. Rolling retention with both an age and a byte cap, deleting
oldest-first, so disk use stays bounded on a small machine.

**Timeline assembly** (`internal/timeline`). Collects observations into a
per-slot `Timeline` and replays a recorded observation stream deterministically.

**RCA engine** (`internal/rca`). `Analyze(Timeline, Config) Verdict` — pure: no
I/O, no clock reads, no randomness, no goroutines, so the same input always
produces byte-identical output. Thirteen rules run in a fixed, documented order,
first match wins, from data-completeness and clock-trust guards through network
attribution (proposer missed, late block, inclusion failure), local layers (p2p,
execution client, consensus client, validator client), a host fallback, and an
unconditional catch-all. Rules prefer `unknown` over a guess: a wrong confident
verdict is treated as worse than no verdict.

**Reporting** (`internal/report`). Markdown post-mortems readable pasted into an
incident thread, and JSON matching the verdict's own field names.

**CLI** (`cmd/whymiss`). `whymiss <slot>` explains a slot, `whymiss watch` runs
the collector daemon, `whymiss timeline <slot>` prints raw recorded facts with no
interpretation.

**Prometheus exporter** (`internal/exporter`). One counter,
`whymiss_duty_verdicts_total{cause,outcome}`, so alerts fire on causes rather
than symptoms. Cardinality is bounded by construction — both labels come from
closed sets, giving at most 76 series regardless of validator count or uptime.
`whymiss watch --validator-index N --metrics-addr :9101` tracks attester duties
per epoch and records a verdict for each completed duty.

**Grafana dashboard** (`deploy/grafana/`). Missed and degraded duties by cause
over time, an outcome breakdown, a cause bar gauge, and a last-hour stat panel.
Binds to whatever Prometheus datasource it is provisioned against.

**Deployment** (`deploy/`). A distroless, non-root, multi-arch container image
with no shell; a Docker Compose stack running whymiss, Prometheus, and Grafana,
each with a read-only root filesystem, dropped capabilities, and memory caps; and
a systemd unit using `DynamicUser` with full sandboxing, so there is no service
account to create or manage.

**Test corpus and evaluation.** Nine labelled failure scenarios in `test/corpus`,
each generated by injecting a real fault into a live Kurtosis devnet and
recording what the beacon API actually reported — never synthesized.
`tools/faultinjector` reproduces them, `tools/corpusctl` validates their format,
and `tools/eval` measures accuracy into `docs/evaluation.md`: currently 9/9 top-1
and zero wrong verdicts reported at high confidence.

**Documentation.** `docs/causes.md` (the taxonomy contract), `architecture.md`,
`configuration.md`, `runbook.md`, `threat-model.md`, `evaluation.md`, and
architecture decision records under `docs/adr/`.

**Release pipeline.** GoReleaser builds static `linux/amd64` and `linux/arm64`
binaries; syft generates an SBOM per archive; cosign signs the checksum file
keyless via GitHub OIDC, so there is no long-lived signing key to protect. CI
enforces formatting, linting, the purity and isolation boundaries, race-enabled
tests, vulnerability scanning, and cross-compilation, plus a job that follows
the README's install instructions from a clean checkout.

### Known limitations

- Automatic duty tracking covers attester duties only; proposer duties are not
  yet wired into the watch loop.
- Accuracy is measured against nine scenarios covering six causes, generated on a
  two-node Lighthouse/Prysm devnet — not yet validated against mainnet incidents
  or other client pairings.
- Host-level causes require a host metrics collector alongside whymiss; without
  one they fall back to lower confidence rather than being inferred from
  beacon-node data alone.
- Several causes in the taxonomy (`local.el_slow`, `network.late_block`,
  `network.inclusion_failure`, `local.host.cpu_steal`) have rules but no corpus
  scenario yet: reproducing them needs either hypervisor-level contention or a
  larger network than a two-node devnet.
- The slot schedule is fixed to pre-ePBS mainnet timings.
- No long-duration soak test has been run against a public testnet.
