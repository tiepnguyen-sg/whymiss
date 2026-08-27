# AGENTS.md

Single source of truth for every AI coding agent working in this repository.
Claude Code reads this via `CLAUDE.md`. Codex, Cursor, Copilot, and Gemini CLI read it directly.

**Keep this file under 8 KB.** Codex truncates once accumulated instruction files
exceed 32 KiB, and files closest to the working directory are truncated first.
Detail belongs in `docs/`, not here.

---

## Project

**whymiss** — a read-only sidecar that runs next to an Ethereum node and, when a
validator misses or is late on a duty, produces a forensic post-mortem naming the
responsible layer with timestamped evidence.

The one question the product answers: *"Was it me, or was it the network — and if it
was me, which layer?"*

Go 1.23+ · single static binary · `CGO_ENABLED=0` · Apache-2.0

---

## Read before doing anything

`docs/BUILD_PROMPT.md` is the engineering contract. Sections 1–8 of that document are
LAW and apply to every task in this repository. Read it at the start of every session.

`docs/causes.md` is the cause-taxonomy contract. Never invent a cause ID that is not
in that file.

---

## Non-negotiable invariants

Full text in `docs/BUILD_PROMPT.md` §2. Summary — violating any of these is a build failure:

- **I-1** Read-only against the beacon node. No mutating calls.
- **I-2** Never read, request, or reference validator keys, keystores, or signer credentials.
- **I-3** Runs unprivileged. No root, no added capabilities.
- **I-4** No egress by default. No telemetry, no version checks, no analytics.
- **I-5** Never degrade the node. Every call has a timeout, a rate limit, and backoff.
- **I-6** `internal/rca` is pure: no I/O, no clock, no randomness, no goroutines.
- **I-7** No verdict without evidence.
- **I-8** Prefer `unknown` over a guess. A wrong confident verdict is worse than no verdict.
- **I-9** Clock discipline. No timing verdict on an untrusted clock.
- **I-11** Client-specific code lives only in `internal/source/**`.
- **I-12** Bounded memory, disk, and connections. Must be safe on a Raspberry Pi 5.
- **I-14** New dependencies require an ADR.
- **I-15** No panics outside `main`. Wrap errors with `%w`.

---

## Commands — use these, not raw tooling

```
make build      # build the binary
make test       # unit tests
make lint       # golangci-lint
make check      # invariant boundary checks (purity, client isolation, egress)
make ci         # everything above — MUST pass before any task is declared done
make eval       # RCA accuracy report against the corpus
```

Do not invent your own command sequences. If a workflow is missing from the
`Makefile`, add it there rather than running ad-hoc shell.

---

## CLI surface — fixed

The binary name *is* the question. Do not add a subcommand that restates it.

```
whymiss <slot>              # THE primary command. Explains that slot.
whymiss watch               # collector daemon
whymiss timeline <slot>     # raw timeline, no interpretation
whymiss doctor              # verify every configured endpoint, storage, clock
```

There is deliberately no `whymiss explain` — `whymiss 12345678` already reads as
"why miss slot 12345678". Adding subcommands beyond this list requires human
approval; the surface is small on purpose.

---

## Definition of done for any task

1. `make ci` passes.
2. `CHANGELOG.md` updated in the same commit.
3. Docs touched by the change are updated in the same commit.
4. Task Report emitted (format in `docs/BUILD_PROMPT.md` §8).

A task is not done because the code compiles.

---

## Stop and ask the human when

- A task appears to require violating an invariant.
- A new third-party dependency is needed.
- The domain model in `internal/domain` needs to change.
- A cause ID needs renaming or re-scoping.
- Requirements are ambiguous.

**Do not guess and proceed.** Ambiguity resolved by guessing is how two agents diverge.

---

## Never

- Add unrequested features.
- Leave `TODO` in merged code — open an issue and reference it.
- Commit commented-out code.
- Weaken or skip a test to make it pass.
- Add a `//nolint` without an inline justification comment.
- Hand-write a mock beacon-node response. Record a real one into `testdata/`.
- Mark a checklist item done without running its verification command.

---

## Commits

Conventional Commits. Scope is the package.

```
feat(rca): add el_slow.snapshot rule
fix(source/beaconapi): reconnect SSE stream after idle timeout
docs(causes): document confidence derivation for p2p_degraded
```

One logical change per commit. No mixed refactor-and-feature commits.

---

## Code style

Full standards in `docs/BUILD_PROMPT.md` §5. The three that matter most:

- Interfaces are defined by the **consumer** and have 1–3 methods.
- Constructor injection only. No global mutable state, no `init()` side effects.
- `internal/app` is the only package that knows how things wire together.
