# ADR-0027: `network.payload_late` — naming the builder's failure under ePBS

- Status: proposed — approved in principle, blocked on evidence
- Date: 2026-08-30
- Governed by: [ADR-0005](0005-cause-taxonomy-governance.md)

## Context

Under EIP-7732 a slot has two producers, not one. The proposer publishes a
consensus block carrying a signed bid; the **builder** reveals the execution
payload separately, by `PAYLOAD_DUE_BPS` into the slot — 6s of a 12s slot on the
Glamsterdam devnet measured on 2026-08-30. A payload-timeliness committee then
votes on whether it arrived in time, by `PAYLOAD_ATTESTATION_DUE_BPS` (9s).

This creates a way to lose a duty that the current taxonomy cannot name. The
fourteen causes in `docs/causes.md` divide the world into the operator's own
layers (`local.*`) and things the network did to them (`network.*`). A builder
that reveals late is squarely the second, but none of the three existing
`network.*` causes fits:

- `network.proposer_missed` means the canonical chain skipped the slot. Under
  ePBS the consensus block can exist and be canonical while the payload is late,
  so this would be false.
- `network.late_block` is about the *block* arriving late by gossip, measured
  against an independent baseline. A payload revealed late is a different event
  with a different deadline and a different responsible party.
- `network.inclusion_failure` is about an on-time attestation not reaching a
  block.

Reporting any of those would be a wrong confident verdict, which I-8 ranks worse
than no verdict at all. Today the engine correctly falls through to
`unknown.no_rule_matched` — honest, and useless to the operator it is telling
nothing to.

## Decision

Add **one** cause ID, `network.payload_late`, with two sub-causes:

| Sub-cause | Meaning |
|---|---|
| `revealed_late` | The payload was revealed, after `PAYLOAD_DUE_BPS` |
| `never_revealed` | The slot ended with no payload for a consensus block that existed |

One ID rather than two, because both are the same finding from the operator's
point of view — *this was not you, and there is nothing on your machine to fix* —
and that is the sentence the product exists to be able to say. The distinction
between them is real but is detail, which is what sub-causes are for (ADR-0005
§ sub-causes).

Per ADR-0005 this is a **minor** taxonomy bump: an ID is added, nothing is
renamed or re-scoped. `docs/causes.md` is edited first and this ADR precedes the
code.

### What is deliberately not added

**A PTC cause.** A committee voting the payload untimely is evidence *about* the
payload's lateness, not a separate thing to blame — the operator's remedy is
identical. `payload_attestations` in the block will be read as corroborating
evidence for this cause, not attributed as its own.

**Anything blaming the operator for a payload.** An operator running a validator
has no control over the builder. There is no `local.*` counterpart and there
should never be one.

## Evidence required

No verdict without evidence (I-7), and this cause needs positive evidence, not
the absence of something:

- the payload-reveal deadline in force, from the schedule the node itself
  reported (ADR-0026), so the verdict states the deadline it measured against;
- the observed reveal time, or a bounded statement that the slot ended without
  one;
- the consensus block's existence, since a skipped slot is
  `network.proposer_missed` and not this.

Confidence is **high** only when the reveal time was measured. A duty inferred to
have lost a flag with no measured reveal remains `unknown.insufficient_data`,
following R-100's precedent (ADR-0021) that absence is not an exonerating
observation.

## Why this is not implemented yet

The decision above is approved; the code is not written, and the reason is
evidence rather than effort. Measured on 2026-08-30:

**No ePBS devnet available here is healthy enough to record against.** A
Glamsterdam devnet with `mev_type: null` produced blocks on 10 of 41 slots. Adding
`mev_type: buildoor` with `buildoor_params.epbs_builder` improved it to 7 of 13
post-fork, with the head running 7 slots behind wall clock — still losing about
half of all slots. Every record taken from a chain in that state carries the
chain's own sickness as well as the injected fault, which is how fourteen corpus
records were once generated and thrown away.

**whymiss cannot yet observe a payload reveal at all.** There is no
`payload_revealed` observation kind and no collector for one. The Beacon API
offers no `payload_attestation` SSE topic (the node answers 400), and the closest
signal is Lighthouse's `beacon_block_delay_available_slot_start` gauge — a
client-specific metric, so implementing this means an adapter per client under
I-11, plus a new observation kind in a domain model frozen while the corpus
depends on it. That observation layer, not the rule, is the bulk of task 5.5.

A rule shipped without a corpus record is unmeasured, which this project does not
treat as passing. So `network.payload_late` stays documented and unimplemented:
the ID is not in `domain.CauseIDs()` and `docs/causes.md` does not list it, both
deliberately, so the public taxonomy never advertises a cause nothing can emit.
The entry lands with the rule and its first record, in one commit, as ADR-0005
requires.

## Consequences

The corpus gains a cause that only a post-ePBS network can produce, so it needs a
Glamsterdam devnet with an ePBS builder — `mev_type: buildoor`,
`buildoor_params.epbs_builder`. A devnet without one produces blocks on roughly a
quarter of slots and cannot evidence anything (measured 2026-08-30: 10 of 41).

`eval.check`'s bar is unchanged and this cause must clear it like any other: no
scenario means unmeasured, which is not the same as passing.
