# Extending whymiss

Two things outside contributors are most likely to want to add: **a client
adapter**, so whymiss understands a beacon node it currently doesn't, and **a
rule**, so it can name a cause it currently can't.

They have very different costs. An adapter is a contained change inside one
package and needs no permission. A rule that introduces a new cause changes the
taxonomy, which is a versioned contract other people's alerting depends on, and
that needs an ADR before any code.

This guide assumes you've read [`architecture.md`](architecture.md) §4, which
lists the boundaries `make check` enforces. Everything here is checkable: if a
step is wrong, a target in `make ci` fails rather than the mistake shipping.

---

## Part 1 — Adding a client adapter

### What you will not have to touch

Start here, because it's the part that surprises people. Adding a consensus
client touches **only `internal/source/`**. `internal/domain`,
`internal/timeline`, `internal/store`, `internal/rca`, and `cmd/whymiss` don't
reference consensus clients at all.

That isn't a convention to be careful about — it's enforced.
`make check.isolation` greps for client names outside `internal/source/` and
fails the build, and it already runs inside `make ci`. If you find yourself
adding a `case ConsensusTeku:` in `internal/app`, the design has gone wrong and
CI will say so.

`architecture.md` §5 walks the same change from the architecture side. This
section is the contributor's version of it.

### The three files

Using Teku as the worked example:

**1. `internal/source/registry.go`** — the one file where client names may
appear. Add the constant and the detection arm:

```go
ConsensusTeku ConsensusClient = "teku"
```

and in `DetectConsensusClient`, a `strings.HasPrefix(versionString, "Teku")`
case. Detection reads the string `GET /eth/v1/node/version` returns, so check
what your node actually reports before guessing the prefix.

**2. `internal/source/promscrape`** — the adapters that turn that client's
Prometheus output into normalised values. You need two:

- slot-qualified block arrival
- cumulative Engine-call counters

**Record a real scrape first.** Do not hand-write the metric names from
documentation. The two existing clients use
`beacon_block_delay_observed_slot_start` (Lighthouse) and
`block_arrival_latency_milliseconds_gauge` (Prysm) — names with nothing in
common. There is no convention to generalise from, and
[AGENTS.md](../AGENTS.md) forbids hand-written node responses for exactly this
reason: capture a real one into `testdata/`.

**3. `internal/source/peers.go`** — add `ConsensusTeku` arms to
`SampleBlockTiming` and `SampleEngineCounters` so the dispatch reaches your
adapters.

### What you get for free, and why that matters

Peer count needs no adapter. It comes from `GET /eth/v1/node/peer_count`, a
spec-defined endpoint every client serves identically (ADR-0023). The network
baseline is the same when `--baseline-metrics-api` is unset: it polls the
independent node's own `/eth/v1/beacon/headers/{slot}` (ADR-0025).

Both used to be per-client adapters, and one of them was **wrong**: Lighthouse's
`libp2p_peers` gauge reads 0 on a genuinely peered node, which made R-200's peer
corroboration silently vacuous there for as long as it existed.

The lesson generalises, and it's the one piece of judgement this guide most wants
to pass on: **when the Beacon API exposes the same fact, take it from there.**
Doing so deletes client-specific code rather than adding more, which is the
direction I-11 points. If you're about to write a fourth Prometheus adapter,
check first whether the standardised endpoint already carries the fact.

### Verifying your adapter

```sh
make check.isolation   # fails if a client name escaped internal/source/
make test              # your adapter's tests, against recorded testdata
make ci                # everything, including the above
```

An adapter is done when `make ci` passes and your `testdata/` fixture came from
a real node.

---

## Part 2 — Adding a rule

### Read this before writing code

A rule that reports an existing cause is ordinary work. A rule that introduces a
**new cause** is a taxonomy change, and
[`docs/adr/0005-cause-taxonomy-governance.md`](adr/0005-cause-taxonomy-governance.md)
governs it. Its rules that bind you:

- **The document is the contract; the code follows it.** `docs/causes.md` is
  edited first. A cause constant with no entry there fails
  `TestTaxonomyMatchesDocs` in `internal/domain`, which compares
  `domain.CauseIDs()` against the document.
- **Every entry documents five things**: definition, the rule that fires it, the
  evidence required, how confidence is derived, and remediation.
- **A taxonomy change requires an ADR** referencing ADR-0005, and the corpus
  manifests it affects must be updated in the same commit.
- Adding an ID is a **minor** taxonomy bump; renaming or re-scoping one — which
  includes *changing when an existing rule fires, even with the wording
  unchanged* — is a **major** bump.

[AGENTS.md](../AGENTS.md) also lists "a cause ID needs renaming or re-scoping"
under *stop and ask the human*. Open an issue with the evidence before writing
the rule.

### The interface

```go
type Rule interface {
	ID() string
	Evaluate(domain.Timeline, Config) (*domain.Verdict, bool)
}
```

Return `(nil, false)` to decline. Returning a verdict claims the duty, because
evaluation is **first match wins** (ADR-0003) — so where you insert yourself in
`rules.Order()` is part of the design, not an afterthought. A rule placed above
R-110 will shadow it for every timeline they both match.

### The three constraints that will fail your build

**`internal/rca` is pure (I-6).** No I/O, no clock, no randomness, no
goroutines, and it may import only the standard library and `internal/domain`.
`make check.purity` enforces the import list. If your rule needs a fact it
doesn't have, the fix is to collect that fact into the `Timeline` upstream — not
to reach for it from the rule.

**No verdict without evidence (I-7).** Every `domain.Verdict` needs at least one
`Evidence` entry. `Statement` is one line, present tense, no speculation — its
own doc comment says that if it needs a hedge, it isn't evidence. Attach a
`Comparison` wherever you have an observed-against-expected pair; that's what
turns an assertion into an argument.

**Prefer `unknown` over a guess (I-8).** A wrong confident verdict is worse than
no verdict. Two live examples of this being taken seriously: R-100 refuses to
exonerate a duty on the *absence* of evidence and reports
`unknown.insufficient_data` instead (ADR-0021), and R-999 distinguishes "nothing
was measured" from "everything was measured and no rule matched" (ADR-0024)
because telling an operator their configuration is a project bug is a worse
failure than admitting ignorance.

### Prove it against real data

A rule is not finished when its unit tests pass. The corpus is the bar:

```sh
make eval          # per-cause accuracy across test/corpus/, writes docs/evaluation.md
make eval.check    # the release gate: >= 50 scenarios, >= 90% top-1, ZERO false-high
```

**Zero false-high is absolute.** A single high-confidence verdict that is wrong
on any corpus scenario fails the build, and is treated as a release blocker.

If your cause has no corpus scenario yet, it is *unmeasured*, which is not the
same as passing. Producing one means a real fault injected against a real
devnet — see `tools/faultinjector/scenarios/` for recipes and their bisection
logs. Those logs are worth reading before you write a new one: several record
faults that worked perfectly and still produced no usable evidence, and the
reasons are not obvious.

Never edit a recorded observation to make a scenario match its label. The
generator writes what it measured regardless of the expectation, and a record
whose facts were adjusted to fit is worthless as a fixture.

---

## Checklist before opening a pull request

1. `make ci` passes. Paste the output — this project treats an unverified
   "it works" as the failure mode that causes the most damage.
2. `CHANGELOG.md` updated in the same commit.
3. Docs your change touched updated in the same commit — for a new cause that
   means `docs/causes.md`, and an ADR.
4. New third-party dependency? That needs an ADR too (I-14).
5. Conventional Commits, scope is the package: `feat(source/beaconapi): …`.

Security issues go through the process in [`SECURITY.md`](../SECURITY.md), not a
public pull request.
