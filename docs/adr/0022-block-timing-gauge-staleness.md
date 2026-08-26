# ADR-0022: a block-timing gauge must place arrival before its own sample

- Status: accepted
- Date: 2026-08-26

## Context

Neither supported consensus client publishes block arrival delay as a
slot-labelled series. Lighthouse exposes `beacon_block_delay_observed_slot_start`
and Prysm `block_arrival_latency_milliseconds_gauge`, both latest-value gauges.
`source.SampleBlockTimingForSlot` establishes which slot a sample belongs to by
polling until `beacon_head_slot` equals the requested slot — a **different
series** from the value it returns. Nothing checks that the delay gauge itself
moved.

A node can advance its head without recording an arrival on the path that updates
the gauge. Measured on this project's devnet: both clients' gossip-block counters
sat frozen (`beacon_processor_gossip_block_imported_total` at 5021,
`block_arrival_latency_milliseconds_count` at 5131) for hours while
`beacon_head_slot` advanced normally and `beacon_block_processing_requests_total`
kept incrementing — the nodes were importing blocks off the gossip path the
metric tracks. Lighthouse's gauge kept updating regardless; Prysm's did not.

The consequence, from a 15-minute live run: **21 consecutive
`network_baseline_sampled` observations carrying the identical propagation value**
(2233 ms, spanning slots 12967 to 13038), each written as that slot's
measurement. R-110 and R-200 compare local propagation against that baseline, so
they were comparing against a constant — a verdict input with no evidence behind
it (I-7) and a fabricated number where I-8 asks for `unknown`.

`blockSeenFromTiming` never had this problem. It has always rejected a sample
whose implied arrival falls after the head observation that triggered it. The
baseline path, added later, checked only that propagation was non-negative. That
asymmetry is the whole defect: the watched node's timing stayed correct while the
baseline silently degraded.

## Decision

Apply the same bound on the baseline path. A block's arrival always precedes the
read that reports it, so reject any sample where

```
slotStart(slot) + propagation > sampledAt
```

Such a gauge cannot be describing the slot that was asked for. Rejecting writes
no observation, `tl.Network` stays nil, and R-110 and R-200 decline — which is
what they are built to do when the baseline is unavailable (ADR-0017).

The check is client-agnostic, stateless, and sound in one direction: it never
rejects a genuine reading, because a genuine reading is by construction a delay
that already elapsed before the scrape. It is deliberately not a complete
staleness test.

## Consequences

- Against the frozen Prysm gauge above, every one of those 21 samples is
  rejected: a 2233 ms delay read 702 ms into the slot is impossible.
- A genuinely late block is still accepted. The node imports it, then the gauge
  is read afterwards, so arrival still precedes the sample.
- **Residual gap, stated rather than hidden:** a stale value *smaller* than the
  elapsed time still passes. A gauge frozen at 80 ms read 1.5 s into the slot is
  indistinguishable from a fresh 80 ms reading by this check alone. Closing that
  needs a freshness proof tied to the value's own metric family, and only Prysm
  offers one — `block_arrival_latency_milliseconds_count` increments exactly when
  the gauge is set, so requiring it to advance between accepted samples would be
  exact. Lighthouse has no counter attached to its gauge, and the obvious
  candidates are wrong: `beacon_processor_gossip_block_imported_total` was frozen
  in a situation where the gauge was live, so requiring it would reject valid
  Lighthouse samples. Per-endpoint counter state was left out of this change to
  keep it sound for both clients rather than exact for one.
- The longer-term direction is to stop depending on these gauges for the baseline
  at all: `--baseline-beacon-api` is already configured, and polling
  `/eth/v1/beacon/headers/{slot}` on the baseline node the way `BlockSeen` does
  for the watched node would give an arrival time that is provably about that
  slot, client-agnostic, and free of any metrics endpoint — at ~500 ms
  resolution instead of milliseconds. That would also let `--baseline-beacon-api`
  work without `--baseline-metrics-api`, which is currently a hard requirement
  and a real barrier: an operator must run a second node they scrape, not merely
  reach a second API. Not taken here because it changes what a baseline
  observation measures, and the resolution trade needs to be judged against
  `thresholds.network_deviation`.
- Prysm block-timing support is **not** withdrawn. The metric name is correct and
  the gauge does update when the node receives blocks over gossip; what was
  observed is a node whose gossip had degraded, which is a node condition rather
  than an adapter bug. Withdrawing support would remove working functionality for
  healthy Prysm nodes.
