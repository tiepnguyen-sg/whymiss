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
- `internal/source/promscrape` (task 2.2): scrapes an execution client's
  Engine API call durations (`newPayload`/`forkchoiceUpdated`), normalised
  into `domain.MetricSample`. Test data reuses the real geth metrics sample
  already captured for `tools/faultinjector`.
- `internal/source/promscrape`'s CL side: peer count, normalised across
  clients into one `cl_peer_count` metric despite genuinely different
  underlying metrics — captured live against the devnet, Lighthouse
  exposes an unlabelled `libp2p_peers` gauge, Prysm exposes
  `connected_libp2p_peers{agent="..."}` split per peer-client-type and
  needing a sum across labels to get the total. The real captures only
  ever had one peer connected, so the sum-across-multiple-labels path
  (`SamplePrysmPeerCount`'s whole reason to scan every matching line) has
  no test with more than one label — noted in `peers_test.go` rather than
  exercised with an invented second line, same discipline as
  `stream_test.go`'s `chain_reorg` gap above.
- `internal/source/hostmetrics` (task 2.3): disk I/O and memory pressure
  via the host-wide PSI files (`/proc/pressure/*`), CPU steal via
  `/proc/stat` deltas between successive samples. Both degrade to a clear
  error rather than a fabricated zero when the file is absent (I-3) — true
  on any non-Linux host, verified directly since this was written and
  tested on macOS. Clock drift stays `internal/clock`'s job (task 1.4), not
  duplicated here.
- `internal/source/registry.go` (task 2.4): `DetectConsensusClient` maps a
  node's self-reported version string to `ConsensusLighthouse`/
  `ConsensusPrysm`/`ConsensusUnknown`, tested against the real
  `"Lighthouse/v8.2.2-e423a66/x86_64-linux"` value captured for the
  beaconapi work. Prysm's detection arm follows the public naming
  convention but is unverified — no real Prysm response has been captured
  in this project yet — and the doc comment says so rather than implying
  confidence this package doesn't have.
- `internal/timeline` (task 2.6): `Assembler` collects observations,
  samples, and duties from however many adapters are producing them and
  builds a `domain.Timeline` with deterministic ordering — sorted by
  timestamp with a fixed, multi-field tie-break rather than arrival order,
  which is not reproducible across runs when adapters run on separate
  goroutines. `Replay` (task 2.8) rebuilds a `Timeline` from a corpus
  scenario's `observations.jsonl`, tested against `test/corpus/vc-slow-cpu`
  — real, already-committed data, including a test asserting replaying the
  same file twice produces byte-identical output (BUILD_PROMPT.md §10.3's
  explicit requirement).
- `internal/store` (task 2.5): SQLite via `modernc.org/sqlite` (ADR-0007),
  WAL mode, `synchronous=NORMAL`, forward-only migrations tracked in
  `PRAGMA user_version`, retention by both age and byte count via
  `PRAGMA page_count`/`page_size` followed by `VACUUM` to actually shrink
  the file. Two real bugs a test caught before this shipped: timestamps
  were stored via `time.RFC3339Nano`, whose trailing-zero-trimming produces
  strings of different lengths for different sub-second precisions — since
  `ORDER BY` on a SQLite TEXT column is byte-wise, a 0ms observation could
  sort *after* a 600ms one recorded earlier, because `.` (0x2E) sorts
  before `Z` (0x5A); fixed with a fixed-width layout instead. Also a test
  fixture bug (an attribute set on a kind that doesn't permit it), caught
  by `domain.NewObservation`'s own validation doing its job.
- `internal/app` (composition root, first populated this pass) and
  `cmd/whymiss` (task 2.7, ADR-0008 for `spf13/cobra`): `whymiss watch`
  runs the collector daemon — streams `head`/`chain_reorg` events, samples
  host pressure on a timer, prunes on a timer, all persisted to SQLite —
  and `whymiss timeline <slot> --format json` reads it back through
  `internal/timeline.Assembler`. Per-duty tracking (polling a specific
  validator's block/attestation/inclusion, which needs to know which
  validator to watch) is not yet wired into `watch`'s loop — that needs a
  config surface (`koanf`, still unused) this pass didn't add, since
  nothing yet requires multi-source config. `whymiss <slot>` and `doctor`
  stay unimplemented placeholders per AGENTS.md's fixed CLI surface —
  `<slot>` needs the RCA engine (Phase 3), `doctor` is Phase 4.
- Three of Phase 2's DoD checkboxes (BUILD_PROMPT.md §10.3), closed after
  an honest look found they weren't actually done despite every individual
  task above having code and tests:
  - `TestReplay_ByteIdenticalAcrossRuns` now replays every scenario under
    `test/corpus/` (11, read from the directory rather than hardcoded, so a
    newly added scenario is automatically covered), not just one. The DoD
    line says "every corpus scenario," and one was not that.
  - `docs/configuration.md`: every `whymiss watch`/`timeline` flag, its
    default, and a safe range with the reasoning behind it (e.g.
    `--retention-max-bytes`'s 100MiB–10GiB range is sized against a
    Raspberry Pi 5's typical disk budget, I-12).
  - `make check.nonroot`: builds the binary, refuses to run itself as root
    (so the check can't accidentally pass by running as root itself),
    checks it carries no Linux capabilities via `getcap` where available,
    and runs `--help` to prove it starts and exits cleanly. Wired into
    `make check`, so `make ci` now enforces I-3 for `cmd/whymiss` itself,
    not just documents the intent.
- **A real bug unit tests could not have caught, found by actually running
  `whymiss watch` against a live devnet:** the collector never produced a
  `slot_start` observation — the SSE stream only carries `head`/
  `chain_reorg` events, and nothing else in the loop derived one. Every
  unit test for `GetTimeline`/`Assembler` had hand-assembled its input
  observations and always happened to include one, so this was invisible
  until `whymiss timeline <slot>` was run against data `watch` had actually
  collected, where it failed for every slot with "no slot_start
  observation recorded." Fixed with a `runSlotClock` goroutine that writes
  a derived `slot_start` for every slot as it begins, computed from
  genesis + the slot schedule (the same source `tools/faultinjector`
  already used). Verified end to end against the live devnet afterward:
  `slot_start` at `09:06:16.000000000Z`, `head_updated` at
  `09:06:16.05497891Z` for the same slot — 55ms apart, a real block arrival
  offset — and `whymiss timeline 4029 --format json` returned the complete,
  correctly ordered timeline for it.
- Closed the last of Phase 2's DoD gaps that didn't need the 72-hour soak
  (that one's still open — see below): "adding a hypothetical third client
  would touch only internal/source/" was asserted in `docs/architecture.md`
  but not actually true yet, because task 2.4's "adapter selection" half
  had never been built — only "detection" had. `internal/app` calling
  `promscrape.SampleLighthousePeerCount` directly (the only way to actually
  *use* the CL peer-count work from CHANGELOG's earlier entry) would have
  put a client name in `internal/app`, failing `make check.isolation`
  itself. Fixed with `internal/source/peers.go`'s `SamplePeerCount`
  dispatcher — the actual "adapter selection" — and wired it into `whymiss
  watch` (`--cl-metrics-api`, `--peer-sample-interval`) so it's exercised,
  not just present. `docs/architecture.md` §5 now walks the exact files a
  third client (Teku, as the example) would touch, and states plainly that
  `internal/app` doesn't change — true now, checked by `make
  check.isolation` on every CI run, not merely written down.
- **Still open**: the 72-hour Hoodi testnet soak (RSS/disk/goroutine-leak
  ceilings, `goleak`) — needs real testnet infrastructure and wall-clock
  time this pass didn't have. Everything else in BUILD_PROMPT.md §10.3's
  Phase 2 DoD is closed.

## Phase 3 — The RCA engine

### Added

- `internal/rca` (tasks 3.1–3.4): the pure `Analyze(Timeline, Config) Verdict`
  engine (ADR-0003) — twelve ordered, first-match-wins rules (R-010 through
  R-999, `internal/rca/rules`), one file per rule, each with a written
  position justification in `rules/order.go`. `Config`/`DefaultConfig`
  carry every `thresholds.*` value from `docs/causes.md` §5 verbatim.
  `deriveOutcome` (`outcome.go`) computes `Outcome`/`RewardFlags` from the
  timeline before any cause rule runs — a documented simplification where
  the closed observation vocabulary has no independent checkpoint-
  correctness signal, so `TimelySource`/`TimelyTarget` are treated as
  earned whenever the attestation was included at all.
- `internal/report` (task 3.5): `JSON` (indented, `domain.Verdict`'s own
  tags) and `Markdown` renderers. Markdown output is verified readable —
  it's what's pasted into this README's sample report.
- `whymiss <slot>` (task 3.6, `cmd/whymiss/root.go`): wired to
  `internal/app.Explain` (`GetTimeline` + `rca.Analyze`), with
  `--format markdown|json`. No longer a placeholder error.
- `internal/rca/golden_test.go` (task 3.7): replays every `test/corpus/*`
  scenario and asserts `Analyze`'s cause/sub_cause matches `manifest.yaml`.
  All 11 scenarios pass. Full unit coverage added alongside it: one
  `_test.go` per rule, `engine_test.go` (first-match-wins, the no-panic
  safe-fallback path on a malformed rule draft, the no-duty short-circuit),
  `outcome_test.go`.
- `tools/eval` + `docs/evaluation.md` (task 3.8): walks `test/corpus`,
  runs each scenario through `Analyze`, and reports top-1 accuracy plus
  per-cause precision/recall as a committed Markdown table. Current
  measured result: **11/11 (100%) top-1 accuracy, 0 false-`high`
  verdicts** — see the caveat below on what this number does and doesn't
  cover.
- `internal/rca/determinism_test.go` (task 3.9): re-analyzes one real
  corpus timeline 1,000 times and asserts byte-identical JSON output.
- Three genuine correctness bugs found by reasoning through the real
  corpus data before the golden test ever ran, each because a rule's
  first-draft condition was broader than the evidence actually justified:
  R-010 (`local` data-completeness) originally fired on any block-seen
  absence paired with attestation activity, which would have wrongly
  pre-empted a scenario where a validator client legitimately attests to a
  stale head its own node hasn't seen the block for yet; R-100
  (`network.proposer_missed`) and R-400 (`local.vc_disconnected`) had the
  same shape of bug in the opposite direction. All three fixed by requiring
  the *specific* combination of absent observations the taxonomy actually
  describes, not just one of them — documented in each rule's own doc
  comment.
- A fourth bug the golden test *did* catch: `Stages.Dominant` treated a
  single known stage (propagation, when `attestation_published` was never
  captured — this build's most common real shape) as trivially "100%
  dominant," which isn't a claim about where the time went, just an
  artifact of having nothing else to compare it to. This silently made
  `local.vc_disconnected` and `local.cl_slow` unreachable for several real
  scenarios, since `local.p2p_degraded` (R-200, ordered earlier) matched
  first every time. Fixed in two parts: `Stages.Dominant` now requires at
  least two known stages before comparing shares at all; R-200 gained a
  second, absolute-duration path for the single-stage case (propagation
  alone exceeding the attestation deadline) plus an explicit deferral to
  R-400 when there's no attestation activity whatsoever, since that's a
  stronger signal than propagation timing either way. A related ordering
  bug in R-310 (`local.cl_slow`) — its generic "no engine_call evidence"
  fallback was pre-empting R-410's (`local.vc_slow`) far more specific
  timing evidence — was fixed the same way: R-310 now defers when R-410's
  exact match condition is also present.

### Known gaps — not attempted this pass

- **Task 3.10, corpus growth to ≥50 scenarios, not done.** The measured
  100%/0-false-high numbers above are real but only cover the 11 scenarios
  that existed going into this phase, across 6 of the taxonomy's causes.
  `local.el_slow` (any sub-cause) and `network.late_block` have zero corpus
  coverage — `network.late_block` structurally can't be exercised until
  Phase 5's baseline exists (R-110 is written and unit-tested against
  synthetic timelines, just never hit by the real corpus).
  BUILD_PROMPT.md §11.3's "adversarial and ambiguous cases that should
  yield `unknown`" is likewise untested against real data for the same
  reason — no such scenario exists in the corpus yet.
- Several rules are deliberately simpler than `docs/causes.md`'s literal
  formula, documented as such in each rule's own comment rather than
  hidden: R-300/R-310 use "any `engine_call` evidence exists at all" as
  the EL/CL elimination signal rather than the documented "Engine API
  accounts for ≥/< half the validation stage" split (no rolling-p99
  baseline is computed anywhere in this codebase yet to make that split
  meaningful); R-410 checks `block_seen`/`attestation_published` directly
  against the deadline rather than requiring "signing stage dominant,"
  since `head_updated` — the observation the real Signing/Validation split
  depends on — is never populated by any collector in this build
  (`ComputeStages`'s doc comment covers the degraded fallback this forces).

### Task 3.10 — corpus correctness audit and real peer-count corroboration

Started as "add one real peer-count-corroborated scenario" and grew into a
correctness audit once generating it surfaced a genuine, dangerous gap.

- **A real false-`high` bug, found via a live devnet run, not a
  hypothetical.** Generating a fresh `p2p-degraded-prysm` scenario
  produced a severely propagation-degraded slot (`block_seen` at +13.6s,
  deadline ~4s) where Prysm's validator client gave up on the duty
  entirely rather than publishing late — no `attestation_published`, no
  `attestation_included`. R-400 (`local.vc_disconnected`) only ever
  checked *whether* those observations existed, never *when*
  `block_seen` happened, so this shape hit R-400 at `ConfidenceHigh`
  ("directly observed, not inferred") for a problem that was actually
  propagation, not VC-to-BN connectivity — exactly the false-confident-
  verdict failure mode I-8 and Phase 3's own DoD treat as most serious.
  Fixed: R-400 now defers (returns no match) when `block_seen` exists and
  is not before the attestation deadline, leaving that shape to R-200. R-200's
  matching first-line defer to R-400 (added earlier this phase for the
  `vc-frozen-*` shape) is now redundant with R-400's own narrower check and
  was removed — R-100 already rules out "no block_seen at all" ahead of
  R-200 in the order, so R-200 never needed its own copy of that logic.
- **Discovered while chasing the above: three already-committed
  `vc-frozen-*` scenarios had stale timing evidence.** All three fault
  the *VC* container only (`pause`), never the CL, so `block_seen` — read
  via the CL's own beacon API — should be fast (~1s) regardless. All
  three instead showed ~18.1–18.2s, the same signature CHANGELOG already
  identified as inflated by the poller-start-timing bug (`RunScenario`
  didn't begin polling until after the fault's full duration elapsed) —
  a bug fixed for `p2p-degraded-lighthouse`/`-prysm` and the
  `el-disk-stall` family, but these three were never regenerated
  afterward. Regenerated against the live devnet: all three now show
  `block_seen` at 0.2–0.7s, no `attestation_published`/`attestation_included`,
  same `local.vc_disconnected` cause — the label was right, only the
  timestamp was stale.
- **`local.p2p_degraded`'s original two scenarios turned out to be
  unreproducible with the poller-timing bug fixed, and were regenerated
  with a real confound found and closed.** `p2p-degraded-lighthouse`/`-prysm`
  (200ms `netem`, no proposer constraint) reran with clean timing and
  came back with *zero* measurable effect — the same "the tool wasn't
  watching yet" pattern already known from `el-disk-stall`. Root cause
  once measured correctly: 200ms is trivial, and a slot where the
  degraded node happens to propose its own block sees no delay at all,
  since a locally-produced block never traverses the throttled network
  path. Fixed with two changes together: 3s of `netem` delay (enough to
  clear the ~4s deadline once combined with this devnet's ~1s baseline,
  while staying under half the HTTP client's 10s timeout so the
  observer's own polling — which shares the same throttled path — doesn't
  time out) and `require_proposer_validators` forcing the *other* node to
  be the proposer, so `block_seen` genuinely depends on cross-node gossip.
  Both regenerated against the live devnet with real degradation
  (`block_seen` 7.6s/14.1s past slot start).
- **`peer-isolated-lighthouse`/`-prysm` removed from the corpus: real
  reruns showed the `peer_drop` (pause) mechanism structurally can't
  isolate a validator from its only peer on this two-node devnet.**
  Pausing the *entire* peer node doesn't just cut connectivity — it also
  makes the un-paused side the sole active proposer for the whole fault
  window (the paused side literally cannot propose while paused), so
  every slot in the window gets self-produced *and* self-included with
  zero dependency on the paused peer. Confirmed symmetrically: reruns of
  both scenarios came back fully healthy (`block_seen` <1s, `inclusion_delay`
  1) with the proposer each time landing on the very node whose peer was
  paused. This is a property of "pause the whole node" on a 2-node
  topology, not something a parameter tune fixes — matches
  `local.p2p_degraded`'s existing evidence via the `netem` scenarios
  above, so nothing is lost.
- **Real peer-count corroboration in the corpus for the first time.**
  R-200's `ConfidenceHigh` branch (peer count below `peer_count_min`)
  had zero real-world coverage — no corpus scenario had ever sampled
  peer count, and even if one had, R-200 only ever read
  `Timeline.SampleValue` (the live-collection `MetricSample` form),
  never the `Observation` form `tools/faultinjector` actually produces.
  Fixed both sides: `peerCountValue` (`internal/rca/rules/helpers.go`)
  checks the `Observation` form first, falling back to `SampleValue`,
  mirroring `hostSampledValue`'s existing pattern; `tools/faultinjector`
  gained `peer_count_target` (`Scenario`), `SamplePeerCount`
  (`observe_peers.go`, reusing `internal/source.SamplePeerCount` directly
  rather than reimplementing client-specific parsing), and the
  observation-building wiring. `p2p-degraded-lighthouse` now samples
  real peer count (0, on this two-node devnet — always below
  mainnet-scale `peer_count_min`) and correctly lands on `ConfidenceHigh`.
- **Fixed a real reliability bug in scenario duty selection, found the
  hard way (five consecutive failed attempts across five epochs).**
  A scenario naming one fixed `validator_index` has exactly one candidate
  attester-duty slot per epoch; combined with a proposer constraint
  (`avoid`/`require_proposer_validators`), that slot has roughly even
  odds of being usable at all, and a miss costs a full epoch (~6.4
  minutes) to retry. Fixed with `Scenario.ValidatorCandidates` (an
  inclusive validator-index range — normally a whole node's validator
  set) and a rewritten `findCleanDuty` that asks for every candidate's
  duty in one request (the beacon API already accepts an array; asking
  about 32 validators costs the same as asking about one) and picks
  whichever lands on a usable slot — makes a usable slot essentially
  certain on the first attempt instead of a coin flip. Also added a
  minimum-lead-time check (a candidate slot too close to "now" to still
  act on is skipped), which the original single-candidate version had no
  way to route around either.
- Raised `observe.go`'s HTTP client timeout from 10s to 25s. A
  `cgroup_cpu` fault applied directly to the node this tool polls also
  starves that node's own REST API of CPU — the same process serves
  both — so a quota low enough to produce genuine duty degradation was
  also low enough to make polling itself time out before any
  degradation could be observed (verified generating `cl-slow-lighthouse`:
  5/7/9% quota all timed out outright; only 10% answered inside 10s, but
  by then there was too little CPU pressure left to degrade the duty).
  25s gives real CPU pressure room to answer without hunting for an
  ever-narrower quota window.
- **Three negative findings from this pass, corpus unchanged as a
  result — evidence for these causes stays exactly what it was.**
  - `cl-slow-lighthouse`: even with the timeout fix, throttling a CL's
    own CPU makes `block_seen` and `attestation_published` measured
    through that same starved node's API arrive within milliseconds of
    each other regardless of throttle severity, since both come from the
    same slow API rather than independent signals — the derived
    "validation span" is always ≈0, so the shape always reads as
    `local.p2p_degraded` (100% propagation share) rather than
    `local.cl_slow`, never mind how low the quota goes. `local.cl_slow`
    already has clean evidence from `cl-slow-cpu` (Prysm); not pursued
    further.
  - `vc-slow-prysm`: four quotas tried (1%, 3%, 5%, 8%); Prysm's
    validator client showed near-binary behaviour — either abandoning
    the duty entirely (no publish at all, `local.vc_disconnected`'s
    shape) or answering close to normally — never landing in the
    "attempts a late publish" band `local.vc_slow` needs, unlike
    Lighthouse's validator client at a single quota (1%, `vc-slow-cpu`).
    `local.vc_slow` already has clean evidence from `vc-slow-cpu`
    (Lighthouse); not pursued further.
  - `host-memory-pressure-prysm`: real memory pressure was produced
    (27–30% PSI, matching `host-memory-pressure`'s Lighthouse-side
    figure), and a direct chain query confirmed the slot's block
    genuinely existed and was canonical (the starved node's own
    collector just never saw it in time — the same shape as
    `host-memory-pressure`) — but Prysm's validator client published
    nothing at all rather than attesting to its last known head the way
    Lighthouse's did in the original scenario, landing on
    `local.network.proposer_missed`'s shape instead. `local.host.memory_pressure`
    already has clean evidence from `host-memory-pressure` (Lighthouse);
    not pursued further.
  - Across all three: Prysm's validator client appears to withdraw from
    a duty entirely under sustained resource pressure rather than
    degrading gradually the way Lighthouse's does, which made every
    Prysm-side variant of an already-covered cause land on
    `local.vc_disconnected` or `network.proposer_missed` instead of the
    intended cause. Worth remembering if this is revisited: the
    mechanism that works on Lighthouse does not reliably transfer.
- Corpus now 9 real scenarios across 6 causes (`local.p2p_degraded` ×2,
  `local.vc_disconnected` ×3, `local.cl_slow`, `local.vc_slow`,
  `local.host.memory_pressure`, `network.proposer_missed` ×1 each) — down
  from 11 (two non-reproducible `peer-isolated-*` scenarios removed), but
  every remaining scenario's evidence has now been verified against the
  current, bug-fixed `RunScenario`. `make eval`: 100% top-1 accuracy,
  0 false-`high` verdicts (`docs/evaluation.md`).
- **Still not attempted**: growing past 9 scenarios, `local.el_slow`,
  `local.host.disk_io`, `local.host.cpu_steal`, `network.late_block`,
  `network.inclusion_failure`, `clock_skew` — unchanged from the
  reasoning already on record earlier in this file; nothing new tried
  this pass.
