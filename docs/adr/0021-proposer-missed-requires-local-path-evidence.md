# ADR-0021: R-100 exonerates only when the attester's own attestation is on chain

- Status: accepted
- Date: 2026-08-26

## Context

R-100 fired on a proven `block_skipped` observation alone and reported
`network.proposer_missed` at `high` confidence, with the remediation
`docs/causes.md` gives that cause: none, because "the operator's attestation path
did not cause the canonical skip".

That is sound when the operator's duty completed. It is not sound when nothing
was ever observed from the operator's own attestation path, and ADR-0015 already
says why in its own consequences: *"Attesters may still publish and be included
normally on a skipped slot."* A skip does not explain a missing attestation. So
for a timeline holding only

```
duty_assigned, slot_start, block_skipped, collection_completed
```

the outcome derived is `missed`, and R-100 attributed the entire loss to the
canonical skip and rendered no remediation at all.

Five recorded devnet scenarios have exactly that shape with a validator client
that was paused outright or capped to 0.1% of one core
(`vc-frozen-lighthouse-r02`, `vc-frozen-prysm-r02`, `vc-frozen-prysm-2-r02`,
`vc-slow-cpu-r02`, `vc-slow-cpu-r04`). In every one, whymiss told an operator
whose VC was down, at high confidence, that nothing was theirs to fix.

No later rule rescued it. R-400 is *right* to decline: it requires `block_seen`
and `head_updated` before the attestation deadline to establish that the beacon
node was healthy, and a skipped slot supplies neither. The engine has no rule for
"the upstream proposer missed **and** the local path failed", and R-100 sits
third in `rules.Order()`, ahead of R-400 and R-410, so it answered first and
stopped the search.

Two committed corpus records asserted the old behaviour as correct —
`proposer-missed-concurrent-vc-pause` and `…-prysm`, whose recipe deliberately
pauses the validator client that owns both the attester and the proposer duty.
The previously reported 13/13 accuracy with zero false-high verdicts rested in
part on them.

## Decision

R-100 keeps requiring a positive `block_skipped` proof, and now splits on what
the timeline says about the operator's own attestation:

1. **`attestation_included` exists** — the attestation reached the chain, so the
   validator client, beacon node, and publication path demonstrably worked while
   the slot was skipped. Report `network.proposer_missed` at `high`, citing the
   inclusion alongside the skip. The exoneration is a finding, not an absence
   read as innocence.
2. **`attestation_published` exists but no inclusion** — decline. Non-inclusion
   of an on-time attestation is R-500's question; R-100 answering it would
   exonerate the network for a loss it has not been shown to have caused.
3. **Neither exists** — report `unknown.insufficient_data` at `low`, with
   evidence stating both facts (the chain skipped this slot; nothing was ever
   observed from the local attestation path) and remediation naming the
   validator client as the thing to check.

Case 3 is I-8 applied literally: two readings fit the evidence equally — only the
upstream proposer failed, or it failed while the local path failed too — and
nothing in the timeline separates them. `unknown.insufficient_data` is the
documented cause for "required observations were unavailable, so no honest
attribution is possible", and its evidence contract asks precisely for what is
missing and why it mattered.

Narrowing when an existing cause fires is re-scoping under ADR-0005, so the
taxonomy advances from 2.0.0 to 3.0.0 and the engine from 0.13.0 to 0.14.0.

## Consequences

- An operator with a dead validator client on a skipped slot is no longer told
  the network is at fault. They get `unknown`, the two facts, and a pointer at
  the validator client.
- `network.proposer_missed` at `high` now requires positive evidence, which
  matches how every other high-confidence verdict in this taxonomy is earned.
- The two committed `proposer-missed-concurrent-vc-pause*` records are relabelled
  to `unknown.insufficient_data` / `low`. Their recipes construct the ambiguous
  case by design, so they become the corpus's ambiguity coverage — which Phase 3's
  DoD asks for explicitly ("ambiguous scenarios correctly yield `unknown.*`")
  and the corpus previously had only two records for.
- R-100's exoneration path is covered instead by the recordings where the
  attestation reached the chain and only `timely_head` was lost, which is the
  shape a real mainnet proposer miss produces.
- Rejected: keeping the cause and lowering confidence. The misleading part is the
  cause's meaning — "the network did this, not you" — and no confidence value
  repairs a claim that points at the wrong layer.
- Rejected: a new compound cause for "both". The taxonomy is closed on purpose
  (I-8), a third state would need its own evidence and remediation contract, and
  nothing in the timeline can establish the local half of the claim.
