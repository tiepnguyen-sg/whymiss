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
