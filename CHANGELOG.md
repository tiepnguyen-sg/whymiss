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
