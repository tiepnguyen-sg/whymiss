# ADR-0020: Remove the unused subnet-peer threshold

- Status: accepted
- Date: 2026-08-23

## Context

The public configuration accepted `thresholds.subnet_peer_min`, and the observation
vocabulary accepted `subnet_id`, but no collector emitted a subnet-specific peer
count and no RCA rule read either value. Operators could change the threshold with
no effect. Applying it to R-200 would also be unsound: R-200 explains delayed block
propagation on the block gossip topic, while attestation-subnet peers describe a
different publication path.

## Decision

Remove the no-op threshold, environment variable, and attribute before release.
R-200 continues to require the measured total connected-peer count plus an
independent same-slot network baseline. A future attestation-gossip rule may add a
client-normalized subnet signal through the normal taxonomy and ADR process.

## Consequences

- Every accepted operator threshold now affects implemented behavior.
- Existing prerelease configurations containing `subnet_peer_min` fail closed as an
  unknown YAML key instead of appearing to work.
- No verdict changes because the removed values were never read.
