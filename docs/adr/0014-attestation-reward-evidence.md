# ADR-0014: Persist verified attestation reward evidence

- Status: accepted
- Date: 2026-08-23

## Context

`inclusion_delay == 1` is necessary but not sufficient for Ethereum's timely-head
participation flag. The included attestation must also vote for the canonical head
and target roots. Treating inclusion delay alone as proof can label a degraded duty
healthy with high confidence, contrary to I-8 and `docs/causes.md`.

The existing `attestation_included` observation already carries the attestation's
voted block root, but it does not record whether the collector verified the voted
head and target against the canonical chain.

## Decision

Add the bounded boolean attributes `head_correct` and `target_correct` to
`attestation_included` observations. The Beacon API adapter derives them by:

1. reading the attestation data from the canonical inclusion block;
2. resolving the canonical block root at the duty slot and at the target epoch's
   first slot, walking backwards across at most 64 skipped slots; and
3. comparing those canonical roots with the attestation's voted head and target.

Canonical-root reads are coalesced and cached briefly with a hard entry bound. If
the evidence cannot be resolved, collection reports an error and does not invent a
boolean. Replay data lacking either attribute is classified as
`insufficient_data` rather than receiving a healthy verdict.

The change is additive: existing observation kinds and cause IDs retain their
meaning. Older corpus files remain parseable but cannot support a complete reward
verdict until regenerated.

## Consequences

- Healthy/degraded outcomes reflect the consensus participation-flag conditions
  instead of inclusion delay alone.
- Skipped duty slots are handled by resolving the most recent canonical ancestor.
- The adapter performs bounded extra read-only Beacon API calls.
- Corpus generation must persist the same evidence as the production collector.
