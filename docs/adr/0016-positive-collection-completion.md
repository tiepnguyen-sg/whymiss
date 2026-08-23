# ADR-0016: Require positive collection completion

- Status: superseded by ADR-0018
- Date: 2026-08-23

## Context

This ADR originally assumed an attestation could be included up to
`SLOTS_PER_EPOCH` slots after its duty. ADR-0018 corrects that pre-Deneb assumption;
the positive-completion decision remains in force. The
collector previously checked only two slots and inferred that collection was
complete from wall-clock age. A request failure therefore looked identical to a
real absence, and an attestation included after slot N+2 was reported as missed.

## Decision

Use the protocol's complete inclusion window. Add `collection_completed` to the
observation vocabulary and emit it only after every required query succeeds through
that window and its final slot has ended. Timeline validation rejects an attester
completion marker timestamped before that boundary. The marker is generated from a
local wall-clock read, so it receives the same measured NTP correction as adapter
timestamps; only canonical `slot_start` remains unshifted. Live assembly and corpus
replay require this positive observation before any absence-based rule may run.
Elapsed wall time alone never establishes data completeness.

ADR-0018 supersedes the original timely-target delay decision for Deneb-or-later
networks. Timely-head remains independently based on a correct head and delay one.

This additive observation is included in the unreleased taxonomy 2.0.0 change set.
The RCA engine advances from 0.4.0 to 0.5.0.

## Consequences

- A missed-attestation verdict is delayed until the end of the following epoch.
- API and persistence failures degrade to `unknown.insufficient_data` instead of a
  false causal verdict.
- Existing corpus fixtures must be regenerated; old fixtures intentionally fail the
  completeness gate.
