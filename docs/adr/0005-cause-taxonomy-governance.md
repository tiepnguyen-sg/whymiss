# ADR-0005 · Cause taxonomy governance

- **Status:** accepted
- **Date:** 2026-08-20
- **Deciders:** maintainers
- **Supersedes:** —

## Context

A cause ID such as `local.el_slow.disk_saturation` is not an implementation detail.
Once released it becomes:

- a **Prometheus label value** in operators' dashboards and alert rules;
- a **JSON field value** in scripts operators wrote against `--format json`;
- a **string in an incident thread**, cited months later when a client team asks what
  whymiss said;
- the **label** on corpus scenarios that measure engine accuracy over time.

If `local.el_slow` silently changes meaning between releases, every one of those
breaks, and three of the four break silently. An operator's alert rule keeps firing
on a label that now means something else — the worst possible failure mode for a tool
whose only asset is trust.

Cause IDs are therefore a public API surface, and the loosest part of the system to
change accidentally: adding a string constant requires no schema migration and no
compiler complaint.

## Decision

**`docs/causes.md` is the single source of truth for the taxonomy. Every `Verdict`
embeds the `taxonomy_version` it was produced under. A cause ID never changes
meaning.**

Governance rules:

1. **The document is the contract, the code follows it.** A cause constant that does
   not appear in `docs/causes.md` is a bug, and a test asserts the two agree. Nobody
   invents a cause ID in a rule file.
2. **Every entry documents five things**: definition, the rule that fires it, the
   evidence it requires, how its confidence is derived, and the remediation guidance
   an operator should act on. An entry missing any of the five is incomplete and
   blocks the rule that emits it.
3. **Versioning follows SemVer on the taxonomy, independently of the binary.**
   - *Adding* an ID is a **minor** bump. Existing consumers keep working.
   - *Renaming, removing, or re-scoping* an ID is a **major** bump. "Re-scoping"
     includes narrowing or widening when a rule fires, even with the wording
     unchanged — that is the change most likely to slip through review.
   - Clarifying prose, remediation text, or confidence documentation without altering
     when the ID fires is a **patch**.
4. **Deprecation over deletion.** A retired ID is marked deprecated in the document,
   keeps its meaning, and stops being emitted. It is not reused for anything else,
   ever.
5. **The hierarchy is meaningful.** `local.el_slow.snapshot` is a sub-cause of
   `local.el_slow`. A consumer aggregating on the parent must stay correct, so a
   sub-cause may never be emitted for a situation its parent would not cover.
6. **`unknown.*` is a first-class outcome, not a fallback.**
   `unknown.no_rule_matched` on a complete dataset is a tracked coverage gap and
   should open an issue; `unknown.insufficient_data` is a collection gap. Keeping them
   distinct is what makes I-8 measurable rather than an aspiration.
7. **A taxonomy change requires an ADR** referencing this one, and the corpus
   manifests affected must be updated in the same commit.

## Consequences

**Good**

- Operators can build alert rules on `cause` labels and trust them across upgrades.
- Prometheus label cardinality is bounded and enumerable, because the taxonomy is a
  closed vocabulary in a document rather than whatever strings the rules produce.
- `taxonomy_version` on every verdict makes historical reports self-describing: a
  report from a year ago can be interpreted against the taxonomy it was written under.
- Accuracy measurements stay comparable across releases, because the labels they are
  measured against cannot quietly shift.
- Writing the five required fields before the rule forces the author to know what
  evidence would justify the claim — which is I-7 enforced at design time.

**Bad**

- Getting a new cause into a release is slow: document first, then rule, then corpus
  scenarios. Contributors will feel this.
- Deprecated-but-never-reused IDs accumulate. Accepted; the document grows, which is
  cheap, and the alternative is breaking consumers silently.
- Occasionally a real-world failure will not fit the taxonomy cleanly, and the honest
  answer will be `unknown.no_rule_matched` plus an issue rather than a quick new ID.

## Alternatives considered

**Free-form cause strings produced by rules.** Maximum flexibility, no governance
overhead. Rejected: unbounded Prometheus cardinality, no possibility of a stable
alert rule, and no way to measure accuracy per cause over time.

**Numeric cause codes with a lookup table.** Compact and rename-safe, since prose can
change without touching the code. Rejected: `WM-2041` in an incident thread requires
a lookup, so nobody uses it, and the entire point is that a human reads the verdict
and acts on it.

**Taxonomy versioned together with the binary.** One version number, less
bookkeeping. Rejected: it forces a major release of the tool for a taxonomy rename,
or hides a taxonomy break inside a patch release. Independent versioning lets each
change signal honestly.
