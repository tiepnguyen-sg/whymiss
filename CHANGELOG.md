# Changelog

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). The
version stays at `v0.x` until the API is stable.

## [Unreleased]

### Added

- Tagged releases now publish an exact-version GHCR image manifest for Linux amd64
  and arm64. BuildKit attaches OCI SBOM/provenance attestations, cosign signs and
  verifies the digest, and the GitHub release stays draft until every binary and
  container verification gate succeeds. Release execution now fails before creating
  artifacts while the repository is private. `make test.image` exercises both image
  architectures locally without publishing.
- `make test.soak` provides the repeatable Linux Hoodi release soak required by the
  Phase 2 gate. It defaults to 72 hours, records RSS and physical SQLite usage once
  a minute, enforces the 256 MiB process and 100 MiB retention ceilings, and keeps
  its private log, samples, and summary as local release evidence. It refuses to
  overwrite any existing evidence directory.
- CI now runs GitHub's dependency review on pull requests, restoring the Phase 1
  gate that rejects newly introduced vulnerable dependencies.
- `whymiss watch` now collects the network baseline it already knew how to reason
  about. R-110 and R-200 were rewritten to require `tl.Network` before blaming
  local propagation — correctly, since a block that was late everywhere is not a
  local fault — but nothing in the daemon ever produced a
  `network_baseline_sampled` observation; only `tools/faultinjector` did, for
  corpus fixtures. A real operator therefore always had `tl.Network == nil`, so
  both rules always declined, and every propagation question fell to
  `unknown.insufficient_data`. That silently disabled the "or was it the network?"
  half of the product's own question. New `--baseline-beacon-api` /
  `--baseline-metrics-api` (`watch.baseline_beacon_api`,
  `watch.baseline_metrics_api`) point at a second, independent beacon node and
  record its block-arrival timing per slot. Both are empty by default — reaching a
  second node is explicit operator configuration, never implicit egress — and
  validation rejects setting only one of the pair, or pointing the baseline back
  at the watched node (which would report local lateness as network-wide lateness
  and exonerate a real local fault). One reference node yields a one-sample
  baseline, which R-200 already caps at medium confidence; the observation shape
  is unchanged from the corpus form, so a real percentile source can replace the
  value without touching a rule.
- Absence-based RCA now requires a positive `collection_completed` observation after
  the full Deneb/EIP-7045 inclusion window through the end of the following epoch;
  API or persistence failures degrade to `unknown.insufficient_data` instead of
  becoming false misses.
- Taxonomy 2.0 adds the positive `block_skipped` observation. R-100 now requires a
  fully synced, non-optimistic Beacon node whose head has advanced beyond the slot
  and a repeated canonical-header 404; missing local observations no longer
  masquerade as `network.proposer_missed`.
- `whymiss doctor` now performs a blocking preflight for read-only beacon access,
  database writability, and a fresh trusted NTP sample.
- The fault-injection corpus format now records generator version, every NTP
  exchange used during collection, and an SHA-256 digest of
  `observations.jsonl`; validation rejects stale, incomplete, or modified fixtures.
- The release evaluation gate now enforces the Phase 3 corpus floor of 50
  scenarios and requires at least one adversarial/ambiguous case labelled
  `unknown.*`; a small all-positive corpus can no longer report release-ready
  accuracy merely because its few cases match.
- `make corpus.generate.all` regenerates 15 canonical recipes serially against the
  Kurtosis devnet, including deterministic concurrent proposer/attester misses on
  both supported clients. `make corpus.generate.campaign` adds 35 independent live
  records with unique slot/validator provenance, balanced to yield seven records per
  positive cause and eight adversarial `unknown` records instead of inflating the
  release metric with copies of an easy fixture.
- The live fault harness includes an adversarial propagation case with no independent
  network baseline; its required result is `unknown.insufficient_data`, proving the
  engine does not use the harness's hidden fault label as evidence.
- Fault recipes reject validator-candidate ranges above 64 entries and enumerate
  ranges ending at `MaxUint64` without integer wraparound.
- Fault recipes now reject empty/invalid netem, I/O, CPU, clock-skew, and peer-drop
  parameters before waiting for a live duty slot or mutating the devnet.
- Corpus generation replays the facts it just recorded through the production RCA
  engine. A label mismatch remains written for diagnosis but makes the scenario and
  batch fail, so a successful writer cannot be mistaken for an accurate fixture.
- The devnet's Lighthouse validator image now starts with a digest-pinned
  libfaketime preload, and the `clock_skew` injector verifies the target process,
  changes its wall-clock offset live, and restores the exact original value.

### Changed

- CI now rejects unresolved `CHANGEME` markers outside historical changelog text,
  preventing placeholder contact or repository details from reaching a release.
- The release workflow now requires GitHub private vulnerability reporting to be
  enabled, so the confidential channels documented in SECURITY.md and the code of
  conduct cannot ship as dead links.
- Engine latency attribution now rejects startup and gapped counter windows so
  multi-slot work cannot be assigned to one canonical head. CL residual verdicts
  expose per-method counters, and block-root evidence is canonical 32-byte hex.
- Age-based retention now uses 1,000-row transactions with bounded WAL
  checkpoints, runs once immediately at daemon startup, and enables incremental
  auto-vacuum for newly created stores. Legacy pre-release stores keep a full
  `VACUUM` compatibility fallback.
- SQLite connections now enable defensive mode and untrusted-schema handling;
  startup rejects future or incomplete database schemas before collection begins.
- Every non-derived observation now carries a fresh measured clock offset and sample
  timestamp. Missing, stale, invalid, or excessive-offset samples suppress timing
  attribution instead of assuming the host clock is correct.
- RCA configuration, stage calculation, and rule ordering live in the pure rules
  package without mutable globals or `init` side effects. Each order request returns
  an independent copy.
- Domain taxonomy/observation registries and SQLite schema definitions no longer
  live in mutable package-level slices or maps. Callers receive fresh closed-set
  values, validation uses pure switches and startup-local schema data, and domain/
  clock sentinel errors are immutable typed constants. `make check.globals` keeps
  production packages free of mutable globals except the linker-stamped CLI version.
- The I-11 client-isolation gate now covers production Go tooling as well as the
  shipped packages, while ignoring tests and explanatory comments.
- The I-4 egress gate now rejects explicit HTTP clients and request construction,
  not only convenience `Get`/`Post` calls, outside `internal/source`.
- Retention serializes mutations, removes globally oldest observations and samples by
  timestamp, checkpoints WAL state, vacuums reclaimed pages, and verifies the byte cap.
- `watch` validates documented safe ranges, binds its metrics listener before startup,
  owns every background goroutine, and drains them before closing the store.
- CI and release workflows use pinned tool versions, reject formatting or module-file
  drift, rerun the complete gate before release, and reject tags outside `main` or
  absent from the changelog. The release workflow then downloads its published
  assets and verifies checksums, cosign identity, SPDX SBOM shape, and SLSA
  provenance with the same commands documented for operators.
- Docker and systemd examples explicitly configure NTP so their default operator path
  produces trustworthy timing verdicts.

### Fixed

- Required Docker quickstart and systemd values are now empty in their environment
  examples, so startup cannot silently use sample validator indices or an implicit
  public NTP endpoint. Compose stops until the operator supplies the Beacon endpoint,
  validator indices, NTP server, exact signed image tag, and Grafana password; optional
  metrics endpoints stay disabled until explicitly configured, and no example accepts
  `change-me` as an admin credential.
- Align the concurrent proposer-miss corpus scenario with R-100's high-confidence
  contract now that generation records a positive canonical skipped-slot proof.
- Whole-node CPU/memory scenarios select from the affected validator range while
  excluding that node's proposers, preventing resource faults from being confounded
  with a proposal miss and avoiding single-validator no-duty retries.

- Block-arrival Prometheus gauges are now accepted only with a matching
  `beacon_head_slot` from the same scrape. Local and independent-node timing can
  no longer be assigned to a newer watched head, baseline nodes on another chain
  are rejected at startup, equivalent watched/baseline URLs cannot bypass the
  independence check with a trailing slash, and YAML/environment baseline values
  now reach the `watch` flags. After a canonical head arrives, bounded 250ms polling
  gives a lagging metrics gauge up to three seconds to reach that exact slot; a stale
  or already-advanced gauge remains rejected.
- The devnet fault injector applies the same exact-slot check to local and
  independent block timing, so regenerated fixtures cannot reintroduce the
  cross-slot attribution rejected by the live collector.
- `make ci` now validates every committed corpus scenario, enforces at least
  90% top-1 accuracy with zero false-high verdicts, and rejects a stale
  `docs/evaluation.md`; a green release gate can no longer conceal corpus drift.
- Public docs and deployment examples now match the shipped taxonomy, engine
  versions, single Prometheus metric, optional baseline egress, SLSA workflow,
  security-reporting path, and actual collection-window timing.
- Fresh-install CI now boots `whymiss` against a deterministic local Beacon API
  and requires a stable container plus a successful Prometheus scrape; a
  crash-looping sidecar can no longer pass merely because Grafana is healthy.
  The harness always invokes the Compose build before startup, so a stale local
  `whymiss:local` image cannot hide Dockerfile or source changes.
  It also verifies the running UID/GID, read-only root filesystem, dropped
  capabilities, no-new-privileges policy, memory/PID ceilings, absence of a
  published whymiss port, and absence of a shell in the image.
- Fresh-install runtime verification is available locally as
  `make test.freshinstall`; it uses an isolated Compose project and always removes
  its temporary volumes, mock process, and environment file.
- The Docker quickstart now uses the canonical public repository URL instead of a
  non-runnable `<this-repo>` placeholder.
- Contributor corpus instructions now name a committed scenario and include the
  required Beacon service instead of invoking a removed scenario with an invalid
  command.
- Removed obsolete scaffold initialization instructions and placeholder history
  from the initialized public repository.
- Devnet and corpus Make targets accept `DEVNET_ENCLAVE`, allowing a release
  regeneration run to coexist with a stopped or unrelated local enclave; the value
  is now forwarded to every fault-injector lookup instead of silently falling back
  to `whymiss-devnet` for metrics.
- Docker Desktop corpus runs can apply CPU/memory cgroup and host-veth netem
  faults through a digest-pinned, short-lived privileged helper inside its Linux
  VM. Live opt-in tests verify the injected limit/latency and clean rollback.
  Native Linux keeps the direct host path; the production binary is unaffected.
- Docker Desktop netem no longer runs `apk add` during fault application. The
  pinned helper uses its built-in `ip`/`nsenter` and Docker VM's existing `tc`,
  eliminating a DNS/package-mirror dependency from the fault window.
- CPU and memory fault reverts now honor the fresh bounded cleanup context; a
  canceled scenario context can no longer prevent Docker Desktop from restoring
  its original cgroup limit. The injector also snapshots and restores the exact
  pre-fault `cpu.max`, `memory.high`, and per-device `io.max` value instead of
  assuming it was unlimited.
- Fault injection now starts eight seconds before the duty and samples PSI while
  the fault is active but before slot end. Corpus host evidence now uses the same
  in-slot window as live collection instead of a post-slot sample that replay alone
  could see.
- Proposer-missed evidence now carries the beacon node's skip-confirmation time;
  validator-disconnected absence evidence carries collection completion time rather
  than pretending either fact was known at slot start.
- Inclusion-failure, healthy-duty, and catch-all evidence now timestamps conclusions
  at collection completion; publication remains a separate fact at its measured time.
- Regenerated corpus completion markers now carry `validator_index`, matching the
  live collector's duty isolation instead of relying on the legacy single-duty
  compatibility path during replay.
- Corpus generation samples NTP both before the duty and after the full inclusion
  window, then attaches the nearest recorded exchange to each observation. A single
  pre-duty sample can no longer become twelve minutes stale while still being
  declared as provenance for late inclusion evidence.
- Corpus network-baseline observations now retain the scrape timestamp exactly as
  the live collector does; their external percentile values, not local arrival
  time, drive attribution.
- Corpus host-pressure observations now use the live collector's canonical
  `host_iowait_pct` and `host_mem_pressure_pct` metric names; legacy replay names
  remain readable.
- Prometheus help text now describes `cause="none"` for every causeless verdict,
  including healthy duties as well as `no_duty`.
- Corpus generation defaults to a fixed NTP hostname and aborts before fault
  injection when the measured offset exceeds the RCA trust limit; an unhealthy pool
  member can no longer consume a full run and publish a mislabeled clock-drift fixture.
- Corpus generation aborted when the injected fault degraded the node it was
  watching. `faultinjector` sampled `/eth/v1/node/syncing` exactly once at the
  poll deadline and treated a not-yet-recovered node as fatal, so
  an earlier `host-memory-pressure` recipe — which capped geth's cgroup
  `memory.high` to 128MB and left the beacon node reporting `el_offline` for a
  while after the fault was reverted — could never complete. Because
  `make corpus.generate.all`
  ran under `set -eu`, that one scenario killed the whole batch, taking the
  seven untried scenarios after it down as well; that target now reports a
  failing scenario and continues, exiting non-zero at the end with the list.
  The node itself recovers unaided
  (observed on a real devnet: the run died checking slot 3442 while the node
  went on to reach slot 3708 by itself), so the deadline check now waits a
  bounded three minutes for it to return to a fully synced, execution-valid
  state. A node that never recovers still fails: a 404 from a node that is not
  execution-valid is not evidence of a skipped slot (ADR-0015).

- `collection_completed` was stamped before the Deneb/EIP-7045 inclusion window
  had actually elapsed, rejected by `Timeline.Validate`'s own freshness check on
  every regenerated corpus scenario. Root cause: three independent formulas for
  "when is the inclusion window closed" existed across `internal/domain`,
  `internal/app/duty_tracking.go`, and `tools/faultinjector/main.go`; the first
  two happened to agree by coincidence (an undocumented `+3` margin exceeding
  domain's required `+1`), and the collector's own `CheckInclusion` returns as
  soon as the window's last slot is first observed — near that slot's start,
  not its end — so whichever caller didn't separately wait past the window's
  true end stamped too early. `internal/app` always waited; `faultinjector`
  never did, which is why the bug only ever surfaced in regenerated corpus
  data. Fixed by adding one canonical `domain.Slot.CollectionWindowEnd`,
  consumed by all three call sites, and adding the missing wait in
  `faultinjector` to match `internal/app`'s already-correct pattern. Found by
  actually regenerating the corpus against a live devnet after Taxonomy 2.0
  landed, not by inspection — traced with real timestamps (a scenario's
  `collection_completed` at 36.07 slot-durations past its slot start against a
  required minimum of 37.0).
- R-999 now emits every known stage duration and share (and names unavailable
  boundaries); stage totals saturate instead of overflowing on extreme input.
- Inclusion scans terminate safely when a malformed replay window ends at the
  maximum slot, and cached block attestations are shape- and size-validated before
  reuse.
- A node that is unsynced, optimistic, execution-offline, or not yet past the duty
  slot now blocks `collection_completed`; an inconclusive 404 can no longer become
  positive absence evidence.
- R-500 now requires the published vote to match an observed canonical head root
  and carries reorg events from the full inclusion window as contextual evidence.
- Duties from multiple configured validators assigned to the same slot are now
  isolated by `validator_index`; automatic tracking analyzes each independently,
  while interactive explain/timeline commands require a selector only for an
  ambiguous slot.
- Inclusion-window head polling is coalesced across all active validators, keeping
  three overlapping epochs of duty tracking within the configured Beacon API
  request budget.
- Canonical inclusion scans retain one bounded minute of validated block-attestation
  and committee data, so a many-validator epoch fetches each block/committee once
  without trusting stale data across a later reorg.
- Collection completion is now timestamp-validated against the end of the final
  Deneb inclusion slot and receives NTP correction; an early marker can no longer
  unlock absence-based RCA.
- Host I/O evidence now identifies the actual Linux PSI `some avg10` signal instead
  of mislabelling it as CPU iowait or claiming a single sample was sustained. CPU
  steal sampling rejects counter resets and no longer double-counts guest time.
- Removed the unused `subnet_peer_min` threshold and `subnet_id` observation
  attribute. No collector or rule consumed them, so accepting the setting falsely
  implied it could change a verdict.
- Beacon duty responses are bounded and validated against the request, epoch, and
  uniqueness constraints before `watch` can spawn tracking goroutines.
- Beacon REST concurrency is capped at four 16 MiB responses to stay within the
  container memory ceiling; upstream error bodies are quoted before reaching logs
  or terminals.
- R-300 no longer upgrades host-wide PSI correlation into the high-confidence
  `local.el_slow.disk_saturation` sub-cause; direct EL/device evidence is required.
- Clock-trust evaluation sorts normalized metric keys before selecting evidence, so
  identical timelines cannot produce different verdict text through Go map order.
- Clock offset and freshness absolute values saturate safely at `MaxInt64`; a
  malicious minimum-duration value can no longer overflow into trusted timing.
- Clock freshness stamping and `doctor` use the same saturating absolute-value
  rule, and `doctor` now opens existing stores through the production schema path
  instead of accepting any writable file as a valid database.
- Host fallback evidence now retains the exact metric timestamp and source;
  external network-baseline observations no longer require a local NTP stamp
  because their percentile attributes, not their local arrival time, drive RCA.
- Host PSI tests no longer mutate package-global production paths; fixed `/proc`
  paths are constants and fixture paths are injected directly into the internal
  reader.
- CPU-steal tests likewise inject fixture paths into the internal reader instead
  of mutating the production `/proc/stat` path.
- Prometheus collection now owns one bounded scraper per `watch` process (and
  devnet scenario) instead of using a package-global HTTP client singleton;
  connection reuse is preserved through explicit construction and wiring.
- The host-memory corpus fault now applies a moderate `memory.high` cap to the
  Lighthouse consensus client on the validation path and uses an independent
  Prysm timing baseline. A bounded devnet-only file-cache allocator in the same
  cgroup sustains the PSI signal; cleanup stops the helper before restoring the
  exact original limit.
- P2P corpus netem now filters packets by the independent peer's container IP.
  Beacon API and Prometheus observation traffic stays unfaulted, so a delayed
  gossip path cannot hide its own exact-slot timing evidence behind HTTP timeouts.
  Peer-count corroboration is sampled at duty start while the fault is active,
  rather than after rollback during the later inclusion wait. The harness detects
  the consensus client from its version through the production registry instead of
  branching on a client substring in the Kurtosis service name (I-11).
- The systemd unit now consumes whymiss's native `WHYMISS_*` environment
  configuration instead of interpolating optional empty values into `ExecStart`;
  an unset metrics or baseline endpoint can no longer shift the next flag into
  the missing value position and prevent the service from starting.

- Attestation collection no longer stops at a fixed 32-slot delay. EIP-7045 permits
  inclusion through the following epoch (up to delay 63), and timely target no longer
  has the removed Altair delay bound. Timely head now also requires the matching
  target that the consensus specification makes a prerequisite.
- Clock stamping is idempotent, preventing REST-fallback head timestamps from being
  corrected twice before block-timing validation.
- Engine evidence now records the exact per-method call count and total duration
  between canonical heads. Valid clients that issue multiple
  `forkchoiceUpdated` calls in one slot no longer lose all EL/CL attribution.
- Timeline completion must exactly match one `collection_completed` observation;
  duplicate or evidence-free completion claims are rejected.
- Observations reject impossible source/kind attribution and unbounded attribute
  values; samples and RCA thresholds reject NaN and infinity.
- R-200 now requires a timely network baseline before blaming local peering. A late
  local block without that comparison returns `unknown.insufficient_data`, even when
  peer count is low, because the whole network may have received the block late.
- R-200 also requires an actual low peer-count sample. Local-vs-network timing alone
  proves only that propagation was delayed locally, not that insufficient peering
  caused it.
- R-500 no longer upgrades `network.inclusion_failure` to high confidence merely
  because an unrelated reorg occurred in the same slot window.
- A duty that completed cleanly (`outcome: ok`, every reward flag earned) was
  reported as `unknown.no_rule_matched` — the catch-all rule, whose remediation
  text asks the operator to file an issue. Every healthy duty looked like a bug.
  `rca.Analyze` now returns a causeless, high-confidence verdict for that case,
  and `domain.Verdict` accepts an empty cause on `ok`, matching how `no_duty`
  already worked.

  The check runs after the rule loop rather than before it. No rule inspects
  `Outcome`, so a rule can legitimately match on a duty that still ended `ok` —
  a validator client that ran slow but beat the deadline should still report
  `local.vc_slow`. Only the catch-all is reinterpreted.

- Markdown reports render `healthy` as the headline for a causeless verdict
  instead of an empty one. Prometheus already mapped an empty cause to `none`,
  so healthy duties scrape as `{cause="none",outcome="ok"}` with no change to
  the documented cardinality bound.

## [0.1.0] - 2026-08-22

First release.

### Added

**Domain model.** `Observation`, `Timeline`, `Verdict`, `Duty`, `RewardFlags`,
and a closed cause taxonomy (`docs/causes.md`), versioned as a public contract:
cause IDs appear in Prometheus labels and machine-readable output, so they never
change meaning silently. Every verdict embeds the engine and taxonomy versions
that produced it, and construction fails without evidence.

**Beacon API adapter** (`internal/source/beaconapi`). REST polling and SSE
streaming against the standard Beacon API: genesis, attester and proposer duties,
block arrival, attestation publication, and on-chain inclusion. Every call is
rate-limited with exponential backoff and jitter — the tool must never degrade the
node it watches. Tests run against real captured responses in `testdata/`, not
hand-written mocks.

**Metrics and host adapters.** `internal/source/promscrape` scrapes Engine API
call durations from an execution client and peer counts from a consensus client,
normalising across client-specific metric names. `internal/source/hostmetrics`
reads disk and memory pressure from `/proc/pressure/*` and CPU steal from
`/proc/stat`. `internal/source/registry.go` maps a node's version string to a
known client; client-specific code lives only in this package tree.

**Clock discipline** (`internal/clock`). NTP offset measured over SNTP at sample
time. When the offset is unmeasurable or exceeds the configured threshold,
timing-derived rules are suppressed rather than producing a verdict on an
untrusted clock.

**Storage** (`internal/store`). SQLite via `modernc.org/sqlite`, keeping the
binary CGO-free. Rolling retention with both an age and a byte cap, deleting
oldest-first, so disk use stays bounded on a small machine.

**Timeline assembly** (`internal/timeline`). Collects observations into a
per-slot `Timeline` and replays a recorded observation stream deterministically.

**RCA engine** (`internal/rca`). `Analyze(Timeline, Config) Verdict` — pure: no
I/O, no clock reads, no randomness, no goroutines, so the same input always
produces byte-identical output. Thirteen rules run in a fixed, documented order,
first match wins, from data-completeness and clock-trust guards through network
attribution (proposer missed, late block, inclusion failure), local layers (p2p,
execution client, consensus client, validator client), a host fallback, and an
unconditional catch-all. Rules prefer `unknown` over a guess: a wrong confident
verdict is treated as worse than no verdict.

**Reporting** (`internal/report`). Markdown post-mortems readable pasted into an
incident thread, and JSON matching the verdict's own field names.

**CLI** (`cmd/whymiss`). `whymiss <slot>` explains a slot, `whymiss watch` runs
the collector daemon, `whymiss timeline <slot>` prints raw recorded facts with no
interpretation.

**Prometheus exporter** (`internal/exporter`). One counter,
`whymiss_duty_verdicts_total{cause,outcome}`, so alerts fire on causes rather
than symptoms. Cardinality is bounded by construction — both labels come from
closed sets, giving at most 76 series regardless of validator count or uptime.
`whymiss watch --validator-index N --metrics-addr :9101` tracks attester duties
per epoch and records a verdict for each completed duty.

**Grafana dashboard** (`deploy/grafana/`). Missed and degraded duties by cause
over time, an outcome breakdown, a cause bar gauge, and a last-hour stat panel.
Binds to whatever Prometheus datasource it is provisioned against.

**Deployment** (`deploy/`). A distroless, non-root, multi-arch container image
with no shell; a Docker Compose stack running whymiss, Prometheus, and Grafana,
each with a read-only root filesystem, dropped capabilities, and memory caps; and
a systemd unit using `DynamicUser` with full sandboxing, so there is no service
account to create or manage.

**Test corpus and evaluation.** Nine labelled failure scenarios in `test/corpus`,
each generated by injecting a real fault into a live Kurtosis devnet and
recording what the beacon API actually reported — never synthesized.
`tools/faultinjector` reproduces them, `tools/corpusctl` validates their format,
and `tools/eval` measures accuracy into `docs/evaluation.md`: currently 9/9 top-1
and zero wrong verdicts reported at high confidence.

**Documentation.** `docs/causes.md` (the taxonomy contract), `architecture.md`,
`configuration.md`, `runbook.md`, `threat-model.md`, `evaluation.md`, and
architecture decision records under `docs/adr/`.

**Release pipeline.** GoReleaser builds static `linux/amd64` and `linux/arm64`
binaries; syft generates an SBOM per archive; cosign signs the checksum file
keyless via GitHub OIDC, so there is no long-lived signing key to protect. CI
enforces formatting, linting, the purity and isolation boundaries, race-enabled
tests, vulnerability scanning, and cross-compilation, plus a job that follows
the README's install instructions from a clean checkout.

### Known limitations

- Automatic duty tracking covers attester duties only; proposer duties are not
  yet wired into the watch loop.
- Accuracy is measured against nine scenarios covering six causes, generated on a
  two-node Lighthouse/Prysm devnet — not yet validated against mainnet incidents
  or other client pairings.
- Host-level causes require whymiss to run on the affected Linux host with `/proc`
  sampling enabled; without those local samples they fall back to lower confidence
  rather than being inferred from Beacon API data.
- Several causes in the taxonomy (`local.el_slow`, `network.late_block`,
  `network.inclusion_failure`, `local.host.cpu_steal`) have rules but no corpus
  scenario yet: reproducing them needs either hypervisor-level contention or a
  larger network than a two-node devnet.
- The slot schedule is configurable, but this build has no post-ePBS/PTC rules yet.
- No long-duration soak test has been run against a public testnet.
