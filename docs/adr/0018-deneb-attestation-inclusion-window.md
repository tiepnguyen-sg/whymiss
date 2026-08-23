# ADR-0018: Track the full Deneb attestation inclusion window

- Status: accepted
- Date: 2026-08-23

## Context

ADR-0016 treated `SLOTS_PER_EPOCH` as a fixed maximum attestation inclusion
delay. That was the Altair-era bound. Deneb's EIP-7045 permits an attestation to
be included while its target epoch is either the current or previous epoch. The
latest valid inclusion slot is therefore the last slot of the epoch after the
duty's epoch: delay 32 for a duty in the final epoch slot and delay 63 for a duty
in the first epoch slot.

Deneb also awards the timely-target flag to every included attestation with a
correct target, regardless of inclusion delay. The canonical specification is
[`specs/deneb/beacon-chain.md`](https://github.com/ethereum/consensus-specs/blob/master/specs/deneb/beacon-chain.md#modified-process_attestation).

Stopping after a fixed 32 slots can convert a valid late inclusion into a false
miss for most slots in an epoch.

## Decision

Compute the end of collection from the duty's epoch. Scan canonical blocks
through the last slot of the following epoch, then emit `collection_completed`
only after that full window was queried successfully. Derive timely target from
canonical target correctness without imposing the removed 32-slot delay bound.

The fault injector and live collector use the same domain helper for the final
inclusion slot. The RCA engine advances from 0.6.0 to 0.7.0 because replaying an
existing late-inclusion timeline can produce different reward flags.

## Consequences

- Final verdict latency varies with duty position: approximately one to two
  epochs, plus bounded head-advance slack.
- Corpus generation takes longer but no longer labels valid Deneb inclusions as
  misses.
- Existing corpus fixtures must be regenerated from the devnet.
