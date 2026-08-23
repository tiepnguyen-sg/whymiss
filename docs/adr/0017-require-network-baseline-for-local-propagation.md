# ADR-0017: Require a network baseline for local propagation attribution

- Status: accepted
- Date: 2026-08-23

## Context

A late local block and a network-wide late block have the same local stage shape. A
low peer count can coexist with a late proposer and does not prove that local peering
caused the delay. R-200 previously blamed `local.p2p_degraded` without any network
comparison, contradicting R-110's documented requirement to distinguish the two.

## Decision

R-110 returns `unknown.insufficient_data` whenever local propagation exhausts the
attestation budget and no network baseline exists. R-200 may run only when the
network p50 for the same slot was timely. Peer metrics then corroborate the local
attribution; they never substitute for the network comparison.

Persist the comparison in the additive `network_baseline_sampled` observation so
live storage and corpus replay reconstruct the same `NetworkBaseline` without hidden
I/O or manifest-only data.

For the single-node live adapter, the baseline Beacon node must match the watched
node's genesis and slot duration. The block-arrival gauge and `beacon_head_slot` must
come from the same Prometheus response and the metric slot must equal the watched
slot; otherwise no baseline observation is recorded.

This narrows an existing cause and is therefore part of the taxonomy 2.0.0 re-scope.
The RCA engine advances from 0.5.0 to 0.6.0. P2P corpus scenarios must record a real
independent observer baseline before they can label a local cause.

## Consequences

- A coincidentally low peer count cannot produce a false local blame verdict.
- A timely independent baseline without a peer-count sample establishes only an
  unspecified local propagation delay; R-200 does not guess that peering caused it.
- Deployments without the opt-in baseline receive an explicit unknown result for
  propagation misses.
- Network-baseline collection becomes a release requirement for useful R-110/R-200
  attribution.
