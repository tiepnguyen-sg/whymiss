# Cause Taxonomy

**Taxonomy version: `1.0.0`** · Status: draft until Phase 3 exit

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
    TimelyHead   bool // correct head root AND inclusion delay == 1
}
```

`OutcomeDegraded` with `TimelyHead == false` is the single most common signal an
operator will see, and it is invisible in most existing tooling. Treat it as a
first-class outcome, not a footnote.

### 2.1 Duty scope

| Duty | v1 | Notes |
|---|---|---|
| Attester | ✅ | Primary focus |
| Proposer | ✅ | Missed proposals are rare but expensive |
| Aggregator | ❌ | Deferred — low reward impact |
| Sync committee | ❌ | Deferred — infrequent assignment |
| PTC (payload timeliness) | Phase 5 | Post-ePBS only, behind feature flag |

---

## 3. The timing model

Every attribution rests on one idea: **a duty has a latency budget, and a miss is a
budget overrun. Name the stage that overspent.**

### 3.1 Slot schedule (declarative, per `SlotSchedule` config — Phase 5 task 5.4)

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
| `thresholds.dominance` | `0.5` | Stage share required for dominance |
| `thresholds.network_deviation` | `750ms` | Local vs network-p50 block-arrival gap |
| `thresholds.engine_spike_multiplier` | `3.0` | × rolling p99 to count as a spike |
| `thresholds.peer_count_min` | `40` | Below this, p2p is considered degraded |
| `thresholds.subnet_peer_min` | `2` | Peers on the relevant attestation subnet |
| `thresholds.iowait_pct` | `20.0` | Host disk pressure |
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
| R-011 | clock trust | `unknown.insufficient_data` | I-9 — never time-attribute on a bad clock |
| R-100 | proposer absent | `network.proposer_missed` | Exonerates the operator immediately |
| R-110 | network-wide lateness | `network.late_block` | Needs baseline; skipped when disabled |
| R-200 | p2p health | `local.p2p_degraded` | Propagation precedes validation |
| R-300 | execution client | `local.el_slow` | Most common local cause in practice |
| R-310 | consensus client | `local.cl_slow` | Validation remainder after EL excluded |
| R-400 | VC reachability | `local.vc_disconnected` | Binary, unambiguous |
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

**Definition.** No block was proposed for this slot by anyone.

**Rule (R-100).** No `block_seen` observation exists for slot N, and the canonical
chain shows slot N as skipped.

**Required evidence.** Absence of `block_seen`; canonical chain confirmation that
slot N is empty.

**Confidence.** Always `high`. This is an observation, not an inference.

**Remediation.** None. The operator did nothing wrong. State this explicitly — an
exoneration is a valuable output.

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
`attestation_included` observation exists within the inclusion window.

**Required evidence.** Publish timestamp; absence of inclusion; head root voted;
canonical head at that slot; reorg observations within the window if any.

**Confidence.** `medium` by default — an aggregator dropping the attestation and a
local gossip failure are hard to separate. `high` when a reorg is observed in the
window.

**Remediation.**
- Verify inbound P2P ports are reachable, since poor connectivity reduces the chance
  an aggregator sees your attestation.
- If recurring, correlate with `local.p2p_degraded` frequency.

---

### `local.p2p_degraded`

**Definition.** Block propagation to this node was slow because peering was
insufficient.

**Rule (R-200).** Propagation stage is dominant, **and** at least one holds:
peer count < `thresholds.peer_count_min`; subnet peers < `thresholds.subnet_peer_min`;
peer count dropped more than 30% within the preceding 60s.

**Required evidence.** Propagation duration and its share of overspend; peer count at
slot start; subnet peer count; peer-count delta over the preceding minute.

**Confidence.** `high` when a peer-count metric corroborates; `medium` when only the
stage share indicates it.

**Remediation.**
- Confirm inbound TCP/UDP ports are open and forwarded (typically 30303 for the
  execution layer, 9000 for the consensus layer — verify against your own config).
- Raise the target peer count in your consensus client configuration.
- If behind CGNAT, expect persistently degraded inbound peering.

---

### `local.cl_slow`

**Definition.** The consensus client spent an unusual amount of time validating the
block, and the execution client is not responsible.

**Rule (R-310).** Validation stage is dominant, **and** Engine API duration accounts
for less than half of that stage.

**Required evidence.** Validation duration and share; Engine API total for the slot;
CL-side queue or processing metrics if exposed; comparison against the node's own
rolling p99.

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

**Required evidence.** `engine_newPayload` and `engine_forkchoiceUpdated` durations
for the slot; rolling p99 baseline; the computed multiple.

**Sub-cause selection** — evaluated in order; the first match wins, and if none
matches the verdict stays at `local.el_slow` with `confidence: medium`:

| Sub-cause | Signal |
|---|---|
| `local.el_slow.syncing` | EL reports not fully synced at slot time |
| `local.el_slow.snapshot` | EL snapshot-generation metric or log active in the window |
| `local.el_slow.pruning` | EL pruning metric or log active in the window |
| `local.el_slow.disk_saturation` | Host iowait > `thresholds.iowait_pct` during the window |

**Confidence.** `high` when a sub-cause matched; `medium` when only the Engine API
spike is present.

**Remediation** (sub-cause specific — generic advice is worthless here):
- `syncing` — wait for sync to complete; do not attest from an unsynced node.
- `snapshot` / `pruning` — schedule offline maintenance outside your duty-dense
  windows; consult your execution client's documented pruning procedure.
- `disk_saturation` — this box needs a faster NVMe drive. Consumer SATA SSDs are the
  most common cause of chronic attestation loss.

---

### `local.vc_disconnected`

**Definition.** The validator client could not reach the beacon node, so no
attestation was produced.

**Rule (R-400).** No `attestation_published` observation, **and** VC-to-BN
connectivity metrics indicate failure during the slot window.

**Required evidence.** Absence of publish; VC connection state; BN availability
during the window.

**Confidence.** `high`. This is directly observed, not inferred.

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

**Rule (R-600).** iowait > `thresholds.iowait_pct` sustained across the slot window,
and rules R-100 through R-500 did not match.

**Required evidence.** iowait percentage; average request latency; the device
involved.

**Confidence.** `medium`. Host pressure is correlational; the causal chain to the
missed duty is inferred rather than observed.

**Remediation.** Identify the process generating the I/O. If it is the execution
client, this is really `local.el_slow.disk_saturation` and the taxonomy should be
reviewed. If it is something else, move that workload off the staking box.

---

### `local.host.cpu_steal`

**Definition.** CPU steal time was elevated — the hypervisor withheld CPU from this
guest.

**Rule (R-600).** steal% > `thresholds.cpu_steal_pct` sustained across the window.

**Required evidence.** Steal percentage over the window; comparison against the
node's own baseline.

**Confidence.** `medium`.

**Remediation.** This is a noisy-neighbour problem and is not fixable in software.
Move to dedicated hardware or a provider with committed CPU. Sustained steal on a
shared VPS is a structural reason not to stake there.

---

### `local.host.memory_pressure`

**Definition.** Memory pressure or swap activity delayed processing.

**Rule (R-600).** PSI memory `avg10` > `thresholds.psi_mem_avg10`, or swap-in
activity observed during the window.

**Required evidence.** PSI value or swap rate; available memory; the largest resident
processes if collectable.

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
| `block_seen` | beaconapi (SSE) | Beacon block first received locally |
| `head_updated` | beaconapi (SSE) | Head advanced to this block after validation |
| `attestation_published` | beaconapi / VC | Attestation broadcast |
| `attestation_included` | beaconapi (REST) | Attestation observed on chain |
| `block_proposed` | beaconapi | This node's proposal was broadcast |
| `reorg` | beaconapi (SSE) | Chain reorganisation observed |
| `peer_count_sampled` | promscrape | Peer count at a point in time |
| `engine_call` | promscrape | Engine API call duration |
| `host_sampled` | hostmetrics | Host resource sample |
| `clock_sampled` | clock | NTP offset measurement |

### 8.1 Attribute keys

`Observation.Attrs` is bounded. Undocumented keys must fail validation.

| Key | Applies to | Example |
|---|---|---|
| `block_root` | `block_seen`, `head_updated` | `0xabc…` |
| `proposer_index` | `block_seen` | `123456` |
| `validator_index` | duty and attestation kinds | `987654` |
| `engine_method` | `engine_call` | `newPayload` |
| `duration_ms` | `engine_call` | `2780` |
| `peer_count` | `peer_count_sampled` | `62` |
| `subnet_id` | `peer_count_sampled` | `17` |
| `metric` | `host_sampled` | `iowait_pct` |
| `value` | `host_sampled`, `clock_sampled` | `23.4` |
| `inclusion_delay` | `attestation_included` | `1` |

---

## 9. Prometheus surface

Cardinality is bounded by the taxonomy, which is why the taxonomy is closed.

```
whymiss_duty_outcome_total{outcome, duty}                  counter
whymiss_verdict_total{cause, sub_cause, confidence}        counter
whymiss_stage_duration_seconds{stage}                      histogram
whymiss_clock_offset_seconds                               gauge
whymiss_baseline_available                                 gauge (0|1)
```

Worst-case `cause × sub_cause × confidence` cardinality is under 60 series. Any
change that makes cardinality unbounded is a defect.

**This is the feature operators actually adopt**: alert on
`whymiss_verdict_total{cause="local.el_slow"}` rising, rather than on a missed-
attestation counter that tells you nothing about what to fix.

---

## 10. Open questions

Resolve before taxonomy `1.0.0` is declared stable.

1. Should `network.late_block` distinguish a late proposer from a late builder? Doing
   so needs relay data and may not be worth the dependency.
2. Is `local.cl_slow` too coarse? It may need per-client sub-causes, which would
   violate the spirit of I-11 unless expressed as generic stage names.
3. How should multi-cause slots be represented? Current design forces a single
   dominant cause. A `contributing_causes` field may be needed — but only with
   evidence that operators want it.
4. Post-ePBS: does the PTC duty need its own outcome enum, or do reward flags extend
   cleanly?
