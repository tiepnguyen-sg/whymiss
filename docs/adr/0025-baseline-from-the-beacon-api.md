# ADR-0025: the network baseline can be read from the Beacon API

- Status: accepted
- Date: 2026-08-26

## Context

The product's headline question is *"was it me, or was it the network?"*. Only two
rules answer it — R-110 (`network.late_block`) and R-200 (`local.p2p_degraded`) —
and both require `tl.Network`, the independent baseline. Without it they decline
and the question falls through to `unknown.insufficient_data` (ADR-0017).

Until now the baseline could only be produced by scraping a second consensus
node's **Prometheus endpoint**: `--baseline-beacon-api` was rejected unless
`--baseline-metrics-api` was set with it. So answering the product's central
question required the operator to *run and scrape a second beacon node*, not
merely to reach one. Most operators running a single validator box have neither.

The metrics path also carries the problems ADR-0022 documents: the arrival gauge
is a latest-value series with no slot label, it goes stale on a node whose gossip
degrades, and it needs a per-client parser. That last point cost a real bug —
Lighthouse's peer gauge reads 0 on a peered node (ADR-0023) — and the wider lesson
recorded there applies here too: where the Beacon API exposes the same fact, it is
the better source, because it is specified, versioned, and identical across
clients.

The Beacon API does expose this fact. `Client.BlockSeen` already polls
`/eth/v1/beacon/headers/{slot}` for the watched node and timestamps the first
appearance of the block. Pointed at the baseline node it measures exactly what a
baseline needs: when that independent node saw this slot's block.

## Decision

`--baseline-metrics-api` becomes optional. With it, the baseline is scraped as
before. Without it, `runNetworkBaseline` polls the baseline node's own
`/eth/v1/beacon/headers/{slot}` and derives propagation from the slot start,
recording the observation with `Source: beaconapi`.

Client detection is skipped on that path. It exists only to choose a Prometheus
adapter, and every client serves the same `/eth/v1/beacon/headers/{slot}` — so
requiring a *recognised* client would reject a perfectly usable baseline for no
reason.

`domain.Observation`'s allow-list for `ObsNetworkBaselineSampled` widens to accept
`SourceBeaconAPI` alongside `SourceXatu` and `SourcePromScrape`. Additive: every
recorded corpus scenario stays valid.

## The trigger matters as much as the source

The collector is driven by the **slot clock**, not by the watched node's
`head_updated`, and that is load-bearing rather than incidental.

`BlockSeen` returns as soon as the baseline node has the block. Polling only once
our own node reports a head therefore starts the measurement late by exactly
however late our node was, making the result an upper bound shaped by our own
latency rather than an arrival time. A watched node seeing the block at +6s would
have produced a baseline reading of ~6.1s for a peer that actually had it at
+0.1s — and R-110 would then see local and network agreeing above the deadline
and report `network.late_block`, exonerating a local fault.

That is a false attribution, the failure I-8 exists to prevent, and it would have
been invisible: the verdict looks reasonable and the numbers look consistent.
Polling from the slot boundary is what makes the measurement mean what the rules
assume it means.

## Consequences

- The baseline now needs an API the operator can **reach** — a friend's node, a
  provider's endpoint, a second box — rather than one they run and scrape. That is
  the difference between the product's core question being answerable by most
  operators and by almost none.
- **The measurement is coarser, and this is the trade to weigh.** `BlockSeen`
  polls at 500 ms, so an API-derived arrival is quantised to that, where the
  Prometheus gauge is millisecond-precise. `thresholds.network_deviation` defaults
  to 750 ms, so the quantisation is a meaningful fraction of the window R-110 and
  R-200 compare local against network within.

  The error is safe in one direction only, which is why it is acceptable:
  coarseness can push the deviation outside the threshold and make those rules
  **decline**, never make them attribute wrongly. I-8 prefers exactly that
  failure. A single-node baseline is also already capped at `medium` confidence
  regardless of how it was measured, so no verdict gains unearned certainty here.
- An operator who wants the precise measurement keeps it by setting
  `--baseline-metrics-api`, and should when the second node is theirs.
- Polling a third party's node is bounded by the client's own
  `MinRequestInterval` rate limit (I-5); it is the same polling this collector
  already does against the watched node.
- Not addressed: a genuine multi-node percentile. Both paths produce a one-sample
  baseline (`sample_count: 1`, p50 == p90), which the rules already treat as weak
  evidence. Real percentiles need the public Xatu dataset, which is Phase 5 and
  keeps this same observation shape so the adapter can replace the value without
  touching a rule.
