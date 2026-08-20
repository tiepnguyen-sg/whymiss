# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). The
version stays at `v0.x` until the API is stable (BUILD_PROMPT §3).

I-16 makes an entry here mandatory: any commit touching `internal/` or `cmd/` must
update this file in the same commit. CI and the pre-commit hook both enforce it.

## [Unreleased]

### Added

- Repository scaffold: canonical directory layout per BUILD_PROMPT §4, Apache-2.0
  licence, and contributor governance documents.
- `go.mod` at Go 1.23 with zero dependencies. Additions require an ADR (I-14).
- `.golangci.yml` with `depguard` rules enforcing the I-6 purity boundaries for
  `internal/domain` and `internal/rca`.
- ADR-0001 through ADR-0005: language/runtime, storage, pure-engine architecture,
  dependency policy, and cause-taxonomy governance.
- `internal/domain`: the frozen domain model (task 1.3) — `Observation`,
  `Timeline`, `Verdict`, and their supporting types, per BUILD_PROMPT §6 as refined
  by the `Outcome`/`RewardFlags` delta in `docs/causes.md` §2. Every constructor
  validates; construction with no evidence fails (I-7). 100% statement coverage,
  including a test that fails the build if `docs/causes.md` and the coded cause
  taxonomy drift apart (ADR-0005).
- `internal/clock` (task 1.4): NTP offset measurement over SNTP, degrading
  honestly — a typed error, never a fabricated offset — when every configured
  server fails within a bounded, jittered retry budget (I-5). `Tracker` remembers
  the last successful reading so a later failure can still report "time of last
  successful sync" (docs/causes.md, `local.host.clock_drift`). No server is built
  in; a caller supplies whatever the operator configured (I-4). Zero new
  dependencies.
- `cmd/whymiss/main.go`: a minimal stub so the binary builds and cross-compiles
  (I-13). The real CLI surface arrives in Phase 2 task 2.7 with the `cobra`
  dependency it needs.
- ADR-0006: `gopkg.in/yaml.v3` for `manifest.yaml` and scenario files.
- `test/e2e/kurtosis`: a two-participant devnet (Lighthouse+Geth, Prysm+Geth),
  matching BUILD_PROMPT §3's initial client scope. `make devnet.up/down/info`.
  See its README for hard-won configuration constraints (network name must be
  the literal `"kurtosis"`; Fulu/BPO forks must be pushed out to avoid a
  128-validator-per-node requirement unrelated to what whymiss tests).
- `tools/faultinjector` (task 1.5): reproducibly injects a declared fault into
  the running devnet and records what actually happens as a corpus scenario.
  Two mechanisms verified against a live devnet — `pause` (`docker
  pause`/`unpause`) and `cgroup_io` (cgroup v2 `io.max`, written from the
  Docker host's own namespaces since a container's cgroup interface is
  correctly read-only from inside). `netem` and `clock_skew` are scaffolded
  but explicitly not implemented pending verification on a real Linux host —
  see their doc comments; `peer_drop` reuses the verified pause mechanism
  aimed at a peer's container. Every value written to `observations.jsonl` is
  measured against the real beacon API during the run, never synthesized
  (docs/BUILD_PROMPT.md §8).
