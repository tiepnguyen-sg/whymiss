# CLAUDE.md

@AGENTS.md

---

## Claude Code specific

Everything above is the shared contract. Only Claude-specific operating notes go below.
If a rule applies to every agent, it belongs in `AGENTS.md`, not here.

### Session start

Begin every session by reading `docs/BUILD_PROMPT.md`. Sections 1–8 are LAW.
Confirm which phase and task we are executing before writing any code.

### Plan mode

Use plan mode for any task touching `internal/rca` or `internal/domain`.
Those two packages are load-bearing: the RCA engine is the product, and the domain
model is frozen after Phase 1 because the test corpus depends on it.

### Parallel work

Do not run parallel subagents on tasks that touch the same package. The corpus
fixtures and golden files are order-sensitive and will produce spurious diffs.

### Verification

Never report a task complete without pasting the actual output of `make ci`.
Claiming a command passed without running it is the single failure mode that
causes the most damage here.
