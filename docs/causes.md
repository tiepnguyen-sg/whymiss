# Cause Taxonomy

**Taxonomy version: `2.0.0`** · Status: draft until release gates pass

This document is a contract, not documentation. Every `Verdict` embeds
`taxonomy_version`, and consumers may depend on cause IDs remaining stable.

---

## 1. Governance

| Change | Version bump | Requires |
|---|---|---|
| Add a cause ID | minor | ADR |
| Add evidence or remediation to an existing cause | patch | — |
| Change what a cause ID *means* | **major** | ADR + human approval |
| Rename a cause ID | **major** | ADR + human approval + deprecation alias for one minor cycle |
| Change rule ordering | minor | ADR |

A cause ID never silently changes meaning. Operators build alerts on these strings.

---

## 2. Outcome model

> **Delta from `BUILD_PROMPT.md` §6.** The `Outcome` enum defined there
> (`ok | late | missed`) is insufficient — it cannot express partial reward loss,
> which is the most common real-world case. Apply this refinement **before**
> freezing the domain model at the end of Phase 1.

```go
type Outcome string

const (
    OutcomeNoDuty   Outcome = "no_duty"   // nothing was owed this slot
    OutcomeOK       Outcome = "ok"        // duty fulfilled, all reward flags earned
    OutcomeDegraded Outcome = "degraded"  // included on chain, one or more flags lost
    OutcomeMissed   Outcome = "missed"    // never included on chain
)

// RewardFlags mirrors the consensus-spec participation flags.
type RewardFlags struct {
    TimelySource bool // correct source checkpoint, inclusion delay within bound
    TimelyTarget bool // correct target checkpoint
    TimelyHead   bool // correct target + head roots AND inclusion delay == 1
}
```

`OutcomeDegraded` with `TimelyHead == false` is the single most common signal an
operator will see, and it is invisible in most existing tooling. Treat it as a
first-class outcome, not a footnote.

### 2.1 Duty scope

| Duty | v1 | Notes |
|---|---|---|
| Attester | ✅ | Primary focus |
| Proposer | Partial | Canonical proposer absence is diagnosed; automatic local proposer-duty attribution is deferred |
| Aggregator | ❌ | Deferred — low reward impact |
| Sync committee | ❌ | Deferred — infrequent assignment |
| PTC (payload timeliness) | Phase 5 | Post-ePBS only, behind feature flag |

---

## 3. The timing model

Every attribution rests on one idea: **a duty has a latency budget, and a miss is a
budget overrun. Name the stage that overspent.**

### 3.1 Slot schedule (declarative, per `SlotSchedule` config)

Pre-ePBS mainnet:

```yaml
seconds_per_slot:      12s
attestation_deadline:   4s   # SECONDS_PER_SLOT / INTERVALS_PER_SLOT
aggregation_deadline:   8s
```

Hard-coding these constants anywhere outside the schedule config is a bug.
Glamsterdam changes them; the code must not care.

### 3.2 Stage decomposition for an attestation at slot N

```
T0 ─────────► T_block_seen ─────────► T_head ─────────► T_published ──┤ D = T0 + 4s
   propagation            validation           signing
        │                      │                    │
        └─ network / p2p       └─ CL + EL           └─ VC
```

| Stage | Measured as | Attributable to |
|---|---|---|
| Propagation | `T_block_seen − T0` | `network.late_block` or `local.p2p_degraded` |
| Validation | `T_head − T_block_seen` | `local.cl_slow` or `local.el_slow` |
| Signing | `T_published − T_head` | `local.vc_slow` |

Validation is decomposed further using Engine API timings: if
`engine_newPayload + engine_forkchoiceUpdated` accounts for the majority of the
validation stage, the cause is `local.el_slow`; otherwise `local.cl_slow`.

### 3.3 Overspend attribution

```
overspend      = T_published − D           (or D − T0 if never published)
stage_share(s) = duration(s) / Σ duration
dominant       = argmax stage_share
```

A stage is **dominant** when `stage_share(dominant) ≥ dominance_threshold`
(default `0.5`).

---

## 4. Confidence derivation

Confidence is computed, never assigned by feel. Exactly three inputs:

| Input | Meaning |
|---|---|
| **D** — dominance | Does one stage account for ≥ `dominance_threshold` of the overspend? |
| **C** — corroboration | Is there an independent metric or host signal supporting the attribution? |
| **K** — clock trust | Is measured NTP offset within `clock_offset_max`? |

```
K == false                    → verdict is forced to unknown.insufficient_data   (I-9)
D && C && K                   → ConfidenceHigh
D && !C && K                  → ConfidenceMedium
!D && K                       → ConfidenceLow
```

**Hard gate (Phase 3 DoD):** no `ConfidenceHigh` verdict may be wrong on any corpus
scenario. A single violation blocks release. High confidence is a promise, and an
operator who is burned once by a confident wrong answer never trusts the tool again.

---

## 5. Configurable thresholds

Every threshold is configurable, documented, and has a safe default. Rules must
never contain magic numbers.

| Key | Default | Meaning |
|---|---|---|
| `thresholds.clock_offset_max` | `100ms` | Above this, timing rules are suppressed |
| `thresholds.clock_sample_max_age` | `2m` | Older samples cannot establish clock trust |
| `thresholds.dominance` | `0.5` | Stage share required for dominance |
| `thresholds.network_deviation` | `750ms` | Local vs network-p50 block-arrival gap |
| `thresholds.engine_spike_multiplier` | `3.0` | × rolling p99 to count as a spike |
| `thresholds.peer_count_min` | `40` | Below this, p2p is considered degraded |
| `thresholds.iowait_pct` | `20.0` | Linux PSI I/O `some avg10` (legacy key name) |
| `thresholds.cpu_steal_pct` | `5.0` | Host CPU contention |
| `thresholds.psi_mem_avg10` | `10.0` | Memory pressure stall index |

---

## 6. Rule ordering

First match wins. Order is declared in `internal/rca/rules/order.go` with a comment
justifying each position. Changing the order requires an ADR.

| # | Rule | Cause | Why here |
|---|---|---|---|
| R-001 | duty guard | `no_duty` | Nothing owed — exit before any analysis |
| R-010 | data completeness | `unknown.insufficient_data` | Cannot reason on missing data |
| R-011 | clock trust | `local.host.clock_drift` or `unknown.insufficient_data` | Direct excessive drift is named; missing/stale clock proof remains unknown |
| R-100 | proposer absent | `network.proposer_missed` | Exonerates the operator immediately |
| R-110 | network-wide lateness | `network.late_block` | Needs baseline; skipped when disabled |
| R-200 | p2p health | `local.p2p_degraded` | Propagation precedes validation |
| R-300 | execution client | `local.el_slow` | Most common local cause in practice |
| R-310 | consensus client | `local.cl_slow` | Validation remainder after EL excluded |
| R-400 | VC reachability | `local.vc_disconnected` | Requires a timely head |
| R-410 | VC timing | `local.vc_slow` | Last stage in the chain |
| R-500 | inclusion | `network.inclusion_failure` | Published on time yet absent on chain |
| R-600 | host fallback | `local.host.*` | Terminal only when no layer above matched |
| R-999 | catch-all | `unknown.no_rule_matched` | Report as a taxonomy gap, never guess |

**Host signals have a dual role.** They serve as corroboration (input **C**) inside
R-300 and R-310, and only become a terminal cause at R-600 when no higher-layer rule
matched. This avoids reporting `local.host.disk_io` when the actionable fact is
`local.el_slow.pruning`.

**R-999 reaching an `ok` outcome is a clean pass, not a taxonomy gap.** The
catch-all's `unknown.no_rule_matched` reading in the table above holds for
`degraded` and `missed` — where something demonstrably went wrong and no rule
explained it. When the outcome is `ok`, nothing went wrong for a rule to attribute,
so the verdict carries **no cause at all** (the same "nothing to attribute" shape
`no_duty` uses) at `high` confidence, rather than telling an operator their healthy
validator is a project bug. Note that a rule *can* still legitimately match on an
`ok` duty — no rule inspects `Outcome`, and a validator client that was measurably
slow yet beat the deadline should still say so (`test/corpus/vc-slow-cpu`). Only the
catch-all is reinterpreted this way; a real cause on an `ok` duty is preserved.

---

## 7. Cause reference

Each entry: definition · rule · required evidence · confidence · remediation.

---

### `network.proposer_missed`

**Definition.** The canonical chain contains no block for this slot.

**Rule (R-100).** A `block_skipped` observation exists for slot N, and no
`block_seen`, `head_updated`, or `block_proposed` observation contradicts it. R-100
does not apply to the operator's own proposer duty because this taxonomy cannot yet
distinguish a local proposal failure from an upstream network event.

**Required evidence.** After the collection window closes, the configured Beacon API
must report all of: the node is fully synced, execution is online and non-optimistic,
its head has advanced past slot N, and a second canonical-header lookup for N returns
404.
Those facts are materialised as `block_skipped`; absence of `block_seen` alone is not
evidence and never triggers this rule.

**Confidence.** `high`: the rule only fires on the positive canonical-chain check
above. If that check cannot be completed, attribution falls through to an unknown
cause rather than lowering confidence on an absence-based guess.

**Remediation.** None for an attester. The operator's attestation path did not cause
the canonical skip; state this explicitly because exoneration is a valuable output.

---

### `network.late_block`

**Definition.** The block arrived late for the network as a whole, not just locally.

**Rule (R-110).** `T_block_seen − T0` exceeds the attestation deadline, **and**
network baseline p50 for this slot also exceeds it, **and**
`|local − network_p50| < thresholds.network_deviation`.

**Requires network baseline.** When baseline is disabled or unavailable, this rule
is skipped and analysis falls through to `unknown.insufficient_data` with the note
*"enable network baseline to distinguish network lateness from local propagation."*

> This is the single most valuable rule in the taxonomy and the reason Phase 5
> exists. Without it, every propagation problem is ambiguous.

**Required evidence.** Local `block_seen` offset; network p50 and p90 for the same
slot; the computed deviation.

**Confidence.** `high` when deviation is small and baseline sample count is adequate;
`medium` when the baseline sample is thin.

**Remediation.** None for the operator. Optionally identify the proposer for
community reporting.

---

### `network.inclusion_failure`

**Definition.** The attestation was published before the deadline but never appeared
on chain.

**Rule (R-500).** `attestation_published` exists with offset < deadline, and no
`attestation_included` observation exists by the final valid Deneb inclusion slot:
the last slot of the epoch following the duty's target epoch.

**Required evidence.** Publish timestamp; absence of inclusion; head root voted;
canonical head at that slot; reorg observations within the window if any.

**Confidence.** `medium` — an aggregator dropping the attestation and a local gossip
failure are hard to separate. An unlinked reorg in the same window is contextual
evidence, not proof that this specific attestation was removed, so it never raises
confidence by itself.

**Remediation.**
- Verify inbound P2P ports are reachable, since poor connectivity reduces the chance
  an aggregator sees your attestation.
- If recurring, correlate with `local.p2p_degraded` frequency.

---

### `local.p2p_degraded`

**Definition.** Block propagation to this node was slow because peering was
insufficient.

**Rule (R-200).** Propagation exceeds the attestation deadline and, when another
stage boundary is available, is the dominant known stage, **and** the network
baseline p50 for the slot was within the deadline. When propagation is the only
measured stage, consuming the full attestation budget is the absolute dominance
test; no synthetic 100% share is claimed.
The baseline is mandatory: without it R-110 returns `unknown.insufficient_data`
because local and network-wide lateness cannot be distinguished. A peer-count
sample is also mandatory and must be below `thresholds.peer_count_min`. Without
that corroboration, the engine cannot distinguish insufficient peering from another
local propagation cause and R-200 does not match.

**Required evidence.** Local propagation duration and its share when another stage
is measurable; a timely network p50 for the same slot; peer count at slot start.

**Confidence.** `high` when an adequate network sample and peer-count metric both
corroborate; `medium` when the network sample is thin.

**Remediation.**
- Confirm inbound TCP/UDP ports are open and forwarded (typically 30303 for the
  execution layer, 9000 for the consensus layer — verify against your own config).
- Raise the target peer count in your consensus client configuration.
- If behind CGNAT, expect persistently degraded inbound peering.

---

### `local.cl_slow`

**Definition.** The consensus client spent an unusual amount of time validating the
block, and the execution client is not responsible.

**Rule (R-310).** The canonical head update is later than the attestation deadline,
validation is the dominant stage, **and** Engine API duration accounts for less than
half of that stage.

**Required evidence.** Validation duration and share; per-method Engine API call
counts and total durations from an exact consecutive canonical-head window. This
build has no portable CL processing baseline or queue metric, so it does not claim
one and caps the verdict at `medium`.

**Confidence.** `medium` by default — CL internals are poorly instrumented across
clients. `high` only when a client-specific metric directly corroborates.

**Remediation.**
- Check the consensus client version against the latest release; several historical
  incidents were fixed in a point release.
- Review CL logs for the slot window.
- Correlate with `local.host.cpu_steal` and `local.host.memory_pressure`.

---

### `local.el_slow`

**Definition.** The execution client responded slowly to Engine API calls, consuming
the validation budget.

**Rule (R-300).** Validation stage is dominant, **and** Engine API duration accounts
for at least half of that stage, **and** the Engine API duration exceeds
`thresholds.engine_spike_multiplier` × the node's rolling p99.

**Required evidence.** `newPayload` and `forkchoiceUpdated` call counts and total
durations from the exact canonical-head window; rolling p99 baseline; the computed
multiple.

**Sub-cause selection** — evaluated in order; the first match wins, and if none
matches the verdict stays at `local.el_slow` with `confidence: medium`:

| Sub-cause | Signal |
|---|---|
| `local.el_slow.syncing` | EL reports not fully synced at slot time |
| `local.el_slow.snapshot` | EL snapshot-generation metric or log active in the window |
| `local.el_slow.pruning` | EL pruning metric or log active in the window |
| `local.el_slow.disk_saturation` | EL-specific device/request telemetry proves saturation in the Engine window |

**Confidence.** `high` only when direct EL-specific telemetry establishes a
sub-cause; `medium` when only the Engine API spike is present. This build records
host-wide PSI as context but does not use it to select an EL sub-cause because PSI
cannot identify the process or device responsible. The current Lighthouse/Prysm
collectors do not emit an EL-specific sub-cause signal, so this build emits the
generic `local.el_slow` at `medium`; the sub-cause IDs remain reserved public
taxonomy entries rather than inferred labels.

**Remediation** (sub-cause specific — generic advice is worthless here):
- `syncing` — wait for sync to complete; do not attest from an unsynced node.
- `snapshot` / `pruning` — schedule offline maintenance outside your duty-dense
  windows; consult your execution client's documented pruning procedure.
- `disk_saturation` — identify the saturated device and competing process before
  changing storage; host-wide PSI alone is not enough.

---

### `local.vc_disconnected`

**Definition.** The validator client could not reach the beacon node, so no
attestation was produced.

**Rule (R-400).** A block was seen and the canonical head updated before the
attestation deadline, but neither `attestation_published` nor
`attestation_included` was observed. This is an inferred branch until direct VC
connection-state collection is implemented.

**Required evidence.** Timely `block_seen` and `head_updated`; absence of both
publish and inclusion.

**Confidence.** `medium`. `high` is reserved for a future direct VC-to-BN
connection-failure signal.

**Remediation.**
- Check the validator client process is running and its beacon-node endpoint is
  correct.
- If using multiple beacon nodes, verify fallback ordering behaved as intended.
- This condition warrants an immediate alert, not a post-mortem — it is ongoing loss.

---

### `local.vc_slow`

**Definition.** The validator client received a valid head in time but published the
attestation after the deadline.

**Rule (R-410).** Signing stage is dominant; `T_head` occurred before the deadline;
`T_published` occurred after it.

**Required evidence.** Head timestamp; publish timestamp; signing duration; remote
signer latency where a remote signer is configured.

**Confidence.** `high` when a remote signer is in use and its latency corroborates;
`medium` for local keystores.

**Remediation.**
- If using a remote signer (Web3Signer or similar), measure its latency — it is
  frequently the culprit and rarely monitored.
- Check for CPU contention on the validator client host.
- Confirm VC and BN clocks agree.

---

### `local.host.disk_io`

**Definition.** Host disk I/O pressure was the dominant explanation and no
higher-layer cause matched.

**Rule (R-600).** Linux PSI I/O `some avg10` > `thresholds.iowait_pct` in the
latest sample, and rules R-100 through R-500 did not match. The configuration key
retains its legacy `iowait_pct` name, but this signal is not `/proc/stat` CPU iowait.

**Required evidence.** Host-wide PSI I/O `some avg10` percentage.

**Confidence.** `medium`. Host pressure is correlational; the causal chain to the
missed duty is inferred rather than observed.

**Remediation.** Identify the process generating the I/O. If it is the execution
client, this is really `local.el_slow.disk_saturation` and the taxonomy should be
reviewed. If it is something else, move that workload off the staking box.

---

### `local.host.cpu_steal`

**Definition.** CPU steal time was elevated — the hypervisor withheld CPU from this
guest.

**Rule (R-600).** steal% > `thresholds.cpu_steal_pct` over the latest interval
between two `/proc/stat` samples.

**Required evidence.** Steal percentage over that sampling interval.

**Confidence.** `medium`.

**Remediation.** This is a noisy-neighbour problem and is not fixable in software.
Move to dedicated hardware or a provider with committed CPU. Sustained steal on a
shared VPS is a structural reason not to stake there.

---

### `local.host.memory_pressure`

**Definition.** Linux PSI memory pressure was elevated while the duty failed.

**Rule (R-600).** PSI memory `some avg10` > `thresholds.psi_mem_avg10` in the
latest sample.

**Required evidence.** Host-wide PSI memory `some avg10` percentage.

**Confidence.** `medium`.

**Remediation.** Add RAM, or reduce the client cache settings. Never run a validator
on a box that swaps.

---

### `local.host.clock_drift`

**Definition.** The system clock was offset far enough to distort duty timing.

**Rule (R-011).** Measured NTP offset > `thresholds.clock_offset_max`.

> This rule fires **early** (position R-011) and suppresses all timing-derived
> attribution, because a bad clock invalidates every other measurement (I-9). It is
> reported as `local.host.clock_drift` with all downstream reasoning marked
> unavailable.

**Required evidence.** Measured offset; NTP source; time of last successful sync.

**Confidence.** `high`. Directly measured.

**Remediation.** Install and enable `chrony` or `systemd-timesyncd`. Verify with
`chronyc tracking`. A drifting clock silently destroys attestation effectiveness and
is one of the most under-diagnosed staking problems.

---

### `unknown.insufficient_data`

**Definition.** Required observations were unavailable, so no honest attribution is
possible.

**Rule (R-010, R-011, or fall-through from R-110).** One or more required
observations are absent, or clock trust failed, or a rule required the network
baseline and it was disabled.

**Required evidence.** Explicitly list which observations were missing and why they
mattered. This is the most important evidence block in the taxonomy — it tells the
operator how to make the *next* miss diagnosable.

**Confidence.** Not applicable; the field is set to `low`.

**Remediation.** Name the concrete gap: enable the network baseline, expose the
missing metrics endpoint, install NTP, and so on.

---

### `unknown.no_rule_matched`

**Definition.** Data was complete and trustworthy, yet no rule matched.

**Rule (R-999).** Terminal fall-through.

**Required evidence.** The full stage decomposition with all durations and shares, so
the timeline can be attached to a bug report unmodified.

**Confidence.** `low`.

**Remediation.** This is a **taxonomy gap and a project bug**, not an operator
problem. The report must say so and link to the issue tracker. Every occurrence
should become a corpus scenario.

> Track the rate of `unknown.no_rule_matched` as a project health metric. It should
> fall over time. If it rises after a client release, the client changed something.

---

## 8. Observation vocabulary

Closed set. Adding a kind is a taxonomy change (minor bump + ADR).

| Kind | Source | Meaning |
|---|---|---|
| `slot_start` | derived | Wall-clock start of the slot |
| `duty_assigned` | beaconapi | Attester or proposer duty known for this slot |
| `block_seen` | beaconapi (REST) / promscrape | Beacon block observed locally |
| `block_skipped` | beaconapi (REST) | Fully synced node, already past the slot, confirmed no canonical block |
| `head_updated` | beaconapi (SSE) | Head advanced to this block after validation |
| `attestation_published` | beaconapi / VC | Attestation broadcast |
| `attestation_included` | beaconapi (REST) | Attestation observed on chain |
| `block_proposed` | beaconapi | This node's proposal was broadcast |
| `reorg` | beaconapi (SSE) | Chain reorganisation observed |
| `peer_count_sampled` | promscrape | Peer count at a point in time |
| `engine_call` | promscrape | Per-method Engine API call count and total duration in an exact canonical-head window |
| `host_sampled` | hostmetrics | Host resource sample |
| `clock_sampled` | clock | NTP offset measurement |
| `collection_completed` | derived | Every required query completed and the final valid Deneb inclusion slot ended |
| `network_baseline_sampled` | xatu / promscrape | Network block-arrival p50, p90, and sample count for one slot |

### 8.1 Attribute keys

`Observation.Attrs` is bounded. Undocumented keys must fail validation.

| Key | Applies to | Example |
|---|---|---|
| `block_root` | block, head, and attestation observations | `0xabc…` |
| `proposer_index` | `block_seen` | `123456` |
| `validator_index` | duty, attestation, proposal, and collection-completion kinds | `987654` |
| `engine_method` | `engine_call` | `newPayload` |
| `duration_ms` | `engine_call` | `2780` |
| `peer_count` | `peer_count_sampled` | `62` |
| `metric` | `host_sampled` | `host_iowait_pct` |
| `value` | `host_sampled`, `clock_sampled` | `23.4` |
| `inclusion_delay` | `attestation_included` | `1` |
| `head_correct` | `attestation_included` | `true` |
| `target_correct` | `attestation_included` | `true` |
| `block_arrival_p50_ms` | `network_baseline_sampled` | `850.5` |
| `block_arrival_p90_ms` | `network_baseline_sampled` | `1300` |
| `sample_count` | `engine_call`, `network_baseline_sampled` | `2` |

---

## 9. Prometheus surface

Cardinality is bounded by the taxonomy, which is why the taxonomy is closed.

```
whymiss_duty_verdicts_total{cause,outcome} counter
```

`cause` is the reported sub-cause when present, otherwise the cause, or `none` when
there is nothing to attribute. The closed cause set plus four outcomes bounds the
surface at 76 possible series. Any unbounded label is a defect.

**This is the feature operators actually adopt**: alert on
`whymiss_duty_verdicts_total{cause="local.el_slow"}` rising, rather than on a missed-
attestation counter that tells you nothing about what to fix.

---

## 10. Open questions

Resolve before the taxonomy is declared stable.

1. Should `network.late_block` distinguish a late proposer from a late builder? Doing
   so needs relay data and may not be worth the dependency.
2. Is `local.cl_slow` too coarse? It may need per-client sub-causes, which would
   violate the spirit of I-11 unless expressed as generic stage names.
3. How should multi-cause slots be represented? Current design forces a single
   dominant cause. A `contributing_causes` field may be needed — but only with
   evidence that operators want it.
4. Post-ePBS: does the PTC duty need its own outcome enum, or do reward flags extend
   cleanly?
