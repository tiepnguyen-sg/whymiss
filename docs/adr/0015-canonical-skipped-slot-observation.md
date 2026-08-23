# ADR-0015: Record canonical skipped slots explicitly

- Status: accepted
- Date: 2026-08-23

## Context

The absence of `block_seen` does not prove that nobody proposed. The same shape is
produced when the collector, Beacon API, or local node fails. R-100 previously used
that absence as a medium-confidence inference and could incorrectly exonerate an
operator.

## Decision

Add `block_skipped` to the observation vocabulary. The Beacon API adapter emits it
only after the observation window closes and both conditions hold:

1. `GET /eth/v1/beacon/headers/{slot}` returns 404; and
2. `GET /eth/v1/node/syncing` reports `is_syncing=false`,
   `sync_distance=0`, `is_optimistic=false`, `el_offline=false`, and
   `head_slot` strictly greater than the slot being checked; and
3. a second header lookup still returns 404 after the sync-status check.

R-100 requires this positive fact. A missing block observation without a verified
skip falls through to insufficient/unknown attribution. An operator's own proposer
duty remains excluded from R-100 because a local proposal failure is not an
exonerating network event.

Adding the observation kind would ordinarily be a minor taxonomy change, but making
it mandatory narrows when the existing R-100 cause fires. ADR-0005 classifies that
as re-scoping, so the taxonomy advances from 1.0.0 to 2.0.0. The RCA engine advances
from 0.3.0 to 0.4.0.

## Consequences

- Collector outages can no longer masquerade as skipped proposer slots.
- A node that is merely caught up at the current slot cannot prematurely call that
  slot skipped.
- Attesters may still publish and be included normally on a skipped slot; those
  observations do not invalidate `block_skipped`.
- Existing corpus scenarios must be regenerated to exercise R-100.
