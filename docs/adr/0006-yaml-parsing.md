# ADR-0006 · YAML parsing

- **Status:** accepted
- **Date:** 2026-08-20
- **Deciders:** maintainers
- **Supersedes:** —

## Context

Two formats BUILD_PROMPT locks by name are YAML: `test/corpus/<id>/manifest.yaml`
(§4, §9.2 task 1.6) and, by the same convention, `tools/faultinjector`'s scenario
declarations that drive it. The standard library has `encoding/json` and
`encoding/xml` but no YAML parser, so this format choice — already made by
BUILD_PROMPT, not being relitigated here — requires a dependency to implement.

`tools/` and `internal/config` (Phase 2, also YAML per BUILD_PROMPT §3) are the
only consumers. Neither `internal/domain` nor `internal/rca` need this: I-6's
purity boundary is untouched regardless of what this ADR decides.

## Decision

**`gopkg.in/yaml.v3`.**

Rationale against ADR-0004's dependency policy:

- **Single-purpose, zero transitive dependencies.** It decodes YAML and nothing
  else — no framework, no config-loading opinions, no logging side effects. A
  `go.sum` for it is a handful of lines.
- **The de facto standard.** It is what `koanf` (BUILD_PROMPT §3's locked config
  choice) uses internally for its own YAML provider, so Phase 2 does not introduce
  a second YAML implementation alongside this one — reusing it here is one fewer
  library the eventual dependency count has to carry, not one more.
- **No credible standard-library alternative.** Hand-rolling a YAML parser to avoid
  one dependency would cost far more maintenance than the dependency itself, for a
  format BUILD_PROMPT already committed to by name.

## Consequences

**Good**

- `manifest.yaml`, scenario files, and Phase 2's config loader all parse through
  one well-tested library.
- Struct tags (`yaml:"..."`) keep the Go types the single source of truth for the
  format, the same pattern `encoding/json` already uses elsewhere in this codebase.

**Bad**

- One more line in the dependency-austerity budget (I-14: fewer than 15 direct
  dependencies at v1.0). Accepted — this is a locked-format requirement, not
  optional convenience.

**Removal path.** Confined to `tools/` and (from Phase 2) `internal/config`, both
outside the I-6 purity boundary. Replacing it means swapping the import in those
packages for another decoder satisfying the same struct-tag contract; no data
format changes, so `manifest.yaml` files already on disk keep parsing.

## Alternatives considered

**`sigs.k8s.io/yaml`** (round-trips through `encoding/json`). Rejected: an extra
hop for no benefit here, since nothing in this codebase needs YAML/JSON
interchangeability — these files are authored as YAML and stay YAML.

**Hand-rolled parser for the specific manifest/scenario shape.** Rejected per
ADR-0004: a dependency that replaces twenty lines is refused; a general-purpose
YAML parser is not that — it is the same austerity trade-off SQLite was in
ADR-0002, just smaller.

**Switch the format to JSON and drop the dependency.** Rejected: it contradicts
BUILD_PROMPT's structure (§4) and task 1.6's explicit `manifest.yaml` naming, which
is not this ADR's decision to overturn — see BUILD_PROMPT §8, "a locked technical
decision seems wrong" is a stop-and-ask, not a silent substitution.
