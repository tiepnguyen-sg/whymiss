# ADR-0003 · Pure RCA engine

- **Status:** accepted
- **Date:** 2026-08-20
- **Deciders:** maintainers
- **Supersedes:** —

## Context

The product is a verdict: *"your execution client stalled on disk during slot
12345678, here is the evidence."* Everything else — polling, scraping, storage,
rendering — is plumbing that feeds or renders that verdict.

A verdict has an unusual quality requirement. An operator who acts on a wrong
confident verdict, restarts the wrong component, and misses more attestations will
uninstall and tell others to. The engine's reputation is the product's reputation,
and it is destroyed by a single memorable false positive rather than eroded by
gradual inaccuracy.

That requirement drives three needs:

1. **Reproducibility.** When an operator disputes a verdict, we must be able to
   replay their exact input and obtain their exact output — months later, on a
   different machine, with a different Go version.
2. **Auditability.** A client team reading a whymiss report in an incident thread
   must be able to trace the conclusion to a written rule.
3. **Cheap exhaustive testing.** Accuracy is a measured number (≥90% top-1, Phase 3)
   across ≥50 corpus scenarios, re-measured on every commit. That measurement has to
   run in seconds.

A conventional design — an analyser that queries the store, scrapes a metric it finds
missing, and checks the wall clock to decide whether a deadline passed — fails all
three. Its output depends on when it ran and what the network was doing at the time.

## Decision

**`rca.Analyze(Timeline, Config) Verdict` is a pure function. `internal/rca` performs
no I/O, no clock reads, no randomness, and starts no goroutines. It may import only
the standard library and `internal/domain`.**

The corollaries are the architecture:

- **The `Timeline` is self-contained.** Every fact the engine could need — duty,
  observations, metric samples, the slot's start time, the clock offset measured at
  sample time, the optional network baseline — is materialised into it before the
  engine is called. If a rule needs something the `Timeline` lacks, the fix is in
  `internal/timeline`, never a lookup from inside the engine.
- **Time is data.** The engine never asks what time it is. Deadlines are computed
  from `Timeline.SlotStart` and the declarative `SlotSchedule`. This is what makes
  the Phase 5 ePBS work a configuration change rather than a rewrite.
- **Rules are ordered explicitly, first match wins.** The ordering lives in one file,
  with a written justification per position. Ordering is behaviour, so changing it
  requires an ADR.
- **One rule per file**, implementing a two-method interface. A rule that does not
  apply returns `(nil, false)` and says nothing.
- **Evidence is constructed, not derived.** `Verdict` construction fails if the
  evidence slice is empty (I-7), which makes I-8's "prefer unknown" the path of
  least resistance rather than a discipline.

Enforcement is mechanical, in three layers: `depguard` in `.golangci.yml` is the
primary gate, `make check.purity` greps as belt-and-braces, and a determinism test
analyses the same timeline 1,000 times asserting byte-identical output.

## Consequences

**Good**

- A corpus scenario is a recorded `Timeline` plus an expected `Verdict`. The whole
  accuracy suite is table-driven, runs without a node, and finishes in seconds.
- Golden-file tests are meaningful: a diff in engine output is always a behaviour
  change, never flake. Reviewers can treat golden diffs as code, which the
  contribution guide requires.
- Determinism is testable rather than hoped for.
- The engine can be handed to a client team, run against their timeline, and produce
  the same answer we saw. That is the conversation the project wants to be able to
  have.
- No lock contention, no context plumbing, no shutdown path inside the engine — the
  package that carries the most logic carries none of the concurrency risk.

**Bad**

- `internal/timeline` becomes the load-bearing complexity: it must decide what to
  collect *before* knowing which rule will need it. Over-collecting costs memory
  (I-12); under-collecting yields `unknown.insufficient_data`. Accepted, and
  deliberately visible — `insufficient_data` verdicts are a monitored signal that the
  assembler needs work, not a failure to hide.
- A rule cannot lazily fetch a metric to break a tie. It must degrade to `unknown`
  instead. This is the intended trade (I-8) but will feel wrong to contributors used
  to analysers that can go and look.
- Adding a genuinely new input means touching `domain`, `timeline`, and the corpus
  format together. The domain freeze after Phase 1 makes that deliberately expensive.

## Alternatives considered

**Analyser with repository access.** The engine takes a `Store` interface and queries
what it needs. Rejected: verdicts become dependent on retention state and on when the
analysis ran, golden tests need a database fixture, and the determinism guarantee
disappears. This is the design the ADR exists to forbid.

**Streaming rule evaluation during collection.** Evaluate rules as observations
arrive, avoiding the timeline materialisation step. Rejected: rules need the whole
slot window — "the attestation was published on time but never included" is not
decidable at publication. It also couples rule logic to arrival order, which is the
least reproducible thing in the system.

**Scored / probabilistic verdicts.** Weight signals and emit the highest-scoring
cause with a confidence number. Rejected: it produces a plausible answer for every
input, which is the opposite of I-8. It also makes "why did it say that" unanswerable
in an incident thread, forfeiting the auditability the product sells. Explicitly a
non-goal per BUILD_PROMPT §1.1 and §11.4.

**Machine learning over the corpus.** Rejected by BUILD_PROMPT §1.1: every verdict
must be traceable to a written rule. A model also cannot be audited by the client
team whose software it just accused.
