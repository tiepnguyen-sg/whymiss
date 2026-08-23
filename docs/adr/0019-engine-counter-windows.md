# ADR-0019: Record exact Engine counter windows

- Status: accepted
- Date: 2026-08-23

## Context

Lighthouse and Prysm expose cumulative Engine API count and duration metrics. An
earlier collector accepted a slot only when both `newPayload` and
`forkchoiceUpdated` advanced exactly once. A real paused-Prysm run advanced
`forkchoiceUpdated` twice for one canonical head, so otherwise valid EL/CL evidence
was discarded. Treating the aggregate as one call would also misstate what was
measured.

## Decision

Sample the counters at consecutive canonical heads and emit one `engine_call`
observation per method. `duration_ms` is the method's total duration in that exact
window and `sample_count` is its exact positive call-count delta. Both required
methods must advance; a reset, missing method, invalid sum, or wider unbounded
window remains insufficient evidence.

This adds an attribute to an existing observation inside the unreleased taxonomy
2.0.0 change set. RCA engine version advances from 0.8.0 to 0.9.0 because valid
multi-call windows can now change a replayed verdict.

## Consequences

- Multiple legitimate `forkchoiceUpdated` calls no longer erase attribution.
- Evidence never presents an aggregate duration as a single call.
- R-300 and R-310 still require an exact bounded window and both Engine methods;
  counter ambiguity degrades instead of being guessed through.
