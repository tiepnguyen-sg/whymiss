# ADR-0004 · Dependency policy

- **Status:** accepted
- **Date:** 2026-08-20
- **Deciders:** maintainers
- **Supersedes:** —

## Context

whymiss asks an operator to run our code on the machine holding their validator
keys. Every transitive dependency is code that operator did not choose, cannot
realistically audit, and must nonetheless trust.

The relevant risk is not primarily a hostile package. It is the ordinary case: a
widely-used library adds a telemetry callback, an HTTP client with a default global
transport, an `init()` that reads an environment variable, or a logging side effect.
Any of those would silently violate I-4 (no egress by default) — the invariant the
threat model leads with. A dependency tree of three hundred modules cannot be
credibly claimed to make no outbound calls.

Supply-chain compromise of Go modules is also no longer hypothetical, and a tool
distributed to staking infrastructure is a high-value target for exactly that.

## Decision

**Fewer than 15 direct dependencies at v1.0. Adding one requires a merged ADR that
justifies it, names the alternative considered, and states the removal path.**

The rules:

1. **The standard library is the default.** `net/http`, `encoding/json`,
   `database/sql`, `log/slog`, and `testing` cover most of what this tool does. Reach
   for a dependency only when the standard library answer is genuinely worse, not
   merely more verbose.
2. **No test frameworks.** Table-driven tests with `testing` and golden files. An
   assertion library is not worth a dependency (BUILD_PROMPT §3).
3. **No utility grab-bags.** A dependency that provides one function we could write
   in twenty lines is refused.
4. **Dependencies arrive in the phase that needs them, not earlier.** Phase 1 has
   zero. `cobra` enters with the CLI in Phase 2, not with the scaffold.
5. **Purity boundaries are enforced, not documented.** `internal/domain` and
   `internal/rca` have `depguard` allowlists (I-6, ADR-0003), so no dependency can
   reach the two packages that constitute the product.
6. **Pinned and verified.** `go.sum` is committed, `govulncheck` runs in CI, GitHub
   Actions are pinned by commit SHA, and releases carry SBOM plus provenance.
7. **Vendoring is not used.** It obscures the dependency count, which is the number
   this policy exists to keep honest.

The locked choices in BUILD_PROMPT §3 — `cobra`, `koanf`, `modernc.org/sqlite`,
`client_golang`, `parquet-go` — are pre-approved by that document, but each still
lands in its own ADR when first added, recording the version and the removal path.

## Consequences

**Good**

- The claim "this makes no outbound calls you did not configure" stays auditable by a
  motivated operator in an afternoon. That audit is the product's trust story.
- `govulncheck` output stays short enough that a finding gets acted on rather than
  triaged away.
- Build reproducibility and cross-compilation stay simple.
- The ADR requirement makes dependency addition a design conversation rather than a
  reflex, and the removal path forces us to keep the boundary that would allow it.

**Bad**

- More code we maintain ourselves, including boring code: metric-name normalisation,
  a small exponential-backoff helper, JSONL round-tripping. Bugs in that code are
  ours.
- Contributors will occasionally have a pull request blocked on "write the ADR
  first", which is friction at exactly the moment enthusiasm is highest. Mitigated by
  saying so plainly in the contribution guide.
- Rejecting an assertion library makes some tests more verbose than a contributor
  would like.

## Alternatives considered

**No policy, review dependencies case by case in pull requests.** The realistic
default. Rejected because "case by case" under time pressure means yes, and the
resulting tree is not defensible to a suspicious operator six months later. The
policy's value is that it applies before anyone has written code that depends on the
answer.

**Hard numeric cap on total (transitive) modules.** More honest about real risk than
counting direct dependencies. Rejected as unenforceable: one legitimate addition can
pull thirty transitive modules through no fault of ours, and the cap would then block
unrelated work. The direct-dependency count plus a mandatory ADR puts the judgement
where it belongs. Transitive weight is still a review consideration and belongs in
each dependency's ADR.

**Vendor everything.** Guarantees reproducibility and makes the tree auditable in the
repository. Rejected: it hides growth and makes the dependency count invisible in
review, which defeats the purpose. `go.sum` plus pinned CI gives the reproducibility
without the concealment.
