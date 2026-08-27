# ADR-0023: read connected peer count from the Beacon API, not client metrics

- Status: accepted
- Date: 2026-08-26

## Context

`local.p2p_degraded` (R-200) will only blame local peering when the connected
peer count is **below** `thresholds.peer_count_min`. That check is the rule's
corroboration: local-vs-network timing establishes that the delay was local, and
only the peer signal establishes that *insufficient peering* — rather than some
other local cause — explains it.

The count was scraped per client. For Prysm,
`connected_libp2p_peers{agent="…"}` summed across labels, which is correct. For
Lighthouse, the `libp2p_peers` gauge — and that gauge reports **0 on a genuinely
peered node**.

Measured on a freshly created two-node devnet, at the same instant:

```
lighthouse  /eth/v1/node/peer_count   -> {"connected":"1", ...}
lighthouse  libp2p_peers                 0
lighthouse  libp2p_peers_multi{...}      0   (every direction/transport)
lighthouse  libp2p_peers_per_client{...} 0   (every client)
lighthouse  block_mesh_peers_per_client{Client="Prysm"}  1
lighthouse  sync_peers_per_status{sync_status="Synced"}  1
prysm       connected_libp2p_peers{agent="lighthouse"}   1
prysm       /eth/v1/node/peer_count   -> {"connected":"1", ...}
```

So the two nodes were peered, both APIs said so, Prysm's own gauge said so, and
Lighthouse's gossip mesh said so — only the series whymiss read said otherwise.

The consequence is not cosmetic. A permanent zero makes `peerCount >= peer_count_min`
impossible, so R-200's corroboration passes unconditionally on every Lighthouse
deployment. The check that exists to prove peering was the problem proves nothing
there.

Nothing caught it because the recorded fixture the unit test replays,
`internal/source/promscrape/testdata/lighthouse_metrics.txt`, contains
`libp2p_peers 0` as well. The fixture is a real capture, as `AGENTS.md` requires
— it was captured from a node whose gauge was already wrong, so the test agreed
with the adapter and both were wrong together. A recorded fixture proves the
parser reads the file; it cannot prove the file means what the parser assumes.

## Decision

Read connected peer count from `GET /eth/v1/node/peer_count`, the standardised
Beacon API endpoint (github.com/ethereum/beacon-APIs), via
`beaconapi.Client.PeerCount`. Delete `SampleLighthousePeerCount`,
`SamplePrysmPeerCount`, and the `MetricsSampler.SamplePeerCount` dispatcher
rather than leaving the wrong adapter in place where a later refactor could wire
it back in.

`MetricCLPeerCount` — the normalised metric name R-200 reads — is unchanged. Only
where the number comes from changed.

Two allow-lists widen to permit it, both additively:

- `domain.MetricSample`: `SourceBeaconAPI` is valid for a `ComponentCL` sample.
- `domain.Observation`: `ObsPeerCountSampled` accepts `SourceBeaconAPI`
  alongside `SourcePromScrape`, so every already-recorded corpus scenario stays
  valid without regeneration.

## Consequences

- R-200's peer corroboration works on Lighthouse for the first time. Any past
  `local.p2p_degraded` verdict on a Lighthouse node was reached with that check
  effectively disabled.
- This fact now needs no client-specific code at all, which is the direction I-11
  points: one spec-defined endpoint replaces two parsers, and a third client
  needs neither.
- `tools/faultinjector` reads the same endpoint through the same call, so a
  generator can no longer bake a permanent zero into every record it writes.
- Peer sampling still runs only when `--cl-metrics-api` is set, because that flag
  is what enables the sampling loop today. That is now an accident rather than a
  requirement — the peer count no longer needs a metrics endpoint — and
  decoupling them would let an operator get peer corroboration with only a Beacon
  API. Left as a follow-up because it changes the config surface and its
  validation rules.
- **Wider lesson, recorded because it has now cost three separate bugs:** a
  client's Prometheus surface is not a contract. Within one night it produced a
  Prysm arrival gauge frozen for hours (ADR-0022), gossip counters frozen while
  blocks were importing, and this. Where the Beacon API exposes the same fact, it
  is the better source: it is specified, versioned, and identical across clients.
  Metrics remain necessary for facts the API does not expose — block arrival
  timing, Engine call latency — and those keep their adapters.
