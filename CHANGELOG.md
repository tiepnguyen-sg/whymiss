# Changelog

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). The
version stays at `v0.x` until the API is stable.

## [Unreleased]

### Added

- **The Phase 2 soak was restarted from zero; the first 9h11m measured stale
  code and is not evidence.** The run started 2026-08-25T07:47:11Z was built at
  07:42Z, before the five collection fixes committed the same day at 16:27Z
  (5a0d82c, 466e5a6, e411835, 685b3af, d56ac76). Its own log proves the gap
  rather than merely implying it: repeated `check inclusion: fetch committee
  lengths for duty slot 3788577: GET /eth/v1/beacon/states/head/committees:
  unexpected status 400 "BAD_REQUEST: 3788577 is not in epoch 118394"` is
  exactly the failure 5a0d82c fixed by reading committee lengths from the duty
  slot's own state instead of `head`. A soak that reproduces an already-fixed
  bug measures code that is not being released, so its numbers cannot sign off
  the gate no matter how healthy they look — and they did look healthy: RSS flat
  at ~28 MiB against the 256 MiB ceiling for 9 hours, database plus WAL/SHM
  peaking near 4.5 MB against the 100 MB cap and visibly falling back after
  checkpoints. Both discarded runs are kept on the soak host under
  `discarded-soak-runs/` with a `WHY_DISCARDED.md` recording all of this,
  because what they measured is a finding even though it is not evidence. The
  first replacement, started 17:02:56Z from binary sha256 `75c2ffd6…`, confirmed
  the fix — zero occurrences of `states/head/committees` in its log against many
  in the run above — and was itself stopped after 41 minutes and archived once
  `internal/app/headfanout.go` landed and changed how duty tracking publishes a
  REST-polled head. Preferring 41 sunk minutes over a binary that matches the
  code being released would be the exact reasoning this entry exists to reject.
  **The run that counts** started 2026-08-25T17:43:59Z from sha256
  `d12bb358c17b92e7e723491573df92ceaa1737b528150d9c996348691eb65af0` and is due
  to complete 2026-08-28T17:43Z. Recording the binary hash alongside a soak
  summary is now called for in `docs/runbook.md`: the version string alone said
  `-dirty` for all three runs and would not have distinguished any of them.

  The 41-minute run also produced the first healthy verdict this deployment shape
  has ever recorded — slot 3788818, `outcome=ok`, empty cause, `high` — so the
  earlier note that a gateway-only soak yields nothing but
  `unknown.insufficient_data` no longer holds with the collection fixes in place:
  a duty that is genuinely fine is now reported as fine.

- `test/soak/run.sh` accepts `METRICS_ADDR` and passes it to `--metrics-addr`.
  The exporter owns the only long-lived HTTP listener in the daemon, and a
  72-hour soak that never starts it leaves a leak there unmeasured; the
  replacement run has it bound to `127.0.0.1:9101`. Loopback deliberately — the
  endpoint is unauthenticated by design and a soak host may have a public
  interface. `docs/runbook.md` also now states plainly that a soak measures only
  the collectors it was given: against a gateway that answers `/eth/v1/events`
  with `501` the SSE path is never exercised, and without `CL_METRICS_API` and
  `BASELINE_*` neither block timing nor the network baseline is collected at
  all.

- Started the Phase 2 72-hour soak test (`make test.soak`) against Hoodi
  testnet: a dedicated `e2-small` host, a public Hoodi beacon API
  (`ethereum-hoodi-beacon-api.publicnode.com`, no operator infrastructure
  required — read-only, no validator keys, matching I-1/I-2), and genesis
  validator index 0 (any active Hoodi validator works; watching one requires
  no ownership of it, only its public duty/attestation data, which is the
  whole point of a read-only observability tool). That gateway does not
  support the SSE `/eth/v1/events` endpoint (`unexpected status 501`) —
  `Stream`'s reconnect loop backs off correctly (full jitter, 1s base/30s
  cap, verified against `internal/source/beaconapi/backoff.go`) and settles
  into a low, safe retry rate rather than hammering it, which is itself a
  72-hour stress case for that loop's own resource behavior. Duty tracking
  does not depend on the event stream — `BlockSeen`, `AttestationPublished`,
  and `CheckInclusion` all poll REST endpoints directly — so this still
  exercises the real collection and RCA path end to end.

- `cl-slow-cpu-lighthouse`'s and `cl-slow-cpu`'s `cgroup_cpu` fault (9%/7%
  quota) throttles the same consensus-client container whose own REST API
  `faultinjector`'s live head-timing observer polls during the fault window —
  a known tight margin (`NewObserver`'s 25s HTTP client timeout was already
  raised specifically because 5-9% quota routinely pushed that node's API
  close to timing out; see its doc comment). Running `make
  corpus.generate.campaign` repeated both recipes back-to-back against the
  same target node several times in a row and lost that race on every repeat
  in this pass (`head observation unavailable: ... timeout awaiting response
  headers`), each time falling back to `unknown.*` instead of `local.cl_slow`.
  Initially misattributed to CPU contention from a second Kurtosis enclave
  running fault-injection work concurrently on the same 4-vCPU host (real,
  and worth avoiding — generate one enclave's fault-injection work at a time
  per host — but tearing that second enclave down and confirming host load
  back under 2 did not stop the same failure from recurring on solo runs,
  which is what narrowed it to the shared-node race instead). Not fixed in
  this pass: doing so without weakening the fault means observing head timing
  from a channel that does not share the target's own throttled API — e.g.
  the SSE stream instead of REST polling — which is a real code change, not
  a config tweak.
- Corpus grew to 15 canonical scenarios (was 9): `cl-slow-cpu-lighthouse`,
  `p2p-ambiguous-no-baseline(-prysm)`, `proposer-missed-concurrent-vc-pause(-prysm)`,
  `vc-frozen-prysm`, and `vc-slow-cpu-prysm` are new — then back down to 13 when
  two of them turned out to be mislabelled and were dropped (see "Corpus: two
  mislabelled scenarios removed" below). `cl-slow-cpu`, `p2p-degraded-lighthouse`, and
  `p2p-degraded-prysm` were regenerated with the current `faultinjector` build after
  their originals (recorded earlier in this corpus-growth effort, before later
  faultinjector fixes) turned out to fail `corpusctl validate` (missing clock
  `round_trip`) and, for the first two, `TestGolden_Corpus` (the clock sample
  attached to their `attestation_included` observation was 10–12 minutes older than
  the observation itself, past R-011's 2-minute freshness limit) — regenerating
  against current code fixed both.
- `internal/timeline/replay_test.go`'s `TestLoadObservations_RealCorpusScenario` and
  `TestReplay_RealCorpusScenario` hardcode exact values (observation count, slot,
  validator index, inclusion delay) against `test/corpus/vc-slow-cpu`'s live content
  rather than a frozen fixture, so regenerating that scenario as part of this
  corpus-growth effort silently broke both assertions until they were updated to
  match. Worth a follow-up: a test asserting against a scenario expected to be
  regenerated should pin its own copy, not read the corpus scenario directly.
- `README.md`'s sample report replayed `test/corpus/host-memory-pressure`, which
  this pass could not get to reproduce `local.host.memory_pressure` (see above) —
  the README was silently making a false claim about reproducible output. Replaced
  with the real, current `local.vc_slow` report from `test/corpus/vc-slow-cpu`.
  That scenario has since been dropped from the corpus entirely.

- **Corpus: two mislabelled scenarios removed, accuracy figure now describes less.**
  `test/corpus/host-memory-pressure` and `test/corpus/vc-slow-cpu-prysm` were the
  two false-high verdicts in `docs/evaluation.md`. The engine was right and the
  labels were wrong. Both recordings show a duty that was fulfilled with every
  reward flag earned — `host-memory-pressure` published its attestation at +3.166s,
  inside the ~4s deadline, and `vc-slow-cpu-prysm` contained no
  `attestation_published` observation at all, only an `attestation_included` with
  `inclusion_delay` 1 and correct head and target. `deriveOutcome` therefore
  returned `ok`, R-600 correctly declined (`dutyHasObservableLoss` is false: PSI
  was 45.41% but nothing was lost), R-410 correctly declined (no publication
  timestamp to measure), and the R-999 catch-all produced its documented
  no-cause/`high` clean pass per `docs/causes.md` §6. Each recipe's own bisection
  log had already recorded the exact run that was committed as "healthy"; the
  `expect:` block was left at the phenomenon the recipe was aiming for rather than
  the one it produced. No rule was changed. `observations_sha256` matched the
  recorded bytes in both, so nothing had been hand-edited — the defect was purely
  in the label. Both recipes stay under `tools/faultinjector/scenarios/` with their
  full bisection logs, now headed with why the record was dropped and how to
  resume; both are removed from `CORPUS_SCENARIOS` and `CORPUS_CAMPAIGN` so no
  batch run re-creates a record whose label the fault has never once earned.
  `docs/evaluation.md` reads **13/13 (100.0%) top-1 accuracy, 0 false-high**, which
  is a *smaller claim* than 13/15, not a better one: the corpus lost its two hardest
  cases and two of the eight causes it used to name, and the campaign that was
  planned to reach the 50-scenario release minimum now tops out at 40 (13 canonical
  + 27), a 10-record shortfall left visible in the `Makefile` rather than padded out
  with more rounds of the recipes that already work. `tools/eval` now prints corpus
  size against that minimum, the number of causes exercised, and a note that
  removing an unreproducible scenario raises the percentage without improving the
  engine — so the report cannot be read as "finished" again.
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

### Known issues

- **The network baseline still misses roughly a third of slots on a healthy
  node, because one catch-up window serves two different jobs.** Measured over a
  15-minute live run: 15 warnings against 38 head updates, of which 11 were
  `block timing gauge remained at slot N while waiting for slot N`.
  `blockTimingCatchupWindow` is 3 s and is shared between the watched node and
  the baseline node, which have opposite expectations — the watched node has
  already emitted the head event being processed, so its gauge should be
  current, while an independent baseline node is routinely a slot behind at the
  instant of the scrape. A rule needing `tl.Network` has nothing to compare
  against on those slots. This is I-8-safe: a missing sample degrades to
  `unknown` rather than fabricating one, so it costs coverage, not correctness.
  Giving the baseline its own window wants a number chosen against measured lag
  rather than guessed, which is why it is recorded here instead of adjusted.

- **`--baseline-beacon-api` cannot be used without `--baseline-metrics-api`,
  which makes the product's core question need a node you scrape rather than an
  API you can reach.** The baseline's arrival time comes only from the second
  node's Prometheus endpoint, so an operator who can reach a friend's or a
  provider's Beacon API still cannot answer "was it the network or me?".
  ADR-0022 records the way out: poll `/eth/v1/beacon/headers/{slot}` on the
  baseline node the way `BlockSeen` already does for the watched node, which is
  provably about that slot, client-agnostic, and needs no metrics endpoint — at
  ~500 ms resolution instead of milliseconds. Not taken here because it changes
  what a baseline observation measures and the resolution trade has to be judged
  against `thresholds.network_deviation`.

- **`local.el_slow` and the four `local.host.*` causes remain unmeasured**, along
  with `network.late_block`, `network.inclusion_failure`, and
  `unknown.no_rule_matched` — 8 of the 14 causes in `docs/causes.md` have no
  corpus scenario. Unmeasured is not passing. One shape now has direct evidence
  of a gap rather than merely no coverage: `p2p-degraded-prysm-r02`, left out of
  the corpus, records propagation 5.38 s alongside validation 4.77 s with no
  Engine samples, so neither stage dominates and no rule matches. The engine
  correctly returns `unknown.no_rule_matched`, which per `docs/causes.md` is a
  bug-report signal — a real two-stage failure the taxonomy cannot yet name. The
  record is kept out rather than labelled with the gap it exposes; it is in
  `../whymiss-campaign-evidence-20260826/records/`.

- `tools/faultinjector/scenarios/cl-slow-pause.yaml` is referenced by nothing —
  not `CORPUS_SCENARIOS`, not `CORPUS_CAMPAIGN`, no corpus record, no bisection
  log. It has never been run, so whether it reproduces its `local.cl_slow` label
  is unknown, and it is not a spare record waiting to be collected. Either wire
  it into a batch and give it a bisection log, or delete it — an unreferenced
  recipe reads as coverage that does not exist.

### Changed

- **The corpus is 33 scenarios, up from 13, and the eval report now says what
  that number does and does not cover.** The devnet host held 40 record
  directories from the campaign; six were empty (runs that failed before writing
  anything), two were the recordings the previous cycle dropped as mislabelled,
  and 11 were already committed, leaving 21 complete new records. All 21 pass
  `corpusctl validate`. Fourteen carried labels that did not describe what the run
  produced, and each was corrected from its own observations against
  `docs/causes.md` — not from what the engine outputs, which would be circular.
  Every corrected record and recipe carries the derivation in its description.

  - Four had a proven skip *plus* the duty's own `attestation_included` with
    source and target earned, so the only flag lost was `timely_head`, which is
    unearnable on a skipped slot: `network.proposer_missed` / `high`
    (`cl-slow-cpu-lighthouse-r03`, `cl-slow-cpu-r02`, `cl-slow-cpu-r03`,
    `p2p-ambiguous-no-baseline-r04`).
  - Ten had a proven skip and nothing at all from the local attestation path:
    `unknown.insufficient_data` / `low` per ADR-0021 (five
    `proposer-missed-concurrent-vc-pause*` campaign rounds, `vc-frozen-lighthouse-r02`,
    `vc-frozen-prysm-r02`, `vc-frozen-prysm-2-r02`, `vc-slow-cpu-r02`,
    `vc-slow-cpu-r04`).
  - One was left out entirely: `p2p-degraded-prysm-r02` (see Known issues).

  The two committed `proposer-missed-concurrent-vc-pause` records and their
  recipes were relabelled the same way, from `network.proposer_missed` / `high` to
  `unknown.insufficient_data` / `low`. Their recipe constructs the ambiguous case
  by design — it pauses the validator client owning both the attester and the
  proposer duty — so they become ambiguity coverage, which Phase 3's DoD asks for
  explicitly. The previously reported 13/13 with zero false-high verdicts rested
  in part on those two records asserting the old behaviour was correct.

  `docs/evaluation.md` now reads **33/33 (100.0%), 0 false-high** — and states, on
  the same screen, that **16 of the 33 expect `unknown.*`**. Nearly half the corpus
  asserts that whymiss correctly declines to attribute rather than that it named a
  cause, and top-1 accuracy cannot tell those apart, so `tools/eval` prints the
  split. Six of 14 causes are exercised. The `Makefile`'s campaign note is updated
  to the measured yield: 33 of 50, 17 short, and the remainder must come from the
  8 uncovered causes rather than more rounds of the recipes that already work.

- **The `corpus.generate.campaign` retry batch was killed mid-run; the devnet it was
  recording against had partitioned.** Eight retry records were queued
  (`cl-slow-cpu-r02/r03`, `cl-slow-cpu-lighthouse-r02/r03/r04`,
  `p2p-ambiguous-no-baseline-prysm-r02/r03/r04`); five ran and all five failed, zero
  OK. The cause was not the faults and not host load (VM load average 0.36 on 4
  vCPUs, every cgroup back at `max`, no leftover `netem` qdisc or `iptables` rule,
  no paused container): both consensus clients in enclave `whymiss-vm-run` were
  reporting `{"connected":"0","disconnected":"1"}` on
  `/eth/v1/node/peer_count` — they had disconnected from each other and were running
  as two independent forks. Prysm served a block for slot 12242 (proposer 41, its
  own validator set) where Lighthouse returned 404, and both nodes' last justified
  checkpoint was stuck at epoch 316 while their heads were near epoch 394. Measured
  empty-slot rate by sampled window: 0% at slot 8465, 1% at 10000, 58% at 11500,
  55% at 12200, 46.5% across slots 12403–12602. So the split began around slot
  10112 (epoch 316) and every record generated after it is contaminated: roughly
  half of all watched slots have no block from the recording node's point of view,
  which makes R-100 fire and mask whatever local cause the scenario was labelled
  for. All 13 committed corpus scenarios are at slots ≤ 8876, before the split, and
  are unaffected. The contaminated `-r02`/`-r03`/`-r04` record directories were left
  in place on the VM as evidence rather than deleted. Before the batch is re-run the
  devnet needs to be repaired and verified healthy — connected peers ≥ 1 on both
  clients and an empty-slot rate under a few percent over the last ~64 slots.

- **Settled, and not fixed: R-100 fired correctly on devnet record `cl-slow-cpu-r02`,
  but the investigation exposed a blind spot in what `docs/causes.md` accepts as
  proof that a slot was skipped.** The record returned `network.proposer_missed`
  (`high`) while the injected fault was a CPU cap on the operator's own consensus
  client, which looked like R-100 firing where the taxonomy forbids it ("does not
  apply to the operator's own proposer duty"). It was not. The watched duty was
  validator 32's *attester* duty, and slot 12240's proposer was validator **17** —
  in the unfaulted Lighthouse set, outside the recipe's
  `avoid_proposer_validators: [32, 63]` guard, which worked as designed.
  `ProposerMissed.Evaluate` also guards on `tl.Duty.Kind != domain.DutyAttester`, so
  the carve-out is enforced in code as well as in prose. The scenario simply failed
  to produce its labelled `local.cl_slow` phenomenon: slots 12240 and 12241 were
  both empty, which forced `inclusion_delay` to 2 and cost the duty its timely-head
  flag for reasons unrelated to the CPU cap.

  The blind spot is what made those slots look empty. R-100's required evidence is
  four facts from the configured Beacon API — fully synced, execution online and
  non-optimistic, head advanced past slot N, and a second canonical-header lookup
  for N returning 404. All four held on a node with **zero connected peers**, whose
  404 described only its own isolated fork. R-100 therefore answered "the network"
  at `high` confidence where the true answer was "you — your node is isolated",
  i.e. `local.p2p_degraded`; and because R-100 sits above R-200 in the declared
  ordering, peer evidence can never override it. Nothing was changed: a fix touches
  `internal/rca` and the taxonomy contract, so it needs plan mode, human approval,
  and probably an ADR.

  Uncertain, and what would settle it: how reachable this is in production. The
  Prysm node here kept advancing its head only because it held validators 32–63 of
  a 64-validator devnet. A solo staker's isolated node would stall, "head advanced
  past N" would fail, and R-100 would not fire — so this may be a devnet-shaped
  hole that only becomes production-shaped when a node holds enough of the
  validator set to keep its own fork moving. Settling it means replaying an
  isolated-node timeline from a node with a negligible validator share. Note also
  that the `cl-slow-cpu` recipe records no peer samples at all, so even a
  peer-aware R-100 would have had nothing to read in this record.

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

- **R-100 no longer exonerates an operator whose duty produced no attestation at
  all** (ADR-0021, taxonomy 2.0.0 → 3.0.0, engine 0.13.0 → 0.14.0). It used to
  fire on a proven `block_skipped` alone and report `network.proposer_missed` at
  `high` with no remediation, which on the timeline

  ```
  duty_assigned, slot_start, block_skipped, collection_completed
  ```

  told an operator whose validator client was paused outright — or capped to 0.1%
  of one core — that nothing was theirs to fix. ADR-0015 already said why that is
  wrong, in its own consequences: *"attesters may still publish and be included
  normally on a skipped slot"*, so a skip does not explain a missing attestation.
  No later rule caught it either, and R-400 is right to decline: it needs
  `block_seen` and `head_updated` before the deadline to establish the beacon node
  was healthy, and a skipped slot supplies neither. R-100 sits third in
  `rules.Order()`, ahead of R-400 and R-410, so it answered first and stopped the
  search.

  R-100 now splits three ways: `attestation_included` present → exonerate at
  `high`, citing the inclusion beside the skip, because the local path
  demonstrably worked; `attestation_published` but no inclusion → decline, since
  non-inclusion of an on-time attestation is R-500's question; neither → report
  `unknown.insufficient_data` at `low`, naming both facts and pointing at the
  validator client. The third case is I-8 applied literally: two readings fit the
  evidence equally and nothing in the timeline separates them.

- **A stale block-timing gauge is no longer written as a slot's measurement**
  (ADR-0022). Neither client publishes block arrival as a slot-labelled series,
  and `source.SampleBlockTimingForSlot` proves a sample's slot by polling
  `beacon_head_slot` — a *different* series from the value it returns. A node that
  advances its head without recording an arrival keeps returning an older slot's
  delay, and that check cannot tell. `blockSeenFromTiming` had always rejected a
  sample whose implied arrival fell after the head observation that triggered it;
  the baseline path, added later, checked only that propagation was non-negative,
  and that asymmetry is the entire defect. The baseline now applies the same
  bound: an arrival cannot postdate the read that reports it.

  Both halves were measured, not reasoned about. Before: 21 consecutive
  `network_baseline_sampled` observations over 15 minutes carrying the identical
  2233 ms, spanning slots 12967 to 13038, while direct probing showed Prysm's
  `block_arrival_latency_milliseconds_gauge` frozen as `beacon_head_slot` advanced
  13049 → 13051. The cause was the node, not the metric name: both clients'
  gossip-block counters sat frozen (`beacon_processor_gossip_block_imported_total`
  at 5021, `block_arrival_latency_milliseconds_count` at 5131) while
  `beacon_block_processing_requests_total` kept climbing — a devnet worn down by
  days of fault injection was importing blocks off the gossip path the metric
  tracks. After, on that same node: five `reject network baseline timing` warnings
  and **zero** baseline observations written. Honestly absent beats plausibly
  wrong.

- The `headFanout` fix above was confirmed against real nodes, not only in test:
  a 15-minute `whymiss watch` against the devnet enclave, watching Lighthouse
  with Prysm configured as the independent baseline, recorded 21
  `network_baseline_sampled` observations alongside 38 `head_updated`, 34
  `block_seen`, 26 `engine_call`, and three end-to-end verdicts. Before the fix
  the baseline collector could only ever be fed by the SSE stream. Full run
  detail in `../whymiss-campaign-evidence-20260826/DEVNET-VERIFICATION.md`,
  including two collection defects that run turned up (see Known issues).

- **The network baseline was collected only on nodes that serve an event
  stream.** Block timing had already gained a REST fallback — duty tracking's
  own `HeadUpdated` poll fed `timingJobs` — but the baseline channel was fed
  from the SSE `events` loop and nowhere else. On a Beacon API that does not
  serve `/eth/v1/events` (the public Hoodi gateway used by the soak answers
  `501`, so this is a real deployment, not a hypothetical) no
  `network_baseline_sampled` observation was ever written, `tl.Network` stayed
  nil, R-110 and R-200 always declined, and every "was it the network or me?"
  question fell through to `unknown.insufficient_data`. This is the second half
  of the defect fixed last cycle, where the daemon never produced the
  observation at all: the collector existed, the fallback reached one consumer
  of two.

  Both discovery paths now publish through one `headFanout` (`internal/app/headfanout.go`)
  instead of each call site forwarding to the channels it happens to know about,
  which is what let the two drift apart. Drop-on-full and the nil-channel cases
  are unchanged in behaviour and now stated once: a full queue drops the sample
  rather than queueing it (I-12) or blocking the loop that feeds it (I-5), and a
  dropped sample degrades a rule to unknown, which I-8 prefers to stale
  evidence. `TestTrackDuty_RESTHeadReachesEveryCollector` is the regression
  guard and was confirmed to fail against the pre-fix wiring — "baseline
  collector never received the REST-polled head" — before the fix was restored.

- `Client.BlockSeen` (`internal/source/beaconapi/blocks.go`) had two
  instances of the same bug class, one of them observed live: (1) a single
  failed poll of `/eth/v1/beacon/headers/{slot}` aborted the whole call
  instead of being tolerated like "not found yet", same fix as HeadUpdated
  and AttestationPublished above; (2) `blockStatusAtDeadline` sampled the
  node's sync status exactly once at the deadline and failed permanently if
  it wasn't ready that instant — the exact bug `tools/faultinjector`'s own
  `blockStatusAtDeadline` had before an earlier fix in this same file's
  history, except that fix was only ever applied to the faultinjector's copy,
  never to this production one. Confirmed live: the Phase 2 72-hour soak test
  against the real public Hoodi gateway hit `"cannot confirm slot 3788193 as
  seen or skipped: node is not fully synced..."` while the node was simply
  lagging, not stuck — losing that slot's evidence for no real reason.
  `blockStatusAtDeadline` now retries for up to 90s (`defaultBlockRecoveryBudget`)
  before giving up, mirroring the faultinjector fix. Regression tests added
  for both.
- `Client.AttestationPublished` (`internal/source/beaconapi/attestations.go`)
  had the same bug just fixed in `Client.HeadUpdated`: a single failed poll of
  `/eth/v1/beacon/pool/attestations` aborted the whole call instead of being
  tolerated like "not found yet". Found by code inspection while
  investigating why `vc-slow-cpu-prysm`'s own cgroup_cpu bisection against
  Prysm kept landing exactly on `local.vc_disconnected` or healthy with no
  clean `local.vc_slow` in between — a node under that fault can answer one
  pool poll too slowly without being unable to answer the next one 500ms
  later, and losing that single poll made a real late publish look
  indistinguishable from the validator never attesting at all. This is
  `internal/app/duty_tracking.go`'s real collection path, not corpus-only
  code: `collectionFailed` already keeps a transient error like this from
  producing a wrong confident verdict (it suppresses `collection_completed`,
  so `R-400`'s guard on that observation still holds), but it did cost a real
  operator's node the evidence needed to distinguish `local.vc_slow` from
  `unknown` under exactly the load condition that rule exists to diagnose.
  `tools/faultinjector/observe.go`'s own `PollAttestationPublished` had the
  identical bug and is fixed the same way — it does not silently produce a
  wrong verdict either (`main.go` hard-fails the whole scenario run on this
  error instead), but every such failure during corpus generation cost a full
  wasted record-generation attempt. Regression tests added in both packages.
- `docs/configuration.md`'s "Slot schedule" table documented
  `schedule.seconds_per_slot`'s constraint as just "Positive", omitting that
  `SlotSchedule.Validate()` also caps it at `maxSupportedSlotDuration` (1
  minute) — found while auditing every configuration table against its
  validator function for release readiness.
- `Client.HeadUpdated` gave up entirely on a single failed poll of
  `/eth/v1/beacon/headers/head`, discarding every remaining chance to observe
  the head before its deadline — even though the same loop already tolerates
  "not found yet" the same way, every poll cycle, until that deadline. A node
  under real CPU pressure (`local.cl_slow`'s own fault, or a genuinely
  overloaded operator box) can answer one request too slowly
  ("timeout awaiting response headers") without being unable to answer the
  next one 200ms later; the loop now retries through any fetch error the same
  way it already retries through "not found", unless ctx itself is why the
  fetch failed. This is `internal/source/beaconapi`, not corpus-only code — it
  is what `whymiss watch` uses in production, so a transient blip on a real
  operator's node no longer permanently loses that duty's `head_updated`
  evidence. Found because this exact pattern reliably failed 5 of 27 records
  in a corpus generation campaign (`cl-slow-cpu`/`cl-slow-cpu-lighthouse`,
  whose fault throttles the very node this poll depends on).
- `SamplePrysmPeerCount` treated a Prysm node reporting zero connected peers
  as a scrape failure (`connected_libp2p_peers not found in metrics`) instead
  of a valid zero reading. `connected_libp2p_peers` is a per-agent labelled
  Prometheus vector, unlike Lighthouse's bare, always-registered
  `libp2p_peers` gauge — a Prometheus client only exposes a label combination
  once it has actually occurred, so a node with no currently connected peers
  omits the series entirely rather than reporting it at 0. This devnet's only
  real capture (`testdata/prysm_metrics.txt`) happened to have one peer
  connected and never exercised the zero case; found because
  `p2p-ambiguous-no-baseline-prysm`'s netem peer-isolation fault reliably
  reaches exactly that state, failing 3 for 3 campaign records with this
  error. No matching line after an otherwise successful, well-formed scrape
  is now treated as the legitimate zero it is.
- `store.Open` failed on any relative database path with a directory
  component — `open results/whymiss.db: connect: SQL logic error: invalid
  uri authority: results` — because `url.URL{Scheme: "file", Path: p}` with
  a relative, Host-less `p` serializes as `file://p`: the RFC 3986 generic
  syntax reads everything up to the first `/` after `//` as the URI's
  authority, not the path. A flat filename like `whymiss.db` happened to
  still open (some SQLite URI parsers tolerate an authority they don't use),
  which hid this for a long time; found running the Phase 2 soak test with
  `--db soak-results/<timestamp>/whymiss.db`. The path is now resolved to
  absolute before building the URI, producing the unambiguous
  `file:///abs/path` form.
- `whymiss doctor` and every `FetchGenesis` caller (`watch`, the Phase 2 soak
  test) failed against a real beacon node — `fetch spec: decode data field:
  json: cannot unmarshal array into Go value of type string` — because
  `GET /eth/v1/config/spec`'s response mixes plain string fields with
  non-string ones (`BLOB_SCHEDULE`, an array of `{EPOCH,
  MAX_BLOBS_PER_BLOCK}` objects added for a later fork) and the client
  decoded the whole object into a `map[string]string`. Never caught before
  because this project's Kurtosis devnet genesis predates that field; found
  running `whymiss doctor` against a real public Hoodi testnet beacon API
  while preparing the 72-hour Phase 2 soak test. Now decodes only the one
  field this package reads (`SECONDS_PER_SLOT`) into a typed struct, so
  `encoding/json` ignores whatever shape the rest of the spec takes.
- `tools/faultinjector`'s `cgroup_cpu` fault could never throttle a validator
  client tight enough to make BLS signing genuinely late: a validator client's
  signing work is only a few ms of real CPU, so even the tightest integer
  `quota_percent: 1` (1ms per 100ms cgroup v2 period) only added a few hundred
  ms of wall-clock delay — nowhere near the ~4s attestation deadline
  `local.vc_slow` needs to cross, so the duty kept finishing healthy. Fractional
  percentages below 1 are now accepted; requesting one below the ~1ms quota this
  host's kernel accepts (`write cpu.max: invalid argument` below that) widens
  the cgroup period instead of asking for a smaller quota, so the same ratio is
  still achieved without hitting that floor. `vc-slow-cpu` (Lighthouse) needed
  0.1%. `vc-slow-cpu-prysm` (Prysm) needed the opposite direction — its VC
  needs meaningfully more absolute CPU to complete a signing cycle at all, so
  1–4.5% left it never publishing within the window (`local.vc_disconnected`)
  while 4.75%+ finished with room to spare (healthy); the true threshold sits
  in that final quarter-point gap and was not found in this pass — see the
  scenario's own description for the full bisection log.
- Every `faultinjector` fault kind (pause, netem, cgroup_mem, cgroup_cpu,
  cgroup_io, clock_skew) failed with "matched N containers, want exactly 1"
  the moment a second Kurtosis enclave was running alongside the one being
  faulted, because `dockerContainerID` resolved a target service name via a
  bare `docker ps --filter name=` match with no enclave scoping — every
  ethereum-package devnet uses the same service names
  (`cl-1-lighthouse-geth`, `vc-2-geth-prysm`, ...), so two enclaves each have
  a same-named container and the filter matched both. `Apply` already
  received an `enclave` parameter for exactly this purpose but never
  forwarded it. `dockerContainerID` now additionally filters on the
  `kurtosis_enclave_name` container label when `enclave` is non-empty; empty
  keeps the original unscoped behavior for the handful of integration tests
  that have no enclave name to give it. Found by actually running two
  devnets in parallel to speed up corpus regeneration — the failure was
  fail-closed, not fail-dangerous: no fault was ever applied to the wrong
  enclave's container, generation just refused to proceed.
- `docs/evaluation.md` was pinned at a stale 9/9 (100%) claim from before Taxonomy
  2.0 and the corpus regeneration effort — it no longer reflected any run of
  `make eval` against the current corpus. Regenerated for real: current corpus
  measures 3/10 (30%) top-1 accuracy with 2 false-high verdicts, both release
  blockers per BUILD_PROMPT.md §11.3, tracked by the corpus regeneration work
  already in progress rather than papered over here.
- `make test.golden` invoked `go test ./internal/rca/... -update`, but no test
  in `internal/rca` declares an `-update` flag — `TestGolden_Corpus` compares
  live analysis against each scenario's `manifest.yaml` directly and has no
  snapshot file to regenerate. The command failed at flag parsing before
  reaching any golden logic. Retargeted to run `TestGolden_Corpus` itself.

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
