# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). The
version stays at `v0.x` until the API is stable (BUILD_PROMPT §3).

I-16 makes an entry here mandatory: any commit touching `internal/` or `cmd/` must
update this file in the same commit. CI and the pre-commit hook both enforce it.

## [Unreleased]

### Added

- Repository scaffold: canonical directory layout per BUILD_PROMPT §4, Apache-2.0
  licence, and contributor governance documents.
- `go.mod` at Go 1.23 with zero dependencies. Additions require an ADR (I-14).
- `.golangci.yml` with `depguard` rules enforcing the I-6 purity boundaries for
  `internal/domain` and `internal/rca`.
- ADR-0001 through ADR-0005: language/runtime, storage, pure-engine architecture,
  dependency policy, and cause-taxonomy governance.
- `internal/domain`: the frozen domain model (task 1.3) — `Observation`,
  `Timeline`, `Verdict`, and their supporting types, per BUILD_PROMPT §6 as refined
  by the `Outcome`/`RewardFlags` delta in `docs/causes.md` §2. Every constructor
  validates; construction with no evidence fails (I-7). 100% statement coverage,
  including a test that fails the build if `docs/causes.md` and the coded cause
  taxonomy drift apart (ADR-0005).
- `internal/clock` (task 1.4): NTP offset measurement over SNTP, degrading
  honestly — a typed error, never a fabricated offset — when every configured
  server fails within a bounded, jittered retry budget (I-5). `Tracker` remembers
  the last successful reading so a later failure can still report "time of last
  successful sync" (docs/causes.md, `local.host.clock_drift`). No server is built
  in; a caller supplies whatever the operator configured (I-4). Zero new
  dependencies.
- `cmd/whymiss/main.go`: a minimal stub so the binary builds and cross-compiles
  (I-13). The real CLI surface arrives in Phase 2 task 2.7 with the `cobra`
  dependency it needs.
- ADR-0006: `gopkg.in/yaml.v3` for `manifest.yaml` and scenario files.
- `test/e2e/kurtosis`: a two-participant devnet (Lighthouse+Geth, Prysm+Geth),
  matching BUILD_PROMPT §3's initial client scope. `make devnet.up/down/info`.
  See its README for hard-won configuration constraints (network name must be
  the literal `"kurtosis"`; Fulu/BPO forks must be pushed out to avoid a
  128-validator-per-node requirement unrelated to what whymiss tests).
- `tools/faultinjector` (task 1.5): reproducibly injects a declared fault into
  the running devnet and records what actually happens as a corpus scenario.
  Three mechanisms verified against a live devnet — `pause` (`docker
  pause`/`unpause`), `cgroup_io` (cgroup v2 `io.max`, written from the Docker
  host's own namespaces since a container's cgroup interface is correctly
  read-only from inside), and `netem` (`tc netem` on a container's host-side
  veth, verified end to end on a native Linux VM: ~300ms of measured added
  latency, reverting cleanly). `clock_skew` remains unimplemented — libfaketime
  needs LD_PRELOAD set at process launch, which cannot be attached to an
  already-running client without restarting it; see its doc comment.
  `peer_drop` reuses the verified pause mechanism aimed at a peer's container.
  Every value written to `observations.jsonl` is measured against the real
  beacon API during the run, never synthesized (docs/BUILD_PROMPT.md §8).
- `tools/corpusctl` (task 1.6): validates a corpus scenario's `manifest.yaml`
  (cause/sub-cause/confidence against the taxonomy) and `observations.jsonl`
  (decodes as sorted, valid `domain.Observation` values for the manifest's
  slot). `make corpus.validate`.
- `Observer.PollAttestationPublished`: polls the beacon API's attestation pool
  to detect when a validator's contribution first becomes visible, decoding
  the same SSZ aggregation-bitlist format `CheckInclusion` already used —
  closes the gap between "was it included" and "was it published on time",
  needed to tell a slow signer apart from a disconnected one.
- Two fixed bugs found generating corpus scenarios against a live devnet:
  `dockerContainerID` returned Docker's truncated 12-character ID, which does
  not match the full-ID cgroup paths under `/sys/fs/cgroup/docker/`; and
  `hostNamespaceExec` double-quoted its payload for `nsenter`'s inner shell,
  which let the *outer* (alpine helper) shell expand any `$(...)` in it against
  its own empty filesystem view before nsenter ever switched namespaces —
  single-quoting defers all expansion to the inner shell as intended.
- Three real corpus scenarios, generated end-to-end against the live devnet,
  covering three distinct causes: `test/corpus/vc-frozen-lighthouse`
  (`local.vc_disconnected`); `test/corpus/el-disk-stall` (`local.el_slow`,
  sub-cause `local.el_slow.disk_saturation` — cgroup-throttled EL disk writes
  measurably delayed block processing to ~31s into a 12s slot, evidenced
  directly, not inferred); and `test/corpus/proposer-missed-concurrent-vc-pause`
  (`network.proposer_missed`) — a scenario built to test `local.vc_disconnected`
  whose real result was a genuinely missing block from an unrelated validator,
  relabelled to match the taxonomy's own rule precedence (R-100 before R-400)
  rather than discarded, per docs/BUILD_PROMPT.md §8's "record what happened."
- Two more real corpus scenarios, `test/corpus/p2p-degraded-lighthouse` and
  `test/corpus/p2p-degraded-prysm` (`local.p2p_degraded`), generated on a
  native Linux host using the now-verified `netem` fault. Their READMEs note a
  real measurement caveat found in the process: netem attached to a node's
  host-side veth delays *all* traffic through it, including this tool's own
  polling of that same node's beacon API — so the ~18s gap recorded between
  slot start and block_seen is a real, unfabricated measurement, but not a
  clean read of gossip propagation delay alone.
- `test/corpus/peer-isolated-lighthouse` (`local.p2p_degraded`): the `peer_drop`
  fault applied to a validator whose only peer was paused. The cleanest
  `local.p2p_degraded` evidence generated so far — unlike the `netem` scenarios,
  nothing about measuring it runs through the paused link, so a locally-published
  attestation (~18s in) that was never observed included is a direct read, not
  one confounded by the tool's own polling path.
- Two more bugs found generating scenarios against a fresh, previously-unused
  host: `writeCgroupIOMax` hardcoded the cgroupfs driver's path convention
  (`/sys/fs/cgroup/docker/<id>/`), which does not exist under the systemd
  driver a stock Ubuntu host uses instead
  (`/sys/fs/cgroup/system.slice/docker-<id>.scope/`) — now located with `find`
  regardless of which driver is active; and `hostNamespaceExec` used
  `CombinedOutput()`, which folded a `docker run` image-pull's progress lines
  (stderr) into the command's actual result on a cache miss, corrupting a
  device path — now `Output()`, stdout only, with stderr still attached to the
  error path for diagnostics.
- Four more real corpus scenarios: `test/corpus/el-disk-stall-prysm` and
  `test/corpus/el-disk-stall-severe` (`local.el_slow.disk_saturation`, the
  latter's README noting a real negative finding — a 16x harder throttle did
  not produce a proportionally worse delay, suggesting the poll-based
  measurement is not sensitive to throttle severity); `test/corpus/
  peer-isolated-prysm` (`local.p2p_degraded`, this run landing on a degraded
  rather than fully-missed outcome — inclusion delay 2 instead of 1 — real
  variance in what isolation produces, not a different mechanism).
- **A real bug that affects the timing evidence in every scenario generated
  before this fix.** `RunScenario` held the fault open for `duration`, then
  reverted, *then* started polling for the block and attestation — meaning
  observation could never begin before roughly slotStart+duration, whatever
  actually happened on chain in the meantime. Every "~18-31s into the slot"
  offset recorded in earlier scenarios (`el-disk-stall`, `-prysm`, `-severe`;
  `p2p-degraded-lighthouse`, `-prysm`) was bounded below by how long the fault
  was held, not by anything the fault caused — the negative finding noted in
  `el-disk-stall-severe`'s README ("throttle severity didn't change the delay")
  now has a mundane explanation: the tool wasn't watching yet. Fixed by
  reverting on its own goroutine while `PollBlockSeen` and
  `PollAttestationPublished` run concurrently, watching from the moment the
  fault is applied. Verified: a rerun of `el-disk-stall` after the fix recorded
  `block_seen` at ~3.5s into the slot, not ~31s. The *binary* facts in the
  affected scenarios (block found, included, published) are still genuine
  on-chain observations and unaffected by this bug — only the exact timing
  offsets they carry should be read with that caveat, which their READMEs did
  not previously state as precisely as this.
- Two more real corpus scenarios generated with the timing fix in place:
  `test/corpus/vc-frozen-lighthouse-2` and `test/corpus/vc-frozen-prysm-2`
  (`local.vc_disconnected`) — clean redundant examples, one per client,
  neither confounded by the paused node's own proposer duty.
- `fault_cgroup.go` rewritten to run entirely as direct, native calls on the
  Linux host (`findmnt`/`lsblk`/`os.WriteFile` against `/sys/fs/cgroup/...`)
  instead of wrapping every step in a `docker run --privileged --pid=host`
  helper container plus `nsenter`. The docker-wrapped approach was reliable in
  isolation but proved to intermittently hang under repeated real use, up to
  and including genuine Docker daemon corruption on one VM (`docker ps` and
  `docker logs` disagreeing about a container's existence) that required
  rebuilding the VM. `faultinjector` now runs under `sudo` directly, matching
  the pattern `netem` was already using reliably.
- Observer extension: two new evidence sources, both real measurements against
  the live devnet, never synthesized. `SampleIOPressure`/`SampleMemoryPressure`
  read a container's own cgroup v2 PSI files (`io.pressure`/`memory.pressure`,
  parsing the `some avg10` field) and produce a `host_sampled` observation.
  `SampleEngineCallDurations` scrapes an execution client's own Prometheus
  endpoint (`rpc_duration_engine_{newPayloadV4,forkchoiceUpdatedV3}_success`,
  quantile 0.5) and produces `engine_call` observations — direct evidence of
  Engine API latency instead of inferring it from block/attestation timing
  alone.
- Fixed `resolveMetricsURL`: on a CLI profile's first-ever `kurtosis port
  print` invocation, a one-time analytics-disclosure banner is printed to
  stdout ahead of the actual URL, which broke naive whole-output parsing.
  Now takes the command's last non-empty line, which is always the URL
  regardless of whether the banner is present.
- **A negative finding, and a correction to three previously-committed
  scenarios.** With the timing bug fixed, `el-disk-stall`, `el-disk-stall-
  prysm`, and `el-disk-stall-severe` were regenerated to get trustworthy
  timing evidence — all three came back with a fast `block_seen` and the
  best possible `inclusion_delay` (1), i.e. no observable duty degradation at
  all. A further scenario throttled to 4KB/s (the kernel's practical floor
  for cgroup v2 `io.max` — 1 byte/sec is rejected as an invalid value) showed
  the same healthy result, as did a variant sampling the execution client's
  own Engine API call durations directly (sub-3ms `forkchoiceUpdated`/
  `newPayload`, unaffected by the throttle). Across four throttle severities,
  cgroup `io.max` on the execution client produces no measurable effect on
  duty timing in this devnet's workload — most likely because geth's engine
  call hot path has no synchronous write gated by the throttled block device
  at this load. The three scenarios' original "~18-31s" delays, and the
  el-disk-stall-severe README's "throttle severity doesn't change the delay"
  note, are now understood to have been artifacts of the poller-start bug
  fixed above, not genuine fault evidence. All three are removed from the
  corpus rather than kept with an unsupported label (docs/BUILD_PROMPT.md
  §8). A parallel attempt at `local.host.disk_io` (throttling the CL
  container's own disk I/O and sampling its PSI) produced 0.00% pressure
  across three attempts and was never added to the corpus. `local.el_slow`
  and `local.host.disk_io` remain undemonstrated on this devnet; achieving
  them will need a different fault mechanism than disk I/O throttling.
- Two new fault mechanisms, `cgroup_cpu` (writes cgroup v2 `cpu.max`) and
  `cgroup_mem` (writes `memory.high`, which reclaims/throttles rather than
  OOM-killing the way `memory.max` would), reusing `fault_cgroup.go`'s
  already-verified host-privileged write path. Three new real corpus
  scenarios came out of it, each isolating a different cause:
  `test/corpus/cl-slow-cpu` (`local.cl_slow`, Prysm CL capped to 5% of one
  core — `inclusion_delay: 2` instead of the best-case 1 seen everywhere
  else so far, genuine degradation; its README notes that `block_seen`'s
  offset is measured through the same throttled node's own API, so that
  particular figure may be inflated by the API server itself being slow to
  respond, not purely by validation time); `test/corpus/vc-slow-cpu`
  (`local.vc_slow`, Lighthouse VC capped to 1% — `attestation_published` at
  4.18s, past the ~4s attestation deadline, while the head had been
  available since 1.67s — a genuine head-before-deadline,
  publish-after-deadline split); and `test/corpus/host-memory-pressure`
  (`local.host.memory_pressure`, EL's `memory.high` capped to 128MB —
  29.24% memory PSI, and the CL never imported this slot's own block within
  the full 36-second watch window, though it did exist and was canonical
  once checked minutes later after the fault cleared — genuine, severe
  degradation, not a polling bug; see its README for the full explanation
  of why `block_seen: false` here doesn't mean the block never existed).
  None of the three faulted containers crashed or needed a restart.
  A fourth attempt, `el-slow-cpu` (same idea, EL capped to first 5% then
  1% — the kernel's practical floor for `cpu.max`), showed the same zero
  effect the disk-throttle family did: sub-3ms Engine API calls, `block_seen`
  under 700ms, `inclusion_delay: 1` at both quotas. `local.el_slow` remains
  undemonstrated on this devnet by any resource-cap mechanism tried so far —
  disk bandwidth and CPU quota both leave this workload's near-empty blocks
  unaffected. The next thing worth trying is active competing load (e.g. a
  CPU-bound process pinned into the same cgroup) rather than a passive cap,
  since a passive cap only bites when the real workload needs more of the
  resource than the cap allows, and this devnet's per-slot work apparently
  never does.
- Retried `local.host.disk_io` on the execution client (an earlier attempt
  on the consensus client, `cl-disk-stall-lighthouse`, was never committed
  after reading 0.00% PSI across three tries) — same 4KB/s `io.max` floor,
  sampling `io.pressure` directly on `el-1-geth-lighthouse` instead of
  inferring from duty timing. Also 0.00%. Unlike `memory.high` (which
  creates pressure passively, since existing resident memory alone can
  already exceed a tight cap), `io.max` only registers pressure if the cgroup
  actually tries to read or write past the throttle — and this devnet's
  clients evidently don't, on either node. `local.host.disk_io` remains
  undemonstrated here by any passive cap.
- `RequireProposerValidators` (`require_proposer_validators` in a scenario
  file): the inverse of the existing `AvoidProposerValidators` — restricts
  the watched slot to one whose *proposer* duty falls in a given range,
  instead of excluding it. Built for `network.late_block`: throttle the
  proposer's own node so its block is genuinely late for every observer,
  not just the locally-faulted one, then watch a validator on the *other*,
  unthrottled node so the observation itself isn't confounded. Three
  attempts at the CPU quota (1%, 10%, 30%) on `cl-1-lighthouse-geth` as
  proposer, watching validator 40 on node 2: 1% made it skip the slot
  entirely (`network.proposer_missed` — confirmed via a direct chain query,
  no canonical block at slot 2287 at all); 30% showed zero effect
  (`block_seen` at 641ms, healthy); 10% landed in between but still healthy
  enough to not count (`block_seen` at 1.23s, `inclusion_delay: 1`). The
  band between "skips the slot" and "no effect" appears to be very narrow —
  three tries didn't land in it. `network.late_block` remains undemonstrated
  on this devnet; the mechanism (`RequireProposerValidators`) is kept since
  it is generically useful for any future proposer-targeted scenario, but no
  scenario file currently exercises it.
- Fixed a real reliability bug in `RunScenario`: if polling errored out
  after a fault was applied (any HTTP response other than 200/404 — this
  was first hit by `network-inclusion-failure`'s 90%-loss netem attempt,
  which made this tool's own polling time out), the function returned
  immediately without reverting, leaving the fault permanently active on the
  devnet. Confirmed in practice: a 90%-loss `netem` qdisc was still attached
  to `cl-1-lighthouse-geth`'s veth on a *later*, unrelated run, breaking that
  run's own genesis fetch too. Fixed with a `sync.Once`-guarded revert
  wrapped in a `defer`, so every exit path — success or error — reverts
  exactly once, immediately rather than waiting out the fault's full
  declared duration.
- Attempted `network.inclusion_failure` via moderate (40%) `netem` packet
  loss on the watched validator's own node (not a full peer drop, unlike
  the existing `local.p2p_degraded` scenarios): `attestation_published` at
  4.33s (past the ~4s deadline), no `block_seen`. Checking a *second*,
  unthrottled node's view of the same slot showed the block did exist and
  was canonical — the throttled node simply never received it over its own
  degraded peer link. That is `local.p2p_degraded`'s mechanism exactly (the
  *observing* node's own reception failed), not a network-wide "published
  then dropped by an aggregator" case, so this scenario was not added —
  keeping it would have meant relabelling working evidence for a cause we
  already have, not adding a new one. `network.inclusion_failure` remains
  undemonstrated; docs/causes.md itself notes this cause is inherently hard
  to isolate from `local.p2p_degraded` even for a real RCA engine with full
  peer-count evidence, which this toolchain doesn't yet collect.

### Phase 1 corpus: final state for this pass

11 real, devnet-verified scenarios across 6 distinct causes:
`local.p2p_degraded` (4), `local.vc_disconnected` (3), `local.cl_slow` (1),
`local.vc_slow` (1), `local.host.memory_pressure` (1),
`network.proposer_missed` (1). Task 1.7's original target — ≥20 scenarios
across ≥8 causes — is not fully met. Two of the eight taxonomy causes
this pass tried for were judged not achievable with this project's current
devnet and toolchain, not merely unlucky:

- `local.host.cpu_steal` needs real hypervisor-level CPU contention
  (`%steal` in `/proc/stat`), which a cgroup quota cannot produce — cgroup
  throttling shows up as reduced `%user`/`%sys` or `cpu.stat`'s
  `throttled_usec`, never as `%steal`. Reproducing it honestly would need a
  genuinely oversubscribed hypervisor or nested virtualization, neither of
  which this VM's machine type supports.
- `network.inclusion_failure` and `network.late_block` were both attempted
  (see above and the `RequireProposerValidators` entry) and both remain
  undemonstrated after multiple real tries — not for lack of trying, but
  because this devnet's two-node, low-load topology doesn't produce a clean
  middle ground between "no effect" and "a different, already-covered
  cause."
- `local.el_slow` and `local.host.disk_io` were also tried repeatedly (disk
  bandwidth throttling at four severities down to the kernel's practical
  floor, CPU quota at three levels) and never produced measurable effect —
  this devnet's per-slot execution/consensus workload is evidently too
  light for any passive resource cap to gate.

Decision: stop generating further scenarios for this pass at 11/6 rather
than continue chasing causes this devnet's workload doesn't produce. The
path to the remaining causes is a different class of fault (active
competing load pinned into a target's cgroup, rather than a passive cap) or
a different devnet (one with real transaction/block load, or one running on
infrastructure that can produce genuine hypervisor-level contention) —
noted here as the concrete next step if this is revisited, not attempted in
this pass.

## Phase 2 — Collector and timeline

### Added

- `internal/source/beaconapi` (task 2.1): the standard Beacon API adapter —
  REST polling (genesis/spec, attester and proposer duties, block existence,
  attestation pool inclusion) plus an SSE stream for `head`/`chain_reorg`
  events with reconnect and full-jitter exponential backoff (I-5). Every
  request is rate-limited and timeout-bounded (I-1, I-5). Ports and
  hardens logic already proven in `tools/faultinjector`'s throwaway
  Observer, structured as a real production adapter this time: a small
  `AttesterDuty` type (in this package, not `internal/domain`, since
  `ValidatorCommitteeIndex` is Beacon-API decoding mechanics, not a fact
  the frozen domain model needs to carry) rather than modifying
  `domain.Duty`.
- A real bug found while capturing testdata against a live devnet:
  `fetchBlock` read a `Eth-Consensus-Block-Root` response header to get a
  block's root — that header does not exist on a real Lighthouse response
  (verified: full header dump captured, absent). Silently produced an empty
  root string every time, in both this new package and the
  `tools/faultinjector` code it was ported from (harmless there in
  practice: no committed corpus scenario ever populated `block_root`, an
  optional attribute). Fixed by reading `GET
  /eth/v1/beacon/headers/{slot}`'s `data.root` field instead, which also
  conveniently carries `proposer_index` — one request instead of one plus a
  header that was never there.
- Unit tests use only real captured Beacon API responses under
  `testdata/` (BUILD_PROMPT.md §8), recorded against the project's Kurtosis
  devnet: genesis, spec, attester/proposer duties, a block, a block header,
  the attestation pool, and one real SSE `head` event. `chain_reorg`
  parsing has no test yet — no reorg occurred during the capture session,
  and hand-writing a substitute payload would violate the same rule; noted
  in `stream_test.go` rather than papered over.
