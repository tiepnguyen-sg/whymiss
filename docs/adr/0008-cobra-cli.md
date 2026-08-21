# ADR-0008 · Cobra for the CLI

- **Status:** accepted
- **Date:** 2026-08-21
- **Deciders:** maintainers
- **Supersedes:** —

## Context

BUILD_PROMPT §3 locks `spf13/cobra` for the CLI, and §4.2 the pre-approved
choice under ADR-0004's "each still lands in its own ADR when first added."
Task 2.7 is the first code that needs a real command surface:
`whymiss watch` (the collector daemon) and `whymiss timeline <slot>
--format json` (Phase 2's read path), on top of the `whymiss <slot>` primary
command and `whymiss doctor` that arrive with the RCA engine in later
phases (AGENTS.md's fixed CLI surface).

The standard library's `flag` package handles a single flat command fine,
but this tool needs subcommands (`watch`, `timeline`, later `doctor`) each
with their own flags, consistent `-h`/`--help` output, and a stable
argument-parsing contract contributors and operators can rely on without
reading this project's own parsing code.

## Decision

**`spf13/cobra v1.10.2`**, command tree only — no `spf13/viper` (config
binding is `koanf`'s job per BUILD_PROMPT §3, added when a task actually
needs multi-source config; task 2.7's flags are plain per-command cobra
flags for now, not yet backed by a config file).

- One `cobra.Command` per CLI verb, in `cmd/whymiss/`, matching AGENTS.md's
  fixed surface (`whymiss <slot>`, `watch`, `timeline <slot>`, `doctor`) —
  no subcommand beyond that list without human approval, per AGENTS.md.
- `cmd/whymiss/main.go` stays thin (parse, wire, run, exit code), per
  BUILD_PROMPT §4's own comment on that file.

## Consequences

**Good**

- Consistent help text and error messages an operator already recognizes
  from countless other Go CLIs.
- Subcommand addition later (`doctor` in Phase 4) is additive, not a
  rewrite of the argument parser.

**Bad**

- One more line against the fewer-than-15-direct-dependencies budget
  (ADR-0004) — already budgeted for in BUILD_PROMPT §3.
- Cobra pulls in `spf13/pflag` as a transitive dependency (POSIX/GNU-style
  flags). Reviewed and accepted: single-purpose, no network code, widely
  used by the same tools operators already trust cobra-based CLIs from.

**Removal path.** Confined to `cmd/whymiss/`. Every command's actual logic
lives in `internal/app` (the composition root) and is invoked from a thin
`RunE` closure, so replacing cobra means rewriting `cmd/whymiss/`'s command
tree only — no package outside it references cobra types.

## Alternatives considered

**Standard library `flag` plus a hand-rolled subcommand dispatcher.**
Rejected per ADR-0004 rule 3: a dependency that saves reimplementing help
text, flag grouping, and subcommand dispatch is not a one-function
utility grab-bag — cobra is the case the "genuinely worse" exception in
rule 1 is for.

**`urfave/cli`.** Comparable feature set. Rejected only because
BUILD_PROMPT §3 already locked cobra by name; not reopened here per §8's
"a locked technical decision seems wrong is a stop-and-ask."
