# ADR-0011 · Koanf configuration merging

- **Status:** accepted
- **Date:** 2026-08-23
- **Deciders:** maintainers
- **Supersedes:** —

## Context

BUILD_PROMPT §3 requires YAML configuration with environment overrides through
`knadh/koanf`. Operators also need explicit CLI flags to remain the highest-priority
source. The merge must be deterministic, reject unknown YAML keys, and avoid global
state.

## Decision

Use **`github.com/knadh/koanf/v2 v2.3.6`** inside `internal/config` only. Defaults,
one optional YAML document, and `WHYMISS_*` environment variables are merged in
that order. Cobra applies explicitly changed flags last. YAML decoding remains on
`gopkg.in/yaml.v3` from ADR-0006 so unknown fields and multiple documents can be
rejected before koanf merges typed values.

## Consequences

The binary gains one direct dependency and koanf's small mapping dependency tree.
Configuration precedence is testable without process-global mutation, and RCA
schedule/threshold changes no longer require code changes.

**Removal path.** All koanf use is confined to `internal/config/config.go`. Replace
its map merge with a small standard-library merge while preserving `Config` and its
tests; no caller changes are required.

## Alternatives considered

**Manual map merging.** Feasible, but rejected because it duplicates the locked
technical decision and makes nested precedence behavior project-owned code.

**Viper.** Rejected as a larger dependency with global-state conventions that are
unnecessary here; BUILD_PROMPT locks koanf.
