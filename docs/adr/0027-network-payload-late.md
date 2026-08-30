# ADR-0027: `network.payload_late` — naming the builder's failure under ePBS

- Status: accepted, implemented 2026-08-30 (one cause, no sub-causes — see below)
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

## The observation source, found by looking at real blocks

Every Gloas block carries `payload_attestations` in its body, and each one reads:

```json
{"beacon_block_root": "0xc016...", "slot": "121757",
 "payload_present": true, "blob_data_available": true}
```

`payload_present` is the payload-timeliness committee's own verdict on the
question this cause asks, and it arrives through the **standardised Beacon API**,
in a block whymiss already fetches for inclusion checking. There is no need for a
per-client metric and therefore no per-client adapter — the same conclusion
ADR-0023 reached for peer count and ADR-0025 for the network baseline, and the
direction I-11 points.

That materially shrinks this work. An earlier reading of it assumed the timing
had to come from a client gauge (Lighthouse's
`beacon_block_delay_available_slot_start`), which would have meant an adapter per
client and a much larger change.

**And the condition occurs on its own, often.** Across 51 PTC votes in 51
consecutive blocks on `glamsterdam-devnet-8` on 2026-08-30, **32 reported
`payload_present: false`** while the chain itself produced blocks in 41 of 41
slots. A corpus record for this cause therefore does not need an injected fault
at all: the network supplies the condition, which is stronger evidence than a
fault of our own making.

## Evidence required

No verdict without evidence (I-7), and this cause needs positive evidence, not
the absence of something:

- the payload-reveal deadline in force, from the schedule the node itself
  reported (ADR-0026), so the verdict states the deadline it measured against;
- the PTC's `payload_present` vote for the slot, read from the block body — the
  committee's own finding rather than an inference of ours;
- the consensus block's existence, since a skipped slot is
  `network.proposer_missed` and not this.

Confidence is **high** only when the reveal time was measured. A duty inferred to
have lost a flag with no measured reveal remains `unknown.insufficient_data`,
following R-100's precedent (ADR-0021) that absence is not an exonerating
observation.

## Sub-causes are not implemented, and should not be

The decision above named `revealed_late` and `never_revealed`. Neither is
implemented, because **nothing whymiss observes can tell them apart**:
`payload_present` says the payload was not there in time and says nothing about
whether it arrived afterwards. Shipping the distinction would be a confident
guess, which is the failure I-8 exists to prevent, so `network.payload_late` has
no sub-causes until an observation exists that separates them.

## The cause's natural subject is a proposer, not an attester

Measured 2026-08-30 by watching 64 validators on the public Glamsterdam network
for an hour: **52 duties tracked, every one of them clean.** Not one attester lost
a reward flag, while over the same period the payload-timeliness committee was
reporting the payload absent on roughly half of all slots it voted on (21 of 41 in
one window, 0 of 12 in another — the rate swings).

That is consistent with what ePBS is *for*. The consensus block and the execution
payload are decoupled: the block arrives on time and the attester votes on it,
whatever the builder does afterwards. So a late payload appears not to cost an
attester anything, and R-120 is gated on the duty having actually lost something —
correctly, because a verdict handed to a duty that lost nothing is worse than no
verdict.

The duty a late payload does cost is the **proposer's**: its block exists without
the payload it sold, and the builder's payment with it. whymiss does not track
proposer duties yet — a known issue of long standing, recorded in `CHANGELOG.md`.

So the obstacle to measuring this cause is not the corpus format, which is now
ready, and not access to a network, which exists. **It is that the duty this cause
befalls is not one whymiss watches.** That is worth knowing before anyone spends
more time on the record.

This is stated as what the evidence supports, not as proof: 52 clean duties is
consistent with attesters being insulated, and does not establish that a late
payload can never cost one.

## Why the corpus record comes from a public network

Measured on 2026-08-30:

**Correction, same day.** An earlier version of this section concluded that no
healthy ePBS network existed and that the work had to wait for Sepolia's fork on
2026-09-28. That was wrong, and wrong because nobody looked: **public Glamsterdam
networks are running today**. `beacon.glamsterdam-devnet-8.ethpandaops.io` and
`beacon.plataberget.ethpandaops.io` both serve a Gloas chain — fork `0x80733183`,
active from epoch 1536, `PAYLOAD_DUE_BPS` 5000 and `PAYLOAD_ATTESTATION_DUE_BPS`
7500 — and measured on 2026-08-30 that chain produced **41 blocks in 41 slots**
with its head exactly at wall clock. whymiss was run against it the same day and collected end to end: it adopted the
schedule from the node's spec (`attestation_deadline=3s`,
`payload_reveal_deadline=6s`, `ptc_deadline=9s`, `post_epbs=true`), tracked four
attester duties, and recorded **`attestation_included` and `collection_completed`
with four verdicts and zero errors** — against **Nimbus**, a client this project
has never written an adapter for. Before the same day's fix to `blocks.go` this
combination produced no `attestation_included` at all. So the defect was in the
local devnet, not in Gloas.

What a public network cannot give is a *controlled* fault: a corpus record for
this cause needs a payload made late on purpose, and nobody may do that to a
shared testnet. That still needs a local devnet, and the local devnet is what
does not work:

**No local ePBS devnet built here is healthy enough to record against.** Three
configurations were tried on 2026-08-30, each with one supernode, one Lighthouse
node and two Prysm nodes:

| Configuration | Result |
|---|---|
| `gloas_fork_epoch: 2`, `mev_type: null` | Chain runs; blocks on **10 of 41** post-fork slots |
| `gloas_fork_epoch: 2`, `mev_type: buildoor` + `epbs_builder` | Blocks on **7 of 13** post-fork slots, head 7 slots behind wall clock |
| `gloas_fork_epoch: 0` (genesis at Gloas) | Lighthouse refuses to start: `Built-in genesis state SSZ bytes are invalid: OffsetsAreDecreasing(0)` |

A builder helps and is plainly required, but does not make the chain healthy. The
degradation is not transition shock that settles: the first run's 10 of 41 spans
slots 64 to 105, while the same chain produced 32 of 32 blocks in the two epochs
before the fork. Scheduling the fork later would postpone the same behaviour, and
starting at it is refused by the client.

| `gloas_fork_epoch: 2`, two Nimbus + one Lighthouse, builder | Blocks on **32 of 90** post-fork slots |
| `gloas_fork_epoch: 2`, 384 validators (128/node), builder | Blocks on **6 of 13** post-fork slots, and **7 Gloas blocks carried 2 payload attestations between them** |

Two guesses were tested and both were wrong. The third row tested the client — the
healthy public chain runs Nimbus — and Nimbus degrades locally too. The fourth
tested scale, on the reasoning that a payload-timeliness committee sampled from 96
validators split three ways is thin; quadrupling to 384 changed nothing.

**What the fourth run did show is the mechanism.** Seven Gloas blocks carried two
payload attestations between them, where the public network carries roughly one
per block. The committee barely votes on a local devnet, which is consistent with
the block loss: a payload nobody attests to in time is a payload the chain does
not build on.

So `ethereum-package` cannot exercise ePBS properly here today, across four
configurations. **Injecting a late payload is therefore not available**, and the
`payload-late` recipe in `tools/faultinjector/scenarios/` is committed against a
devnet that cannot yet run it. The observation and the condition both have to
come from a public network instead.

Every record taken from a chain losing half its slots carries the chain's own
sickness as well as the injected fault, which is how fourteen corpus records were
once generated and thrown away.

**whymiss cannot yet observe a payload reveal at all.** There is no
`payload_revealed` observation kind and no collector for one. The Beacon API
offers no `payload_attestation` SSE topic (the node answers 400), and the closest
signal is Lighthouse's `beacon_block_delay_available_slot_start` gauge — a
client-specific metric, so implementing this means an adapter per client under
I-11, plus a new observation kind in a domain model frozen while the corpus
depends on it. That observation layer, not the rule, is the bulk of task 5.5.

A rule shipped without a corpus record is unmeasured, which this project does not
treat as passing. The record for this cause therefore has to come from the public
Glamsterdam network, where the condition occurs on its own, rather than from a
fault injected into a devnet that cannot stay healthy.

## Consequences

The corpus gains a cause that only a post-ePBS network can produce, so it needs a
Glamsterdam devnet with an ePBS builder — `mev_type: buildoor`,
`buildoor_params.epbs_builder`. A devnet without one produces blocks on roughly a
quarter of slots and cannot evidence anything (measured 2026-08-30: 10 of 41).

`eval.check`'s bar is unchanged and this cause must clear it like any other: no
scenario means unmeasured, which is not the same as passing.
