# Contributing to whymiss

Thank you for considering a contribution. This document is the short version; the
engineering contract lives in [`docs/BUILD_PROMPT.md`](docs/BUILD_PROMPT.md) and its
sections 1–8 are binding on every change, including yours.

## Before you write code

Read these three files. They will save you a rejected pull request:

| File | Why |
|---|---|
| [`docs/BUILD_PROMPT.md`](docs/BUILD_PROMPT.md) §2 | The sixteen invariants. Violating one fails the build. |
| [`docs/BUILD_PROMPT.md`](docs/BUILD_PROMPT.md) §5 | Coding standards — interfaces, errors, context, naming. |
| [`docs/causes.md`](docs/causes.md) | The cause taxonomy. Never invent a cause ID that is not in there. |

## What gets accepted quickly

- A new rule in `internal/rca/rules/` that comes with corpus scenarios proving it
  fires when it should and stays silent when it should not.
- A client adapter under `internal/source/` that touches nothing outside that
  directory plus one line of `registry.go` (I-11).
- Corpus scenarios. These are the most valuable contribution to the project and the
  hardest to produce — see [Corpus scenarios](#corpus-scenarios).
- Documentation that removes a step from an operator's first five minutes.

## What gets rejected

- A verdict the evidence does not support. Every verdict carries evidence (I-7), and
  `unknown` is always preferable to a confident guess (I-8).
- A hand-written mock of a beacon-node response. Record a real one into `testdata/`.
- A new third-party dependency without an ADR (I-14).
- Anything that reads, requests, or even references validator keys (I-2). There is no
  version of this that gets merged.
- A feature nobody asked for. The CLI surface is deliberately four commands.
- `TODO` markers, commented-out code, weakened tests.

## Workflow

```sh
make help                    # every supported workflow is a make target
make ci                      # the gate. Must pass before you open a PR.
```

Do not run ad-hoc shell in place of a make target. If a workflow you need is
missing, add it to the `Makefile` in your pull request.

1. Open an issue first for anything larger than a bug fix. Alignment before code.
2. Branch, commit in [Conventional Commits](https://www.conventionalcommits.org/)
   form with the package as the scope: `feat(rca): add el_slow.pruning rule`.
3. One logical change per commit. No mixed refactor-and-feature commits.
4. Update `CHANGELOG.md` in the same commit as the code (I-16). CI enforces this.
5. Run `make ci` and paste the result in the pull request.

Install the pre-commit hooks once, and they will catch most of the above before you
push:

```sh
lefthook install
```

## Architecture decisions

An ADR is written **before** the implementation it justifies, in `docs/adr/` as
`NNNN-title.md`. An ADR written afterwards is a rationalisation, not a decision
record. You need one for: a new dependency, a change to a locked technical decision
(BUILD_PROMPT §3), a change to rule ordering, or a taxonomy change.

## The two pure packages

`internal/domain` imports only the standard library. `internal/rca` imports only the
standard library and `internal/domain`. This is I-6, enforced by `depguard` in
`.golangci.yml` and by `make check.purity`.

If you find yourself wanting an HTTP client inside `internal/rca`, the design has
gone wrong — the data should already be in the `Timeline`. Ask in the issue rather
than working around the boundary.

`internal/domain` is additionally **frozen**: changing a type there breaks every
corpus fixture, so it needs maintainer agreement before you start.

## Corpus scenarios

`test/corpus/` is not test scaffolding. It is the regression suite, the accuracy
benchmark, and the evidence the project works. Scenarios are reviewed with the same
rigour as production code.

A scenario is a directory containing:

```
test/corpus/<scenario-id>/
├── manifest.yaml        expected cause, confidence, description
├── observations.jsonl   recorded raw observations
└── README.md            what was broken and how
```

Generate one reproducibly rather than writing it by hand:

```sh
make devnet.up
make corpus.generate SCENARIO=vc-frozen-lighthouse BEACON=cl-1-lighthouse-geth
make corpus.validate
```

Scenarios that *should* yield `unknown` are as valuable as clear-cut ones. A corpus
containing only easy cases measures nothing.

## Licence

Contributions are accepted under the [Apache-2.0](LICENSE) licence. By opening a pull
request you confirm you have the right to submit the work under that licence.
