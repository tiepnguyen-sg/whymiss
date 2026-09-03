# Changelog

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). The
version stays at `v0.x` until the API is stable.

## [Unreleased]

## [0.3.0] - 2026-09-02

Signed off by a 72-hour soak that finished 2026-09-02T10:32:57Z with
`result=PASS`: peak RSS **34076 KiB** against the 262144 KiB ceiling (13.0%), a
database peaking at **25361656 bytes** against the 104857600 byte cap (24.2%),
and 673 verdicts over 4321 one-minute samples. All 55 error lines belong to the
public gateway it ran against, none to whymiss, and no `context canceled` appears
at shutdown any more. Evidence and the commands to recompute every figure are in
[`test/soak/evidence/20260830T103211Z/`](test/soak/evidence/20260830T103211Z/README.md).

The binary that ran it is machine-code-identical to this tag: rebuilding it with
its own version string differs in 190 bytes across five regions, all build
metadata, and `git diff` over `cmd/` and `internal/` excluding tests is empty.

### Added

- **The corpus can hold records of conditions nobody injected, reported
  separately.** `manifest.yaml` gains `origin: injected | observed` — empty means
  injected, so every existing record keeps its meaning — and `corpusctl` requires
  fault fields only for injected records while refusing an observed record that
  claims a fault it did not inject. `corpusctl export --db --slot --out` writes a
  record from a running collector's own store. Manifests may also now carry the
  `schedule` a record was collected under, because a post-ePBS record's verdict
  depends on deadlines mainnet does not have.

  **`make eval` reports the two classes separately and never folds them
  together.** An injected record's label comes from what the harness did — cap the
  execution client, expect `local.el_slow` — and is independent of anything whymiss
  observed, which is what makes it a test of attribution. An observed record's
  label and the rule under test read the same on-chain fact, so it tests the
  collection path and the rule's gates instead. Both are worth having; conflating
  them would have quietly changed what the headline accuracy figure means.

- **`network.payload_late`: whymiss can name a builder's failure under ePBS
  (ADR-0027).** A new cause, a new observation kind `payload_attested`, and rule
  R-120. Under EIP-7732 the payload-timeliness committee votes on whether a
  slot's execution payload arrived in time, and that vote is carried in the next
  block's `payload_attestations` — a **standardised Beacon API field**, so this
  needs no client-specific adapter, the same conclusion ADR-0023 reached for peer
  count and ADR-0025 for the baseline. An earlier reading assumed the timing had
  to come from a client gauge and would have meant an adapter per client.

  Validated against the live public Glamsterdam network: the daemon adopted the
  post-ePBS schedule from the node's own spec, tracked duties, and recorded
  `payload_attested {payload_present: true, ptc_votes: 2}` from real blocks.

  **Running it live also caught a defect the unit tests did not.** R-120 first
  fired on the committee's vote alone. On that network 32 of 51 votes reported
  the payload absent while attestations were still being included on time, so
  every healthy duty would have been handed a cause. The rule is now gated on
  `dutyHasObservableLoss`, with a test for it. A verdict for a duty that lost
  nothing is worse than no verdict.

  **No sub-causes, deliberately.** ADR-0027 sketched `revealed_late` and
  `never_revealed`; `payload_present` cannot tell them apart, and claiming the
  distinction would be the confident guess I-8 exists to prevent.

  **The cause has no corpus scenario yet, so it is unmeasured** — which this
  project does not treat as passing. Chasing that record produced two findings
  worth more than the record would have been.

  First, **no local ePBS devnet can produce it.** Four `ethereum-package`
  configurations were tried and all degrade post-Gloas; the fourth showed why —
  seven Gloas blocks carried two payload attestations between them, where the
  public network carries about one per block. The committee barely votes locally,
  so the fault cannot be injected here. `tools/faultinjector/scenarios/payload-late.yaml`
  is committed against a devnet that cannot yet run it.

  Second, and more useful: **the duty this cause befalls is not one whymiss
  watches.** Watching 64 validators on the public Glamsterdam network for an hour
  produced 52 duties, every one clean, while the committee was reporting the
  payload absent on roughly half the slots it voted on. That is what ePBS is for —
  the consensus block and the payload are decoupled, so the attester votes on a
  block that arrived on time regardless. The duty a late payload costs is the
  **proposer's**, and proposer duty tracking is a long-standing known issue. The
  obstacle is not the corpus format and not network access; it is the duty kind.

- **A case study reproduces a public incident's root cause with whymiss's own
  output.** `docs/case-studies/2023-05-mainnet-finality.md` takes the May 2023
  mainnet finality incidents — finality lost twice in 24 hours, participation down
  to ~75% at epoch 411,448 — and reproduces the **first link** of their published
  chain: an execution client that stops responding. Steps three through five are a
  consensus-client cache bug and out of scope for a node-local tool, which the
  study states plainly rather than claiming the whole incident.

  The reproduction is a real fault on a real devnet, `test/corpus/el-slow-cpu`,
  and the verdict is quoted unedited: `local.el_slow` at medium confidence, Engine
  calls totalling 9970.78ms against that node's own 491.23ms rolling p99 — 20.3x
  its normal behaviour — with `block_seen` at +0.50s showing propagation was fine.
  It also says what whymiss would *not* have told an operator in 2023, because a
  case study that only lists strengths is an advertisement.

- **The slot schedule is read from the node instead of typed by an operator
  (ADR-0026).** `GET /eth/v1/config/spec` publishes the whole timing model —
  `SECONDS_PER_SLOT` plus the deadlines as basis points — so `whymiss watch` now
  adopts it at start-up and logs which schedule it is running with. A schedule
  the operator configured to anything other than the mainnet defaults still wins,
  and every failure path keeps the configured one and carries on.

  On existing networks this is a no-op, verified rather than asserted: the
  recorded Hoodi spec yields exactly `domain.MainnetPreEPBS()`. On a Glamsterdam
  devnet, measured live on 2026-08-30, the daemon logs

      slot schedule adopted from the node's own spec
      attestation_deadline=3s aggregation_deadline=8s
      payload_reveal_deadline=6s ptc_deadline=9s post_epbs=true

  with no configuration at all — which is BUILD_PROMPT task 5.4's "a fork is a
  config change" reduced to no change whatsoever.

  **A post-ePBS network is decided by `GLOAS_FORK_EPOCH`, never by the presence
  of the ePBS keys**, and that distinction is the reason this needed an ADR. The
  public Hoodi gateway publishes `PAYLOAD_DUE_BPS` and `ATTESTATION_DUE_BPS_GLOAS`
  while having Gloas unscheduled, and its `PAYLOAD_DUE_BPS` was 7500 where the
  Glamsterdam devnet's was 5000. Inferring the fork from key presence would have
  produced a confident, wrong payload deadline on every pre-fork node, differing
  by client build.

  Basis points are converted rounded to the nearest millisecond, because they are
  a fixed-point approximation: 3333 bps of a 12s slot is 3.9996s, and carrying
  that 0.4ms would move every timing verdict off the boundary `docs/causes.md`
  documents.

- **`SlotSchedule` carries the two post-ePBS deadlines, so a fork that moves the
  timing model is a configuration change.** `PayloadRevealDeadline` and
  `PTCDeadline` join the schedule additively, exactly as that type's doc comment
  said they would; `IsPostEPBS()`, `PayloadRevealDeadlineAt`, and `PTCDeadlineAt`
  read them. Both arrive through YAML (`schedule.payload_reveal_deadline`,
  `schedule.ptc_deadline`) and the environment
  (`WHYMISS_PAYLOAD_REVEAL_DEADLINE`, `WHYMISS_PTC_DEADLINE`) like every other
  setting, and `TestLoadSwitchesToPostEPBSTimingByConfigurationAlone` loads the
  same binary twice to prove it: no configuration yields the pre-ePBS mainnet
  schedule, a YAML file yields a post-ePBS one with both deadlines resolving
  against the slot start, and nothing differs between the runs but the file.

  **No post-ePBS default ships anywhere, deliberately.** The spec values are not
  final, and a plausible-looking constant compiled in would be indistinguishable
  from a measured one at the moment it produced a wrong verdict (I-8). Whether a
  schedule is post-ePBS is decided by whether a payload-reveal deadline was
  configured, never by a fork name — whymiss does not ask which fork is running,
  only what the timing model says.

  `Validate` rejects a half-configured pair at load rather than later: a
  `ptc_deadline` with no `payload_reveal_deadline` names no boundary for the
  committee to vote against, and both deadlines must fall after the ones they
  follow and inside the slot. The accessors return `(zero, false)` on a pre-ePBS
  schedule instead of the slot start, so a caller that ignores the bool cannot
  read "this fork has no payload deadline" as "the deadline already passed on
  every slot".

  What this does not claim: **no rule consumes either deadline yet.** That is
  BUILD_PROMPT task 5.5, and its evidence has to come from an ePBS devnet.

- **The release soak's evidence is in the repository, not just in a claim.**
  `test/soak/evidence/20260827T014421Z/` holds the `summary.txt` that
  `test/soak/run.sh` wrote itself, the 4321 samples gzipped, the daemon's whole
  18,006-line log gzipped, and the binary sha recorded before the run — 244 KB in
  total. Its README gives the commands to recompute `max_rss_kib=34688` and
  `max_database_bytes=25447600` from the raw samples and to reproduce the 55-line
  error taxonomy, so the figures quoted here and in `docs/BUILD_PROMPT.md` can be
  checked rather than believed.

  Kept because the host that produced it has been deleted, and because a soak is
  the one gate whose result cannot be re-run on demand. The 22.8 MB database and
  the 17 MB binary stay out of git: neither is needed to check any claim, and the
  binary differs from a build of the same tree only in build IDs, the module
  pseudo-version, and `vcs.revision`/`vcs.time`.

  `PHASE2_STATUS.txt` is included **with its wrong error count intact**. The
  watcher that wrote it grepped `level=ERROR` in logfmt while the daemon emits
  JSON, so it reported `errors : 0` for a run with 55. The script was fixed; the
  artefact is kept as written, next to the log that contradicts it, because a
  status file that once lied is more useful shown than quietly corrected.

### Fixed

- **This changelog quoted measurements the committed record does not support.**
  The `local.el_slow` entry described an Engine total of 16,295ms against a 981.8ms
  baseline with `head_updated` at +25.19s. Replaying `test/corpus/el-slow-cpu`
  gives 9970.78ms against 491.23ms with `head_updated` at +10.92s: the old figures
  belong to a bisection run that was never admitted as a record. Both occurrences
  are corrected and each says where the wrong numbers came from. Found while
  writing the case study, by reading the numbers back out of the record instead of
  copying them from prose — which is the only way a figure in this file should be
  produced.

- **whymiss produced no verdict at all on a post-ePBS chain.** Its Electra-era
  check that `attestation.data.index` must be 0 rejected the whole block on
  Glamsterdam, so `CheckInclusion` failed for every duty, no
  `attestation_included` was ever written, and every slot resolved to
  `no observations recorded`. EIP-7732 repurposes that field to signal payload
  availability, so a non-zero index there is correct data.

  Verified afterwards on a **public, healthy** Glamsterdam network
  (`beacon.glamsterdam-devnet-8.ethpandaops.io`, 41 blocks in 41 slots, head at
  wall clock): whymiss adopted the schedule from its spec, tracked four attester
  duties and recorded `attestation_included` and `collection_completed` with zero
  errors — against Nimbus, a client this project has no adapter for. The same
  binary before this fix recorded no inclusion on any Gloas chain.

  Measured on a local Glamsterdam devnet on 2026-08-30, on one chain across its
  own fork boundary: **23 of 32 attestations in 13 post-fork blocks carried index 1,
  against 0 of 32 in the 32 blocks before it.** The check is now scoped to the
  forks that specify it — Electra and Fulu — using the version the node itself
  reports on the same response. An unrecognised fork is accepted rather than
  refused: the failures are not symmetric, since a wrong rejection costs the
  entire product on that network while a missing assertion costs one check.
  `testdata/block_attestations_gloas.json` is the recorded block, and the test
  built on it fails against the old logic.

- **The spec response could not be decoded at all against a real node.** The
  first version of the schedule reader decoded `data` into `map[string]string`,
  which a live response breaks: `BLOB_SCHEDULE` is an array. The fixtures had
  been trimmed to the keys of interest and so hid it — the exact failure
  `AGENTS.md` forbids hand-written node responses to prevent. Both fixtures are
  now the untouched recordings, and values are decoded individually so an
  unfamiliar shape is skipped rather than discarding the document.

- **A clean shutdown was logged as a collection failure.** Every in-flight
  collector reaching a cancelled context wrote `logger.Error` — the release soak
  ended with `check inclusion ... context canceled` as its last line, which reads
  as something breaking at the exact moment nothing did. `collectionError` now
  reports a cancelled context at DEBUG and a genuine failure at ERROR. The duty
  is still marked incomplete either way: collection really was cut short, so
  `collection_completed` must not be written for it, and no timeline changes
  meaning. Only the report does.

- **One unreachable endpoint could bury every real error in the log.** The
  72-hour soak wrote 18,006 lines, of which **17,275 were the same warning** —
  96% of the file — because the gateway answers `/eth/v1/events` with `501` and
  `internal/source/beaconapi` retries for the life of the process. That retrying
  is deliberate and unchanged: a node that gains the endpoint after an upgrade is
  picked up with no operator action, and the backoff was already correct
  (exponential, full jitter, capped at 30s, measured p50 16.0s). What was wrong
  was that 55 real errors sat underneath an identical line repeated every fifteen
  seconds.

  `streamHealth` now reports the first failure, repeats it at most every fifteen
  minutes while it persists — 288 lines across a 72-hour outage rather than
  17,275 — and logs a recovery line with the attempt count and how long it was
  down. A *different* error is always reported immediately, because two failure
  modes in succession are two events. Recovery is reported when an observation
  actually arrives, since that is the only evidence the stream works: the
  reconnect loop cannot tell a connection that succeeded from one about to fail.
  Durations in these lines are strings (`"2m0s"`) rather than slog's default
  nanosecond integers, because a person reads this log during an incident.

### Changed

- **The README claimed whymiss has no ePBS support.** It said the schedule
  defaults to `MainnetPreEPBS()` and that ePBS readiness was future work, which
  stopped being true the same day: the schedule is read from the node, a live
  public Glamsterdam devnet was collected from end to end, and
  `network.payload_late` exists. The bullet now states both halves — what works
  and the two gaps, that the cause is unmeasured and that the duty it costs is the
  proposer's, which whymiss does not track. Its taxonomy counts were stale too:
  8 of 15 causes exercised, seven unmeasured, not 8 of 14 and six.

- **The install instructions named a tag that has no release.** README and the
  Compose file pointed at `v0.2.1`'s predecessor throughout — the archive
  download, both `cosign verify` identities, `WHYMISS_IMAGE`, and the
  `docker-compose.yml` error message. That tag exists in the repository but its
  release never published and no GHCR image was ever pushed for it, so every one
  of those instructions would have failed for a reader following them literally.
  All now name `v0.2.1`, which is the first tag with a published release behind
  it.

## [0.2.1] - 2026-08-30

Everything in 0.2.0, which was tagged but never published. Its release workflow
built, signed, and generated SLSA provenance for the artifacts, then failed at
the step that verifies them before anything becomes public — so no 0.2.0 release
exists and this is the first published release since 0.1.0.

### Fixed

- **The release workflow could not read the draft release it had just created.**
  The `verify published artifacts` job ran with `contents: read`, and GitHub
  shows draft releases only to tokens with push access, so `gh release download`
  answered `release not found` and the run stopped there — after `build, sign,
  sbom` and all four SLSA provenance jobs had already passed. The draft held all
  seven expected assets the whole time; nothing was wrong with the artifacts.

  Raised to `contents: write` for that job alone. There is no narrower scope that
  can read a draft, and the obvious alternative — verifying the job's own build
  artifacts instead of the uploaded ones — would check something other than what
  the world downloads, which is the one thing this job exists to prevent.

  Two things this cost, both worth stating. `v0.2.0` is burned: the tag ruleset
  makes release tags immutable, and a `push`-triggered workflow re-runs the
  workflow file from the tag's own commit, so a fix on `main` cannot rescue it.
  That is the intended trade-off, not a surprise. And the last three jobs —
  `verify`, the GHCR image publish, and the un-drafting — had still never
  executed at that point, because 0.1.0's release never ran this far either.

## [0.2.0] - 2026-08-30

### Added

- **A test proves the rules read their deadlines from `SlotSchedule` rather than
  from literals.** `TestAnalyze_TimingFollowsTheScheduleNotTheCode` replays the
  real `p2p-degraded-prysm-r06` record twice with no code change between the
  runs: under mainnet's 4s attestation deadline the verdict is
  `local.p2p_degraded`, and under a widened schedule (8s attestation, 10s
  aggregation) the same observations become `unknown.no_rule_matched`. Nothing
  tested this before — every other test in the repository analyses with mainnet
  timing, so a rule hard-coding `4 * time.Second` would have passed all of them,
  and the Phase 5 claim that a fork is a config change rested on reading the code
  rather than on a failing test if it stopped being true.

  Slot duration is held at 12s on purpose. Widening it too pushes the inclusion
  window past the record's own `collection_completed`, and `Replay` then rejects
  the fixture — which is the harness being right, not an obstacle to work around.

- **`local.el_slow` has evidence for the first time.** `test/corpus/el-slow-cpu`
  is the eighth cause covered and the first record to carry a `samples.jsonl`,
  pinned by `samples_sha256` like the observations beside it. The numbers are not
  marginal: an Engine total of **9970.78ms** — 9928.92ms across three `newPayload`
  calls plus 41.86ms of `forkchoiceUpdated` — against a measured rolling p99 of
  **491.23ms**, which is 20.3x the baseline and 6.8x the 3x spike threshold R-300
  requires, with `block_seen` at +0.50s and `head_updated` at **+10.92s** against
  a 4s deadline.

  (An earlier version of this entry quoted 16,295ms against a 981.8ms baseline
  with `head_updated` at +25.19s. Those figures belong to a bisection run that was
  not admitted; the committed record does not support them. The numbers above were
  read back out of `test/corpus/el-slow-cpu` by replaying it, which is the only
  form a number in this file should take.)

  Several cycles recorded this cause as unreproducible and `docs/BUILD_PROMPT.md`
  task 1.7 blamed the devnet's workload. That was wrong twice over. Load was one
  missing piece — Engine work is 3-5ms per block on an empty chain and 361ms under
  transaction load, and a cgroup cap can only gate work that exists. But the
  binding constraint was the corpus format: R-300 reads its baseline as a
  `domain.MetricSample`, records carried only observations, and `tl.Samples` was
  therefore nil for every scenario ever replayed. No amount of load or fault
  severity could have produced this record before the format could carry the
  input the rule reads.

  Corpus finished this cycle at 52 scenarios over 8 causes, 52/52 correct, zero
  false-high — past the 50 the release gate requires. The share asserting refusal
  rather than attribution is 21.2%, down from 48.5% at the start of this cycle.
  (This paragraph read "41 scenarios … 22.0%" while the cycle was still running;
  the figures move with `docs/evaluation.md`, which `make eval` regenerates.)

- **Corpus records can carry metric samples, which is what `local.el_slow` was
  missing.** A record was `observations.jsonl` plus a manifest, `timeline.Replay`
  built a Timeline from observations alone, and so `tl.Samples` was always nil for
  a replayed scenario. R-300 reads its `el_engine_calls_p99_ms` baseline as a
  `domain.MetricSample` — written only by the watch daemon — so that rule could
  never fire on a corpus record at any fault severity or load. Several cycles read
  the resulting silence as the devnet being too quiet.

  Records now carry an optional `samples.jsonl` beside the observations.
  `timeline.LoadSamples` reads it and treats a missing file as "no samples", so
  every record already committed replays byte-identically and no
  `corpus_format_version` bump is needed. `ReplayWithSamples` feeds them to the
  assembler; `Replay` is the same call with none. `tools/eval`, the golden test,
  and `corpusctl` all pass and validate them, so a malformed sample is caught at
  validation rather than reaching a rule as evidence.

- `tools/faultinjector` now measures a real Engine baseline before choosing a
  duty (`collectEngineBaseline`), behind its own `sample_engine_baseline` flag.
  Deliberately not folded into `sample_engine_calls`: `cl-slow-cpu` and
  `cl-slow-cpu-lighthouse` already set that flag and are in the release corpus, so
  implying the baseline from it would have added ~6.4 minutes to every one of
  their runs and, more seriously, written a baseline into their records that R-300
  evaluates *before* R-310 ever sees them. Their verdicts must not change as a
  side effect of another cause becoming reproducible.

  The ordering within a run is forced rather than preferred: the Beacon API only guarantees duties one epoch ahead and a chosen
  duty can be `minDutyLead` — 25 seconds — away, so there is no window between
  "duty known" and "duty slot" long enough to gather the 32 slots
  `EngineBaselineMinSamples` requires. Sampling first and choosing afterwards
  costs an epoch of wall clock, about 6.4 minutes per run, and is the only
  ordering that always has the samples in hand.

  The collection loop is bounded, because every other polling loop in this
  project is. As first written it would have spun one slot at a time until its
  context died against a node whose Engine counters are absent, reset every slot,
  or served by a client this build has no adapter for — a corpus run would have
  hung instead of failing with something an operator could act on. It now gives up
  after three times the ideal slot count and names what it got. The sampler is
  taken as a one-method consumer interface so that bound is covered by a test
  rather than asserted in a comment.

  The p99 accumulator moved from `internal/app` to `internal/source` as
  `EngineBaseline` so the generator computes a record's baseline exactly the way
  a running deployment computes it. A baseline derived some other way would not
  represent what the daemon writes, and the corpus exists to represent that.

- `tools/faultinjector/scenarios/el-slow-cpu.yaml` uses all of it: an 8% CPU cap
  on the execution client under transaction load, chosen from the measurement
  rather than guessed — Engine work is 361ms per block loaded against 3-5ms
  empty, and R-300 needs the slot's total to clear 3x the baseline with
  validation dominant past the +4s deadline.

- **The corpus devnet runs a transaction spammer, and the numbers show why that
  was always required.** Engine API work was trivial without it: geth's
  `newPayload` measured `beacon_block_delay_execution_time` of **3-5ms** on an
  empty chain, so no cgroup CPU or `io.max` cap could push validation past a 4s
  deadline. A passive resource cap can only gate work that exists. With
  `spamoor` running: **276-320 transactions per block**, ~13.7M gas used, and
  `execution_time` at **361ms** — about ninety times more Engine work to throttle.

  `tx_fuzz` was tried first and does not work here. It logs `Airdropping`,
  `Spamming 6 transactions per account on 100 accounts`, and then nothing: every
  EL's txpool stayed at `pending 0x0`, every block came back `gasUsed 0 / 0 txs`,
  and `execution_time` stayed at 3ms. The faucet it names holds ~10M ETH and all
  three ELs are peered, so it is neither a funding nor a connectivity problem —
  it simply never lands a transaction. Worth knowing before trusting any earlier
  note that load was added and made no difference.

  Verified stable afterwards: three nodes at 2 connected peers each, head
  advancing exactly one slot per 12s, host load settling to 2.77 on 4 vCPU with
  11.2 GB free. The heavier enclave is comfortable, which matters given the
  previous devnet died of exhaustion after days of fault injection.

- **The corpus devnet has a third node, because `network.late_block` could not be
  reproduced without one.** R-110 fires only when the watched node *and* an
  independent baseline node agree the block arrived late — and a consensus client
  records no gossip arrival for a block it produced itself. On two nodes the
  proposer is always one of the two observers, so one of the two measurements is
  always missing, and the cause was unreproducible by construction rather than by
  bad luck with recipes. That is why it has sat unmeasured since Phase 1.

  `test/e2e/kurtosis/network_params.yaml` now provisions three participants
  (one Lighthouse+geth, two Prysm+geth; validators `0-31`, `32-63`, `64-95`). The
  third is topology, not client coverage — BUILD_PROMPT §3 still locks support to
  Lighthouse and Prysm. Verified after rebuild: every node reports **2 connected
  peers**, heads advance in lockstep, host load 0.56 on 4 vCPU with 12.5 GB free,
  so the heavier enclave is comfortable.

- `tools/faultinjector/scenarios/network-late-block.yaml` uses that topology:
  node 3's consensus client is paused from eight seconds before its own proposal
  slot until five seconds into it, so the block is produced late rather than not
  at all, while the watched attester on node 1 and the baseline on node 2 are both
  untouched and neither is the proposer. Added to `CORPUS_SCENARIOS`, and
  generated on the first attempt: `test/corpus/network-late-block` records the
  block at **+5.777s** on the watched node against an independent baseline of
  **+5.747s** — 30ms apart, both past the +4s deadline, which is precisely R-110's
  discriminating condition and the first `network.late_block` evidence this
  project has ever had. The block is then orphaned (slot 47 returns 404; slot 46
  stays head), so the attester's vote is head-correct and the duty earns every
  flag — whymiss reports the cause for a duty that lost nothing, which is right:
  it explains why the slot is empty. The recipe records why late-but-canonical is
  not reachable here, with the arithmetic rather than an assertion.

- `tools/faultinjector/scenarios/proposer-missed-upstream.yaml` restores the
  coverage `network.proposer_missed` lost. It pauses the Prysm validator client
  on a slot whose proposer duty belongs to the Prysm set, while the watched
  attester duty belongs to the Lighthouse set on the other node — so the chain
  skips the slot while the watched validator keeps its own beacon node, publishes,
  and is included normally. That is the separation `proposer-missed-concurrent-vc-pause`
  deliberately does not make: pausing the client that owns *both* duties leaves
  nothing observable from the operator's own attestation path, which ADR-0021
  makes ambiguous by design. This recipe supplies the positive evidence R-100 now
  requires before it will exonerate, and is the only scenario that can exercise
  that cause at high confidence.

  Added to `CORPUS_SCENARIOS`, and generated: `test/corpus/proposer-missed-upstream`
  records slot 314 with the watched Lighthouse validator publishing at +4.34s, the
  chain skipping the slot because the paused Prysm client owned its proposer duty,
  and the attestation included at `inclusion_delay: 1` with both `head_correct` and
  `target_correct` true. The duty earned every reward flag while the slot was
  skipped — exoneration in its purest form, and exactly the shape R-100's
  high-confidence branch exists for. The corpus stood at **21** scenarios over 6 causes at that
  point in the day, and `network.proposer_missed` had evidence again after the
  unpeered-devnet records were deleted. (It ends this cycle larger — see "Corpus
  is 39 scenarios" under Changed for the final state.) The run's own log carries the provenance
  the deleted ones lacked: `preflight ok, cl-1-lighthouse-geth has 1 connected
  peer(s)`.

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
  **The run that counts** started 2026-08-26T09:16:17Z from sha256
  `5d117d1505e143cdd65f639a2688318e8c09d15525ffd1086cd266477a3c27f1` and is due
  to complete 2026-08-29T09:16Z. It is the sixth start; each earlier one was
  stopped the moment a collection-visible change landed, and every discarded run
  is kept with its reasoning under `discarded-soak-runs/` on the soak host.

  The 4h40m run it replaced is the most informative yet and is worth reading
  before the next release: 277 samples, RSS peaking at 28 MB against the 256 MB
  ceiling and the database plus WAL/SHM at 3.1 MB against the 100 MB cap; 42
  verdicts of which **38 were healthy duties correctly reported healthy**; three
  ERROR lines, all the public gateway briefly declining to confirm sync state,
  each recovered from rather than fatal. Two verdicts came back
  `unknown.no_rule_matched` on live Hoodi data, at slots 3791371 and 3791424 —
  the engine reporting a taxonomy gap against a real network rather than a
  devnet, and the most interesting unexamined lead currently open. Recording the binary hash alongside a soak
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
  planned to reach the 50-scenario release minimum topped out at 40 (13 canonical
  + 27), a 10-record shortfall left visible in the `Makefile` rather than padded out
  with more rounds of the recipes that already work. That shortfall was closed
  later in the cycle by new recipes rather than extra rounds — `network-late-block`
  and `proposer-missed-upstream` each needed the third devnet participant, and
  `el-slow-cpu` needed the samples format — and the corpus finished at 52. `tools/eval` now prints corpus
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

### Changed

- **Peer sampling no longer requires `--cl-metrics-api`.** After ADR-0023 the
  connected peer count comes from the Beacon API's `/eth/v1/node/peer_count`, but
  the sampling loop was still started only inside the `CLMetricsAPI` branch — a
  leftover from when the count came from Prometheus. The effect was that an
  operator running nothing but a beacon node got no peer count at all, and so
  lost R-200's peer corroboration, for a reason the measurement had stopped
  having. It now starts on its own.

  One behaviour change worth noting: `peer_sample_interval` was validated only
  when CL metrics were enabled, and is now validated always, because it now takes
  effect always. The shipped default is 15s and well inside the 5s–60s range, so
  only a config that deliberately set an out-of-range value is affected, and it
  fails at startup with the range in the message rather than silently sampling at
  a rate nothing checked.

  `docs/configuration.md` already told operators that `cl_metrics_api` "no longer
  governs peer sampling". That was false while the branch stood, so this closes a
  documented claim the code did not honour rather than only an internal coupling.

- **`whymiss doctor` now verifies every configured endpoint, not only the ones
  collection needs.** It contacted the beacon API, the store, and NTP — exactly
  what its contract promised — and said nothing about `--cl-metrics-api` or
  `--baseline-beacon-api`. An operator could watch every check go green and then
  get `unknown.insufficient_data` on every degraded duty, because without a
  reachable CL metrics endpoint no stage of a duty is ever timed (ADR-0024), and
  without an independent baseline the product's central question cannot be
  answered at all (ADR-0025). Setup is exactly when that is cheap to learn.

  The contract widens to "verify every configured endpoint", which needed a
  severity distinction doctor did not have. An endpoint left unset is a
  legitimate deployment, not a misconfiguration, so it reports `WARN` naming what
  becomes unreportable and does not fail the command. An endpoint that *was*
  configured and cannot be scraped reports `FAIL`, because the operator asked for
  it and it is not there. Doctor also now rejects the two baseline
  misconfigurations `watch` already rejects — a metrics endpoint with no beacon
  API to name its node, and the watched node offered as its own baseline — so
  doctor can no longer pass on a configuration `watch` will refuse to start on.

  This was previously recorded as a known issue and deferred on the grounds that
  the binary was being held stable for the 72-hour soak. That premise no longer
  held: the soak was running `5d117d15` while the tree built `bca550eb`, so the
  soak had to restart against a fresh artifact regardless, which made this the
  cheapest moment to widen the contract rather than the most expensive.

- `internal/source/promscrape/peers.go` is now `scraper.go`, because it no longer
  contains any peer scraping — ADR-0023 moved that to the Beacon API and what
  remains is the bounded HTTP client every scrape shares. A file whose name
  describes code that was deleted from it is a small trap for the next reader.

  Checked before doing it: a rename changes the built binary's hash, so it would
  normally cost a 72-hour soak restart. It did not here, because the running soak
  is already on `5d117d15` while the tree builds `04c92f38` — the genuine fixes
  that landed after it started (ADR-0025's slot-driven baseline, R-100's evidence
  timestamps, the samples format) already owe that restart. The rename rides
  along at zero marginal cost rather than buying one.

- **The README's quickstart produced a whymiss that could not attribute anything,
  and said nothing about it.** `--cl-metrics-api` appeared nowhere in the file — not
  in the `watch` example an operator copies first, and not in Limitations, which
  warned about NTP and about the baseline but not about the one flag without which
  `local.cl_slow`, `local.el_slow`, `local.vc_disconnected`, `local.vc_slow`,
  `network.late_block`, and `local.p2p_degraded` are all unreportable. The flag is
  now in the example and the consequence is stated in Limitations: collection still
  works without it, attribution does not.

  The same section carried three stale figures — 21 scenarios, 6 causes, 6
  `unknown.*` — against an actual 41, 8, and 9, and claimed a two-node devnet. All
  corrected.

- **Corpus is 39 scenarios, and the shape of it improved more than the count.**
  The campaign produced 16 usable records from 19 attempts; the three that failed
  failed honestly, with the harness refusing to write a passing record and saying
  so. Per-cause coverage after admission: `local.p2p_degraded` 8,
  `unknown.insufficient_data` 9, `local.vc_disconnected` 7, `local.cl_slow` 4,
  `network.late_block` 4, `network.proposer_missed` 4, `local.vc_slow` 3. Two of
  those causes had a single record this morning.

  The share of the corpus that merely asserts whymiss declines to attribute is now
  **23.1%**, down from 48.5% when the session began — the earlier figure was
  inflated by artifacts from an unpeered devnet, and every drop since has been an
  artifact removed or a real cause covered.

  Every admitted record was provenance-checked before admission, not just
  validated: each one either observed a block proposed by a *different* node —
  proof the devnet was peered when it was recorded — or is a scenario whose
  intended phenomenon is the absence of a block. That check exists because 14
  records generated during an unpeered window had to be deleted earlier in this
  cycle.

- **`cl-slow-cpu-lighthouse` re-tuned from 9% to 5% CPU quota, because the devnet
  changed underneath it.** It produced a healthy duty on both campaign rounds —
  no cause, `included=true` — while its sibling `cl-slow-cpu` passed 2/2 at 7%.
  The recipe was not broken: with a third participant the throttled node receives
  each block from two peers instead of one, so it still observes the block in time
  even with its CPU cut. The two siblings bracket the answer, and 5% sits below the
  7% that still bites.

  At 5% it reproduces **1 of 2**, and its failure mode is the one worth reading:
  r05 came back not with a wrong label but with **no timing at all**. At 5% of a
  core the consensus client cannot answer its own metrics endpoint in time, so no
  measured `block_seen` is recorded, no stage boundary exists, and R-999 reports
  `unknown.insufficient_data` — the fault broke the channel the scenario measures
  *through* rather than the thing it means to measure. That is the same wall
  `host-memory-pressure` hit at 16MB, and it is what "too severe" looks like for a
  fault aimed at a client that is also the observation source.

  So 9 is too loose and 5 is at the edge of too severe. 7 is the obvious next
  candidate — the sibling `cl-slow-cpu` passes 2/2 there against Prysm — but it
  has not been tried against Lighthouse, so the recipe keeps the value that has
  actually produced the cause rather than moving to a guess.

  `vc-slow-cpu` is recorded as **marginal at 2/3** rather than adjusted. Its one
  failure came back `local.vc_disconnected` because the validator client published
  nothing at all at 0.1% of a core — the fault slightly too severe, not a wrong
  label, and R-400 read the evidence correctly. Raising it is what left the duty
  healthy when the recipe was first tuned, so the window is genuinely narrow and
  saying so is more useful than picking a number.

- **Operator-facing configuration docs corrected where they had drifted from the
  code.** Both `deploy/docker/.env.example` and
  `deploy/systemd/whymiss.env.example` told operators to "set both or leave both
  empty" for the baseline pair, which ADR-0025 made wrong, and described
  `CL_METRICS_API` as being "for peer-count corroboration", which ADR-0023 made
  wrong — peer count now comes from `/eth/v1/node/peer_count`, and that endpoint
  is what makes those local-layer causes attributable at all. These are the files
  an operator edits before their first run, so a stale comment there costs more
  than a stale one in an ADR.

- `docs/architecture.md`'s "adding a third client" walkthrough referenced
  `SampleLighthousePeerCount`, `SamplePrysmPeerCount`, and
  `MetricsSampler.SamplePeerCount`, all of which were deleted with ADR-0023. It
  now demonstrates something stronger than it used to: **peer count has dropped
  off that list entirely.** A third client inherits it with no code, because the
  fact comes from a standardised endpoint rather than a per-client gauge — and the
  Lighthouse gauge it replaced was the one reading 0 on a peered node. Same for
  the network baseline when `--baseline-metrics-api` is unset. Where the Beacon
  API exposes the fact, taking it from there *removes* client-specific code
  instead of adding more, which is the direction I-11 points.

- `docs/BUILD_PROMPT.md` task 1.7 recorded four causes as unachievable "because
  this two-node devnet's per-slot workload is too light for a passive resource cap
  to gate". Three of the four now have measured counter-evidence and the entry
  says so: `network.late_block` needed a third node, not more load;
  `local.el_slow` was blocked by the corpus format; `local.host.disk_io` failed
  because every cap tried sat above the offered write rate. Only
  `local.host.cpu_steal` stands as first written. The line kept in the doc is the
  one worth remembering: *"the fault had no measurable effect" and "the fault was
  never applied to anything" look identical from the outside.*

- `docs/architecture.md` and `STRUCTURE.md` describe the current devnet (three
  participants plus a transaction spammer) and the current record layout
  (`samples.jsonl`, optional).

- **The network baseline no longer requires a second node you run — only one you
  can reach** (ADR-0025). `--baseline-metrics-api` becomes optional. Without it,
  `runNetworkBaseline` polls the independent node's own
  `/eth/v1/beacon/headers/{slot}` — the same call `BlockSeen` already makes
  against the watched node — and derives propagation from the slot start.

  This is the difference between the product's headline question being answerable
  by most operators and by almost none. R-110 and R-200 are the only rules that
  separate "the block was late for everyone" from "it was late here", both need
  `tl.Network`, and until now producing it meant running *and scraping* a second
  beacon node. A friend's node, a provider's endpoint, or any reachable API now
  works.

  Client detection is skipped on that path: it exists only to pick a Prometheus
  adapter, and every client serves the same headers endpoint, so demanding a
  recognised client would have rejected a usable baseline for no reason.

  **The first version of this had a false-attribution trap in it, and the fix is
  the reason the path is driven by the slot clock.** It was triggered by the
  watched node's `head_updated`, like the metrics path. But `BlockSeen` returns as
  soon as the baseline node has the block, so starting the poll only once *our*
  node reported a head makes the reading an upper bound shaped by our own latency:
  a node seeing the block at +6s would produce a ~6.1s baseline for a peer that
  actually had it at +0.1s. R-110 would then see local and network agreeing above
  the deadline and report `network.late_block` — exonerating a local fault, which
  is precisely what I-8 exists to prevent and precisely the class of bug this
  cycle spent the day removing. `runNetworkBaselineFromAPI` polls from the slot
  boundary instead, which makes the number an arrival time rather than a
  reflection of our own lateness.

  A second gap in the same change was caught by its test rather than by review:
  `NetworkBaseline.Validate` carries its own source check, separate from the
  observation allow-list, and still rejected `beaconapi`. The observation would
  have been built and stored and then failed to decode, leaving `tl.Network` nil
  and the whole feature silently doing nothing.

  **The trade, stated rather than buried:** `BlockSeen` polls at 500ms, so an
  API-derived arrival is quantised to that where the Prometheus gauge is
  millisecond-precise, and `thresholds.network_deviation` defaults to 750ms — the
  quantisation is a meaningful fraction of the window those rules compare within.
  It is acceptable because the error runs one way only: coarseness can push the
  deviation outside the threshold and make the rules **decline**, never make them
  attribute wrongly, which is the failure I-8 asks for. A one-sample baseline is
  already capped at `medium` confidence however it was measured. Operators whose
  baseline node is their own keep the precise path by setting the flag.

  `domain`'s allow-list for `ObsNetworkBaselineSampled` widens additively to
  accept `beaconapi`, so every recorded corpus scenario stays valid.

- **The corpus passed through 19 scenarios, up from 13 — after 14 records were
  admitted and then deleted the same day for coming from a devnet that had stopped
  being a network.** (19 is the figure at that step, not the cycle's final size.) The devnet host held 40 campaign directories; six were empty, two
  were the recordings the previous cycle dropped, and 11 were already committed,
  leaving 21 complete new records that all passed `corpusctl validate`. They were
  relabelled from their own observations and admitted, taking the corpus to 33.

  Then the devnet was inspected directly and found with **zero connected peers on
  both consensus nodes**, each advancing its own fork — 8 slots in 7 minutes
  instead of 35. Checking every record for whether it ever observed a block
  proposed by the *other* node gave a clean split at a single moment:

  ```
  … 2026-08-25T08:11  every record: CROSS-NODE block seen   (peering worked)
    2026-08-25T08:30  every record: no block observed       (isolated)
  ```

  The 14 records after that boundary are exactly the 14 whose labels had not
  matched. Their `block_skipped` observations record a node that could not see
  its peer's blocks, not a proposer that missed — and ADR-0015's skip proof
  cannot tell the difference, because an isolated node truthfully reports itself
  synced, execution-valid, and past the slot on its own fork. All 14 were
  deleted. Corpus 33 → **19**, still 19/19 with zero false-high verdicts, and the
  share of scenarios expecting `unknown.*` falls from 48.5% to 31.6% — the
  inflated ambiguity was the artefact announcing itself.

  Six campaign records from before the boundary survive
  (`p2p-degraded-lighthouse-r02/-r03/-r04`, `p2p-degraded-prysm-r03`,
  `p2p-ambiguous-no-baseline-r02/-r03`), each with a cross-node block on record.
  The two committed `proposer-missed-concurrent-vc-pause*` records also stay:
  generated 2026-08-23, well before the break, and their recipe *intends* no
  block by pausing the validator client that owns both duties. They keep the
  relabel to `unknown.insufficient_data` / `low`, which ADR-0021 justifies on the
  evidence rather than on provenance.

  One record was then generated on the rebuilt devnet and admitted, taking the
  corpus to **20**: `p2p-degraded-lighthouse-r05`, the first recorded with the
  peering preflight in front of it and the first whose peer count is truthful
  rather than a Lighthouse zero. Its log line is the provenance the earlier
  fourteen lacked — `preflight ok, cl-1-lighthouse-geth has 1 connected peer(s)`,
  `preflight ok, cl-2-prysm-geth has 1 connected peer(s)` — and its observations
  carry a block proposed by validator 47, i.e. by the *other* node, alongside a
  148 ms independent baseline against 5.509 s of local propagation.

  `tools/eval` now also prints what share of the corpus expects `unknown.*`,
  because top-1 accuracy cannot distinguish "named the right cause" from
  "correctly declined to answer", and this episode is exactly how that
  distinction gets lost. The `Makefile`'s campaign note carries the measured
  yield instead of the assumed one.
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

- **CI has been red since 2026-08-27, and the reason would have shipped a binary
  the soak never measured.** `go.mod` declared `go 1.25.14` while `ci.yml` pins
  `goreleaser/v2@v2.17.1`, which requires Go >= 1.26.5. With `GOTOOLCHAIN=local`
  the tool install failed outright — exactly the loud failure that setting is
  there to produce — so the `invariant gate` job never reached a single check.
  `make ci` passed locally throughout, because the local toolchain is go1.26.6
  and the `go` directive is only a minimum.

  The disagreement mattered for more than a red badge. `setup-go` reads
  `go-version-file: go.mod` in both `ci.yml` and `release.yml`, so a release
  built on GitHub would have used Go 1.25.14 while the binary that ran the
  72-hour soak was built by go1.26.6 — and those are not close: same source,
  same flags, 14258360 bytes against 17019042, with 13.25 million bytes
  differing. The soak evidence would have described a compiler the release
  artifact was never produced by.

  Fixed in three parts. `go.mod` gains a `toolchain go1.26.6` line beside the
  unchanged `go 1.25.14`, so the language minimum for anyone building from source
  stays where it was while the compiler the release is produced by is stated
  explicitly. The workflows pin `go-version: "1.26.6"` literally instead of
  `go-version-file: go.mod` — **`setup-go` reads the `go` directive and ignores
  `toolchain` entirely**, which the first attempt at this fix assumed otherwise
  and which the runner then reported in as many words: `Setup go version spec
  1.25.14`. And `make check.toolchain`, now inside `make check`, fails the build
  if a workflow's pinned version stops matching go.mod's toolchain line or
  reverts to reading the file. The check was verified by breaking it on purpose:
  setting one workflow back to 1.25.14 makes it exit non-zero naming the file and
  line, and restoring the value makes it pass.

  Clearing that failure exposed a second one of the same shape underneath it,
  which only became visible once CI got far enough to run `make ci` at all: the
  pinned `gofumpt@v0.10.0` flagged 23 files that the developer machine's v0.11.0
  called clean. The two versions disagree about whether a multiline call's
  closing paren goes on its own line. `gofmt` from the same Go release considers
  the tree clean either way, so the code was never unformatted — the pin was
  simply older than the tool people run. Bumped to `v0.11.0` in both workflows,
  and `check.format` now prints the gofumpt version when it fails, so the next
  person sees a version mismatch instead of reformatting 23 files to satisfy a
  binary nobody has.

  A guard is warranted rather than a note, because the failure was invisible in
  both directions: `make ci` was green on every developer machine for three days
  while the runner never executed a single check, and the version that would have
  built the release lived in a file nobody edits during a release. Verified after
  the change by rebuilding and diffing against the soaked binary again — still
  identical but for build IDs, the module pseudo-version, and
  `vcs.revision`/`vcs.time`.

- **`docs/causes.md`'s observation vocabulary listed sources the domain model had
  stopped agreeing with.** `peer_count_sampled` was documented as `promscrape`
  only, though ADR-0023 moved the count to `/eth/v1/node/peer_count` and widened
  the allow-list to accept `beaconapi`; `network_baseline_sampled` was documented
  as `xatu / promscrape`, though ADR-0025 added `beaconapi` there too. That file
  is the cause-taxonomy contract and §8.1 requires undocumented keys to fail
  validation, so a table disagreeing with `sourcePermittedFor` is the contract
  contradicting the code it governs. Both rows corrected, and every one of the
  fourteen observation kinds cross-checked against the allow-list rather than
  spot-checked — the other twelve already matched.

- **`el-slow-cpu` was absent from `CORPUS_SCENARIOS` despite having an admitted
  record.** It is the recipe that produced this project's first-ever
  `local.el_slow` record, and `make corpus.generate.all` would never have
  regenerated it. Added. Every recipe with a corpus record is now in that list;
  the four still outside it (`cl-slow-pause`, `host-disk-io`,
  `host-memory-pressure`, `vc-slow-cpu-prysm`) have no records, which is the
  intended state for each.

- **The corpus generator's peering preflight only guarded the nodes a recipe
  happened to name, and its peer floor was stale.** `minPreflightPeers` was fixed
  at 1 and documented as "the corpus devnet has exactly two consensus nodes" —
  correct when written, silently wrong from the moment a third participant was
  added to make `network.late_block` reproducible. A test pinned the constant at
  1 and justified the two-node premise, so the stale assumption was guarded
  rather than caught.

  What that permitted, measured on 2026-08-26: `host-memory-pressure` left
  cl-1-lighthouse-geth with zero connected peers, and the node did not recover
  after the fault was reverted or after a container restart. cl-1 owns validators
  0-31 of 96, so roughly a third of all proposals stopped happening — the skip
  rate on the shared devnet went from 0% before that run to 23%, 29%, and 39% as
  the hours passed with no faults running at all. Eight of the twelve runs queued
  afterwards were correctly refused, because their recipes named cl-1 as a
  baseline target. Three were not: they named only cl-2 and cl-3, which each
  still reported one peer, which cleared a floor of 1. Those three records were
  discarded rather than admitted.

  Preflight now enumerates every consensus node in the enclave from
  `kurtosis enclave inspect` and requires each to report the whole mesh — every
  other consensus node — rather than at least one peer. The requirement is
  derived from the node count, so the topology can change again without the check
  quietly weakening. `host-memory-pressure`'s recipe now records that it leaves
  its target unpeered and must run last in any batch, or be followed immediately
  by recreating the devnet.

- **Two remediation lines told operators to configure things that would not have
  helped.** Both are text the engine prints mid-incident, which is the moment the
  product exists to be right about.

  R-999's unmeasured-duty verdict said to "set `--baseline-beacon-api` **and**
  `--baseline-metrics-api`". ADR-0025 had already made the metrics endpoint
  optional — that was the whole point of it, so that answering "was it me or the
  network" needs a node an operator can *reach* rather than a second one they run
  and scrape. The remediation was still asking for the barrier the ADR removed.

  R-100's ambiguous-skip verdict said to "keep `--cl-metrics-api` set so peer
  count and block timing are recorded even on slots the chain skips", and both
  halves were wrong. Peer count comes from the Beacon API and is sampled on a
  timer regardless of that flag (ADR-0023, and since this cycle it does not even
  require it); the arrival gauge needs a block, so a skipped slot yields no timing
  from it either. That line is removed rather than reworded: no configuration
  makes this case diagnosable, the missing fact is whether the validator client
  was alive, and the two remaining lines already ask for exactly that. One fewer
  instruction beats a confident wrong one.

  Found by auditing operator-facing text against the ADRs that landed after it
  was written — the same drift that had `docs/configuration.md` claiming peer
  sampling was already decoupled and the changelog claiming
  `network.proposer_missed` had no corpus coverage.

- **Every recorded peer count claimed a provenance its own collection path
  contradicted.** `SamplePeerCount` reads `/eth/v1/node/peer_count` and the
  `domain.MetricSample` it returns is stamped `beaconapi` (ADR-0023), but the
  corpus recorder discarded that and wrote `Source: promscrape` onto the
  `peer_count_sampled` observation. R-200 prints the peer fact's source verbatim
  as its evidence attribution, so a report generated from any such record told an
  operator the count came from Prometheus — from the gauge ADR-0023 abandoned
  precisely because Lighthouse's reads 0 on a peered node. The source is now
  carried through from the sample.

  It also meant the corpus never exercised the `beaconapi` source that
  `domain.Observation`'s allow-list for `ObsPeerCountSampled` was widened to
  accept. `p2p-degraded-prysm-r06`, generated after the fix, is the first record
  to carry it and gives that widening its first recorded coverage; the thirteen
  older records still read `promscrape`.

  All 13 existing records carrying a peer count are affected and are left as
  recorded rather than rewritten: editing a `source` field on committed evidence
  is asserting something about how a fact was collected, and the honest fix is a
  fresh run. The seven records reading `peer_count: 0` are a separate and older
  artefact — they predate ADR-0023 and their zeros are the Lighthouse gauge bug
  itself, so for those the `promscrape` label is the accurate one. No verdict
  changes either way: R-200 gates on the value against `peer_count_min`, never on
  the source.

- **The corpus generator measured a faulted node through the very channel the
  fault was degrading, and lost the measurement instead of recording it.** Two
  races, one shape: the harness read both the head and the block-arrival gauge
  *after* the head had arrived, which on a node under load is after the window
  the reading was available in.

  The head was read by polling `/eth/v1/beacon/headers/head` every 200ms, and
  `headUpdatedUncached` gives up the moment it reads a slot past the one asked
  for. A node under a `cl_slow` or `p2p` fault is precisely a node whose head
  advances in bursts while its HTTP server answers slowly, so the head stepped
  from `slot-1` to `slot+1` between two samples and was never observed. Every run
  of `cl-slow-cpu-lighthouse` and `p2p-ambiguous-no-baseline-prysm` was lost this
  way: no stage was timed, R-110 declined, and the duty came out healthy with no
  cause — the fault was real and the record said nothing.

  The head is now read from the `/eth/v1/events` SSE stream alongside the poll,
  taking whichever answers first. The stream cannot lose a transition: it pushes
  one head event per head change, so a throttled node delays the event rather
  than dropping it. It also makes a distinction the poll cannot — a slot the
  stream steps over was *genuinely skipped*, where a poll's not-found conflates
  a skip with a sample that arrived too late. The deadline, not the poll, is what
  now ends the wait negatively: the poll's not-found carries no information,
  since it is returned the instant the head is past, and letting it end the wait
  reported "not observed" while the stream still had its whole window.

  The arrival gauge had the same shape with a different cause. `p2p-degraded-prysm-r04`
  measured propagation of 6.19s against an independent baseline of 449ms with a
  peer count of 2 — an unambiguous `local.p2p_degraded` — and reported
  `unknown.insufficient_data`, because by the time the gauge was scraped it had
  advanced to the next slot and `SampleBlockTimingForSlot` could only fail.
  `p2p-degraded-prysm-r05`, the same recipe against the same devnet minutes
  later, kept its arrival and produced the verdict. The recipe was never the
  variable; which side won the race was. The gauge is now watched from before the
  slot begins, so it is observed *through* its transition into the slot rather
  than sampled after it. A genuinely skipped slot still yields the same
  "advanced to slot N+1" error, because the gauge still steps over it.

  Harness-only (`tools/faultinjector`); the shipped binary is untouched. See
  "Known issues" for the narrower form of the gauge race that remains in the
  collector.

- **A p2p scenario could abandon a good record because its own fault made the
  peer-count sample time out.** `p2p-ambiguous-no-baseline-prysm-r02` measured
  block propagation at 8.096s — the fault working exactly as intended — and was
  then thrown away because one `GET /eth/v1/node/peer_count` against the node it
  was degrading exceeded the 10s client timeout.

  Not a regression from ADR-0023, which is where the suspicion pointed first: both
  the Beacon API client and the Prometheus scraper bound requests at 10s, so
  moving peer count between them changed no deadline. Production is unaffected
  either way — `runPeerSampling` logs a failed sample at debug and keeps its loop,
  so the daemon loses one sample and nothing else. It is the generator that treats
  the sample as required and abandons the record.

  Abandoning is right: a p2p scenario without a peer count is missing evidence
  R-200 requires, and writing it would produce a record whose label the run cannot
  earn. But a single timeout from a node the scenario is *actively degrading* is
  the fault working, not evidence about peering — the same reasoning the Engine
  baseline collector already applies to a failed scrape. It now retries three
  times, two seconds apart so all three land inside the window the fault is live
  for, and still abandons the record if none succeeds.

  The class was swept rather than the one instance patched. Every other place the
  generator abandons a record on a failed measurement was checked: the setup and
  clock samples all happen before the fault is applied, `sample_pressure` reads a
  local cgroup file, and the two that do poll a degraded node — `poll block` and
  `poll attestation publish` — already tolerate a transient error the way
  `HeadUpdated` was taught to in d56ac76, checking the context and continuing to
  poll rather than returning. Peer count was the only one missing that tolerance,
  because it is a single one-shot call rather than a poll loop.

- **The generator threw away every valid `local.el_slow` record it produced.**
  `runScenarioCmd` checks its own output before writing by replaying the
  observations and comparing the verdict against the recipe's `expect:` block, and
  it called `timeline.Replay(observations, ...)` — without the samples it had just
  spent an epoch collecting. R-300 reads its baseline from `tl.Samples`, so the
  check evaluated a timeline the engine will never see again, the rule declined
  for want of an input the run had already measured, and a correct record was
  reported as a failed scenario.

  Caught on the first real `el-slow-cpu` run. The record it wrote is right —
  `tools/eval` scores it `local.el_slow` at medium, 1/1 — while the generator's
  own log called it `unknown.no_rule_matched`. The numbers were never marginal:
  Engine total 9970.78ms against a measured 491.23ms rolling p99, 20.3x the
  baseline and 6.8x the 3x spike threshold, with `head_updated` at +10.92s against
  a 4s deadline. Samples now flow into the check and the record from one slice, so
  the two cannot diverge again. (This paragraph first quoted 16,295ms against
  981.8ms, figures from an unadmitted bisection run; corrected against a replay of
  the committed record.)

  This is the cost of wiring a new input into four consumers and missing the
  fifth: `tools/eval`, the golden test, and `corpusctl` all got samples when the
  format was added; the generator that produces them did not.

- **Three queued devnet drivers raced and ran two fault injections at once.**
  Each waited for the devnet with `while pgrep faultinjector; do sleep 30; done`,
  and a poll landed in the gap between one run exiting and the next starting — so
  `el-slow-cpu` and a top-up record executed concurrently on a 4-vCPU host. That
  is the contention this project already recorded as producing bad records, and it
  is exactly what the preflight peering check cannot catch, because the devnet
  stays healthy while the *measurements* get noisy.

  The two records generated during the overlap were discarded rather than
  inspected — a record made under known contention is not evidence worth
  arguing about. Replaced by a single sequential driver: one process running one
  scenario at a time cannot race with itself.

- **`samples.jsonl` was evidence nobody could prove had not been hand-edited.**
  `observations.jsonl` is pinned by `observations_sha256` in the manifest and
  `corpusctl` verifies it — that checksum is what the previous cycle relied on to
  show two mislabelled records had not been tampered with. The samples file
  introduced alongside it had no checksum at all: it only had to parse. An engine
  baseline is precisely the value that decides whether R-300 sees a spike, so
  "it parses" is not a standard that file can be held to.

  The manifest now carries `samples_sha256` when a record has samples, written
  before the manifest is marshalled rather than after — the first version set it
  afterwards, so the file on disk would have pinned observations and said nothing
  about samples. `corpusctl` verifies the hash and rejects both halves of the
  mismatch: a `samples.jsonl` with no checksum to pin it, and a manifest
  declaring a checksum for a file that is not there. Records without samples are
  unchanged, with no file and no field.

- **The goroutine-leak gate covered three of the daemon's nine collectors, with
  the Phase 2 DoD item ticked.** `goleak.VerifyTestMain` runs for `internal/app`,
  but `validWatchConfig` enables none of the optional collectors, and every other
  `BaselineBeaconAPI` reference in the test file is a config-validation case that
  never starts the daemon. Block timing, peer sampling, clock sampling, duty
  tracking, host sampling, and the metrics-path network baseline all start only
  under configuration no test set, so a leak in any of them would have shipped
  with "zero goroutine leaks (goleak)" recorded as met.

  Two tests close it. `TestWatch_EveryCollectorShutsDownCleanly` turns every
  collector on at once against `httptest` servers and asserts the whole daemon
  unwinds on cancellation, and `TestWatch_BaselineFromBeaconAPIShutsDownCleanly`
  covers ADR-0025's collector specifically — it sits inside a `BlockSeen` poll
  with a 15s deadline, so a missed context check there would hang a shutdown
  rather than fail loudly.

- **Both cgroup pressure helpers never joined the cgroup they were meant to load,
  and had never worked.** `startCgroupMemoryPressure` — and the I/O helper copied
  from it — ran `nsenter -t 1 -m -- sh -c 'printf ... > cgroup.procs'` from inside
  the helper container to place itself in the target's cgroup. That write failed
  with `printf: I/O error` on every run, and nothing checked it: the helper stayed
  in its own cgroup and its load was charged there, not to the client under test.

  Proven rather than inferred. Writing the same PID to the same
  `cgroup.procs` **directly from the host succeeds** — the target's process count
  goes from 2 to 3, and the cgroup is a plain `domain` leaf with empty
  `cgroup.subtree_control`, so nothing about it forbids the move. The namespace
  hop was the only thing failing. `faultinjector` already runs privileged on the
  host, so it now performs the move itself through the existing
  `writeContainerCgroupFile` path, and the helper simply waits two seconds before
  issuing any I/O so its first write is already charged and throttled.

  It stayed invisible because `memory.high` throttles the target's own processes
  directly, so PSI rose whether or not the helper joined —
  `host-memory-pressure.yaml`'s bisection figures (45.41%, 25.14%, …) were geth's
  own reclaim, not the helper's doing, and that recipe should be re-bisected now
  that its helper actually contributes. `io.max` has no such side effect, which is
  what finally exposed the bug: four consecutive disk-io attempts read
  `io.pressure some avg10=0.00%`, and the fifth, with the move fixed, read
  **43.78%**.

- **The netem p2p fault degraded one peer's link, which stopped meaning anything
  the moment the devnet became a mesh.** `NetemParams.PeerTarget` scoped the tc
  filter to a single `ip src <peer>/32`, and the four p2p scenarios relied on that
  peer being the target's *only* peer. With a third node the same gossip reaches
  the target unthrottled via the relay, so the fault would have degraded nothing
  measurable while the recipes still claimed a 5s delay on block propagation —
  a scenario silently measuring the wrong thing, which is worse than one that
  fails.

  It is now `peer_targets`, a list, with one tc filter per peer steering into the
  same throttled band, and the four scenarios name every peer of their fault
  target. Validation requires a non-empty list for `local.p2p_degraded` and
  rejects one containing the target itself. `PeerDropFault`'s doc comment carried
  the same two-node assumption in prose ("Target's only peer is PeerTarget") and
  is corrected; no committed scenario uses that fault.

  Committed p2p records are unaffected — they are honest recordings of a two-node
  devnet — but regenerating them requires this fix, which is why it lands with the
  topology change rather than after it.

- **On the default deployment, any degraded duty was reported as a bug in
  whymiss** (ADR-0024, taxonomy 3.0.0 → 4.0.0, engine 0.14.0 → 0.15.0). Found in
  the soak's own output, not by reading code: 2 of the first 42 verdicts against
  live Hoodi came back `unknown.no_rule_matched`, whose remediation is "this is a
  taxonomy gap and a project bug, not an operator problem — file an issue".

  Slot 3791424 is not short of facts — `block_seen` at +6.18s past the +4s
  deadline, `head_updated` +7.32s, `attestation_published` +12.68s, outcome
  degraded — yet its verdict said every stage was "unavailable because its timing
  boundary was not observed". `timedBlockSeen` only accepts a `block_seen` sourced
  from `promscrape`, which is right: the Beacon API's polled `block_seen` records
  when the collector *noticed* the block, not when it arrived, and using it as a
  stage boundary would invent precision nobody measured. But a collector without
  `--cl-metrics-api` records only that polled observation, so no stage is ever
  timed, every timing rule declines for want of input, and R-999 blamed the
  project for a configuration the operator never made. That is also a direct
  contradiction of the cause's own definition in `docs/causes.md` — "data was
  complete and trustworthy" — when the data was never collected.

  R-999 now branches. Nothing timed and something lost → `unknown.insufficient_data`
  naming `--cl-metrics-api` as the fix. Any stage measured → unchanged
  `unknown.no_rule_matched` with the full decomposition, which for the first time
  is a claim worth trusting and a project-health metric worth tracking, since its
  rate was previously dominated by deployments that simply had no metrics
  endpoint. Nothing lost → unchanged, deliberately: the branch is gated on
  `dutyHasObservableLoss` because the engine turns `no_rule_matched` into its
  clean-pass verdict, and without the gate a duty that earned every reward flag
  would have been reported as "we could not tell" instead of "nothing went wrong".
  `TestAnalyze_HealthyDutyThroughRealRules` caught that regression during
  implementation.

- **Lighthouse's peer count read 0 while the node was genuinely peered, which
  made R-200's corroboration vacuous there** (ADR-0023). `local.p2p_degraded`
  only fires when the connected peer count is *below*
  `thresholds.peer_count_min`, so a permanent zero meant that check — the one
  that establishes insufficient peering rather than some other local cause —
  passed unconditionally on every Lighthouse deployment. Measured on a fresh
  two-node devnet at one instant: `/eth/v1/node/peer_count` returned
  `connected: 1`, Prysm's `connected_libp2p_peers{agent="lighthouse"}` returned
  1, Lighthouse's own `block_mesh_peers_per_client{Client="Prysm"}` returned 1 —
  and every `libp2p_peers*` series, the one whymiss read, returned 0.

  The recorded fixture the unit test replays contains `libp2p_peers 0` too, so
  the test agreed with the adapter and neither noticed. A real capture proves the
  parser reads the file; it cannot prove the file means what the parser assumed.

  Peer count now comes from `GET /eth/v1/node/peer_count`, the standardised
  Beacon API endpoint, through `beaconapi.Client.PeerCount`. The Lighthouse and
  Prysm peer parsers and their dispatcher were deleted rather than left in place
  where a refactor could wire the wrong one back in; the normalised metric name
  R-200 reads is unchanged. `tools/faultinjector` reads the same endpoint through
  the same call, so a generator can no longer bake a permanent zero into every
  record it writes. Two allow-lists widen additively so no committed corpus
  record needs regenerating.

- **`tools/faultinjector` now refuses to record a scenario on an isolated node.**
  `preflightPeering` reads each consensus service's connected peer count before
  the fault is applied and aborts with instructions to recreate the devnet if any
  of them is at zero. This is the check whose absence cost 14 corpus records: an
  isolated node reports `is_syncing=false`, `sync_distance=0`, and an advancing
  head, all truthfully about its own fork, so ADR-0015's skipped-slot proof is
  satisfied while being wrong about the canonical chain. Peer count is the fact
  that separates "the network skipped this slot" from "this node is alone", and
  it is now checked before a recording exists to be trusted. The fault target
  itself is deliberately excluded — it is often a validator client, which has no
  peer count, and a peer-degradation scenario is allowed to reduce peering on a
  node it names elsewhere.

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

### Verified

- **The 72-hour Hoodi soak passed, and the binary it ran is the binary being
  released.** It ran 2026-08-27T01:44:21Z to 2026-08-30T01:45:18Z on
  `1d0bdd6d5660219b73f7bb10196222f6e67cfb0b5c7750d8bd60853f6421d08d` and
  `test/soak/run.sh` wrote `result=PASS` itself — the script exits non-zero the
  instant RSS crosses 262144 KiB or the database crosses 104857600 bytes, so the
  result is an assertion rather than a reading. Over 4321 one-minute samples:
  `max_rss_kib=34688` (13.2% of the ceiling), `max_database_bytes=25447600`
  (24.3% of the cap), 674 verdicts recorded. The run directory is archived under
  a gitignored `soak-results/`.

  The identity claim is the part worth stating precisely, because the sha does
  not match a fresh build and that looks alarming until it is chased down. The
  soaked binary was built at `0c0a94b` with a dirty tree; HEAD has moved since.
  Rebuilding HEAD for `linux/amd64` with the soaked binary's own version string
  produces the same size, 17019042 bytes, differing in **201 bytes across five
  regions, every one of them build metadata** — the Go and GNU build IDs (120 B),
  the module pseudo-version (2 x 20 B), and `vcs.revision` with `vcs.time`
  (2 x 75 B). No instruction byte and no string constant differs. Nothing under
  `cmd/` or `internal/` was touched while closing the item, precisely so this
  stays true.

  All 55 `ERROR` lines in the run were read and attributed, none of them to
  whymiss: 49 are `cannot confirm slot <n> as seen or skipped: node is not fully
  synced, execution-valid, and past the slot after waiting 1m30s` — the node
  behind the public gateway lagging — 5 are `unexpected status 500: dialing ...
  timed out` from the gateway itself, and 1 is the `context canceled` raised when
  the soak stopped the daemon at the end. Note that the helper watcher on the
  soak host reported `errors : 0` for this run: it greps `level=ERROR` in logfmt
  while the daemon emits JSON. The script was fixed; the number to trust is 55.

### Known issues

- **A gateway without `/eth/v1/events` fills the log with one repeated warning.**
  The public Hoodi gateway answers that endpoint with `501`, and
  `internal/source/beaconapi`'s `Stream` retries indefinitely by design — a node
  that gains the endpoint after an upgrade is then picked up with no operator
  action. Every attempt logs `event stream error, reconnecting`. Measured over
  the release soak: **17,275 of 18,006 log lines, 96% of the file**, 2.79 MB in
  72 hours or about 0.93 MB per day, one line roughly every 15 seconds.

  The backoff itself is correct and is not what is being reported here. It is
  exponential with full jitter, base 1s and cap 30s (`backoff.go`), and the
  measured intervals match: p50 16.0s, max 29.7s, so the node sees at most two
  connection attempts a minute in the worst case. I-5 is respected.

  What is wrong is the signal-to-noise: 55 real `ERROR` lines sat inside 17,275
  identical warnings. Throttling repeats belongs in the `onError` callback in
  `internal/app/watch.go`, which is a change to the daemon binary, and the
  release binary is the one that just passed a 72-hour soak — so this is recorded
  for after v0.1.0 rather than fixed now. Until then `docs/runbook.md` says to
  filter the log by level instead of reading it top to bottom. The daemon writes
  to stdout, so rotation is journald's or the container runtime's job.

- **Resident memory's shape is warm-up, not drift — settled by the full 72-hour
  run.** The release soak ended 2026-08-30 with `max_rss_kib=34688` across 4321
  samples, 13.2% of the 262144 KiB ceiling, so the plateau held for three days
  and the alternative this entry was worried about (a pause before retention
  makes the SQLite page cache grow again) did not happen. The rest of this entry
  is the reasoning from when it was still open, kept because the shape it
  describes is what the long run confirmed. An earlier reading taken at the
  three-hour mark reported
  a steady +1407 KiB/h climb and treated it as possible unbounded growth. More
  samples corrected that. Quarter-by-quarter least-squares slopes on the release
  run's own `samples.csv` (266 samples, 4.42h):

    0.0-1.1h   mean 24608 KiB   +2869.9 KiB/h
    1.1-2.2h   mean 25839 KiB   +1784.9 KiB/h
    2.2-3.3h   mean 27504 KiB    +180.8 KiB/h
    3.3-4.4h   mean 27697 KiB    -118.8 KiB/h

  RSS flattens near 27.7 MiB by the third hour and the most recent quarter is
  slightly negative. The whole-run figure of +995 KiB/h is an artefact of
  averaging the warm-up in, which is why the early reading was wrong.

  What keeps this on the list rather than closing it: the discarded 9h33m run on
  the previous binary (`5d117d15`) did *not* flatten — its second-half slope
  (+601 KiB/h) was steeper than its first (+435 KiB/h) across nearly ten hours.
  Two runs, two shapes. The plateau here may be the real steady state, or it may
  be a pause before the database reaches its retention threshold and the SQLite
  page cache grows again. The 72-hour soak is the measurement that settles it,
  and `test/soak/run.sh` fails on its own if RSS ever crosses 262144 KiB. It ran
  and it passed: peak 34688 KiB, roughly 7 MiB above the 4.4-hour plateau this
  entry described and nowhere near the ceiling.

  It still cannot be attributed from outside the process: the exporter registers
  only `whymiss_*` collectors — no `go_memstats_*`, no `process_*` — so nothing
  can distinguish live heap from pages the Go runtime is holding. Adding the Go
  runtime collector answers it in one scrape and is the obvious next step; it was
  not done here because the release soak was already running on a frozen binary
  and restarting costs another 72 hours.

- **The collector loses this slot's block arrival when the block is more than a
  slot late.** `runBlockTiming` is driven by `head_updated` and scrapes the
  client's latest-value arrival gauge once the head has arrived
  (`internal/app/blocktiming.go`). The gauge carries no slot label, so
  `SampleBlockTimingForSlot` fails outright when it finds the gauge already past
  the slot asked for — which happens when propagation exceeds one slot, because
  the next slot's block has then already moved the gauge on. The polled
  `block_seen` from the Beacon API is still written, but `timedBlockSeen` does not
  accept it as a stage boundary (ADR-0024), so no stage is timed and the duty
  resolves to `unknown.insufficient_data`.

  Two reasons this is recorded rather than fixed. It fails in the direction I-8
  prefers — an honest `unknown`, never a wrong confident verdict — and the
  condition is narrow: propagation has to exceed a full 12s slot, not merely the
  4s attestation deadline. And the fix worth having is the one ADR-0024 already
  names as deferred: teaching the rules to use the polled arrival as a coarse
  boundary with its resolution declared. That is not a free widening. A polled
  timestamp is biased *late*, so it inflates propagation and would make R-110 and
  R-200 fire more readily rather than less — the opposite of the safe direction
  ADR-0025 relied on for the network baseline, and the direction that puts the
  corpus's zero-false-high record at risk. It needs the resolution carried on the
  observation, a conservative comparison against it, and a major taxonomy bump
  under ADR-0005.

  The same race in the corpus generator, where deliberate faults make
  multi-second propagation the norm rather than the exception, is fixed — see
  "Fixed" above.

- **`local.host.disk_io` cannot be produced on this devnet, and the reason is now
  understood rather than assumed.** With the helper fixed, the fault works: 48.36%
  I/O pressure, well over the 20% `iowait_pct` threshold. It still yields no
  record, because R-600 requires the duty to have lost something and nothing a
  disk fault does here costs a reward flag.

  Two measured reasons, both structural:

  - Inclusion on this devnet is unconditionally forgiving. An attestation observed
    **23.2s** late still landed at `inclusion_delay: 1` with `head_correct` and
    `target_correct` both true. With 96 validators and no aggregation competition,
    lateness alone never costs `timely_source` or `timely_target`.
  - The only flag a disk fault could plausibly take is `timely_head`, which needs
    `head_updated` to slip past the +4s deadline so the attester votes the previous
    head. It stayed at **+1.34s** under 48.36% pressure, because geth's validation
    on this chain is not disk-bound: a devnet a few hundred slots old holds a state
    small enough to sit entirely in RAM, so execution issues no trie reads to
    throttle. Capping reads at 32 KB/s changed nothing, for exactly that reason.

  Changing this needs a chain with enough state to push execution's reads out of
  RAM — a devnet-scale problem, not a fault-severity one. The recipe stays out of
  `CORPUS_SCENARIOS` and `CORPUS_CAMPAIGN` with the full six-run bisection log so
  no batch creates a record whose label the fault has not earned.

- **Phase 1's "io.max has no measurable effect at any severity" was true and
  misdiagnosed.** Measured on the loaded devnet: el-1's geth cgroup writes
  **40.8 KB/s**, averaged over two slots (`io.stat` wbytes 64,995,328 ->
  65,998,848 across 24s), arriving as a burst of roughly 500 KB per block. The
  first attempt here capped `write_bytes_per_sec` at 1 MB/s — **25 times above
  the rate being offered** — and produced exactly what earlier cycles saw:
  `io.pressure some avg10=0.00%`, propagation 408ms, a healthy duty. A cap above
  the offered rate throttles nothing, and that is indistinguishable from "the
  fault does not work" unless the offered rate is measured first.

  The mechanism was never at fault. `io.stat` confirms the container's cgroup is
  accounted on device `8:0`, the same whole disk `CgroupIOFault` resolves and
  writes `io.max` for, and the cgroup carries the `io` and `memory` controllers
  so writeback is attributed rather than escaping to the root cgroup. Severities
  in the MB/s range against a devnet writing tens of KB/s is the entire
  explanation.

  The first cap chosen from that measurement was also wrong, in the opposite
  direction, and the recipe's bisection log keeps both rather than quietly
  replacing one: 128 KB/s — above the average, on the theory that each block's
  ~500 KB burst would stretch to about 4s — again produced `0.00%` PSI, with
  propagation at 550ms against 408ms unthrottled. PSI `some` measures time a task
  spends *stalled* on I/O, and a buffered write does not stall the writer: the
  kernel accepts it and flushes later, so throttling writeback only lets dirty
  pages accumulate. geth stalls when it fsyncs. What has to be made slow is an
  individual synchronous flush, not aggregate throughput, which argues for a cap
  well *below* the offered rate.

  `tools/faultinjector/scenarios/host-disk-io.yaml` records the measurement, both
  rationales, and a bisection log in the same format `host-memory-pressure` uses. The harness
  refused to write a passing record for the failed attempt on its own —
  "recorded evidence yielded verdict \"\" (high), expected \"local.host.disk_io\";
  fixture was preserved for diagnosis but does not pass the labelled scenario" —
  which is the check that stops a mislabelled record reaching the corpus.

- **RESOLVED later the same day — kept because the diagnosis is the useful part.**
  `local.el_slow` was unreproducible in the corpus for a reason that had nothing
  to do with the devnet, and every earlier cycle's diagnosis was wrong. The format
  was built and the cause now has a record; see "`local.el_slow` has evidence for
  the first time" under Added. What follows is why it could not have worked before. The
  standing explanation was that this devnet's per-slot workload is too light for a
  passive resource cap to gate. Half of that is true and now fixed (see the
  transaction-load entry under Added). The other half is the actual blocker:

  R-300 requires a rolling baseline, `tl.SampleValue(ComponentEL,
  el_engine_calls_p99_ms)`. That value is a `domain.MetricSample`, and it is
  written in exactly one place — `internal/app/blocktiming.go`, inside the watch
  daemon. `tools/faultinjector` never writes it. Worse, it *could not*: a corpus
  record is `observations.jsonl` plus a manifest, `timeline.Replay` builds a
  Timeline from observations alone, and so `tl.Samples` is always nil for every
  replayed scenario. R-300's baseline check therefore fails on every corpus record
  that will ever be generated, at any load, under any fault severity.

  R-200 has the same dual-form problem and solves it: `peerCountFact` reads the
  peer count from either a `peer_count_sampled` observation (the corpus form) or a
  `MetricSample` (the live form). R-300 has no such reader, and there is no
  observation kind that carries an Engine baseline.

  The load fix is real and measured, so the concept now works — Engine work went
  from 3-5ms to **361ms** per block once transactions flowed, and a CPU-capped EL
  against a ~361ms baseline is a spike R-300 would name. Only the plumbing is
  missing. Closing it needs, roughly: an optional `samples.jsonl` beside
  `observations.jsonl` (absent means no samples, so no `corpus_format_version`
  bump), a loader and a Replay path that accepts it, `tools/eval`, the golden
  test and `corpusctl` updated to pass and validate it, `engineBaseline` extracted
  out of `internal/app` so the generator can share it, and a pre-fault sampling
  loop in `faultinjector` long enough to fill it — `engineBaselineMinSamples` is
  32 slots, about 6.4 minutes.

  Not attempted here deliberately. It crosses the corpus-format contract and
  `internal/timeline`, which the shipped binary replays. It was built later the
  same day with tests and a checksum, and `docs/BUILD_PROMPT.md` task 1.7 has been
  corrected to say this rather than blame the devnet's workload.

- **The corpus cannot test R-200's peer threshold at all, and never could.**
  `thresholds.peer_count_min` defaults to 40, while this devnet's ceiling is the
  number of other participants — **one** when it had two nodes, **two** since the
  third was added for `network.late_block`. Either way `peerCount <
  peer_count_min` is trivially true for every scenario it generates, degraded or
  healthy, so the peer half of the rule is satisfied by the topology rather than
  by the fault. The corpus now spans the whole range this devnet can produce —
  `peer_count` 0, 1, and 2 across thirteen records — and no value in it can
  distinguish a peering fault from a healthy node. (The zeros are the older
  Lighthouse gauge artefact, not a reading: see the peer-count provenance entry
  under Fixed.)

  What those records do exercise is the timing half, and that part is real:
  `p2p-degraded-lighthouse-r05` measured propagation of 5.509 s locally against
  an independent baseline of 148 ms. Testing the peer half needs either a devnet
  with dozens of peers per node or per-scenario thresholds in the corpus
  manifest, which `tools/eval` and the golden tests do not have — they analyse
  every record with `rca.DefaultConfig()`.

- **Proposer duty tracking is blocked by the taxonomy, not by the collector.**
  `beaconapi.Client.FetchProposerDuties` exists and is called by nothing;
  `deriveOutcome` already handles `DutyProposer` (block proposed → ok, otherwise
  missed). Wiring it into `runDutyTracking` is small. It was not done, because
  the verdict it would produce is worse than no verdict — replaying a missed
  own-proposal through `rca.Analyze` today gives:

  ```
  outcome=missed  cause="unknown.insufficient_data"  confidence=low
  evidence: no stage of this duty was timed: block arrival was never measured,
            so propagation, validation, and signing durations are all unknown
            and no timing rule could be evaluated
  ```

  That output was re-measured against the current engine rather than quoted from
  when this entry was written. It used to read `unknown.no_rule_matched` with
  "data was complete and trustworthy, yet no rule matched — this is a taxonomy
  gap, not an operator problem", which is precisely the claim ADR-0024 split R-999
  to stop making. The verdict is better than it was and still not worth shipping.

  Every rule that could explain it keys on attestation observations, and R-100
  excludes the operator's own proposer duty by design (ADR-0015: the taxonomy
  cannot distinguish a local proposal failure from an upstream network event).
  So an operator who just lost a block proposal — the most expensive miss there
  is — would be told the data was insufficient, on a duty where nothing about the
  collection was actually wrong. `docs/causes.md` has no cause for a local
  proposal failure, and adding one is a taxonomy change under ADR-0005 that needs
  its own evidence and remediation contract. That comes first; the collector
  wiring is an afternoon after it.

- **6 of the 14 causes in `docs/causes.md` still have no corpus scenario**:
  `network.inclusion_failure`, all four `local.host.*`, and
  `unknown.no_rule_matched`. (`network.late_block` and `local.el_slow` were on this
  list when it was written and both gained records the same day.) Unmeasured is not passing. One
  gap has direct evidence rather than merely no coverage:
  `p2p-degraded-prysm-r02`, held out of the corpus, records propagation 5.38 s
  alongside validation 4.77 s with no Engine samples, so neither stage dominates
  and no rule matches. The engine correctly returns `unknown.no_rule_matched`,
  which `docs/causes.md` defines as a bug-report signal — a real two-stage
  failure the taxonomy cannot yet name. Kept in
  `../whymiss-campaign-evidence-20260826/records/` rather than labelled with the
  gap it exposes.

  **A second instance, from a different fault, arrived on 2026-08-27 and makes
  the gap look structural.** `cl-slow-cpu-lighthouse-r05` measured every stage
  cleanly once the observer races were fixed — propagation 1.056s (5.9%),
  validation 8.901s (49.7%), signing 7.947s (44.4%), Engine total 1.398s — and
  R-310 still declined, because `Stages.Dominant` requires a share of at least
  `thresholds.dominance` (0.5) and validation reached 0.497. It missed by three
  tenths of a percentage point, roughly 54ms of stage time. Every other condition
  R-310 asks for held: validation was the largest stage and the Engine total sat
  well under half of it, so execution was correctly exonerated.

  The mechanism is the same in both records and it is not a threshold-tuning
  problem. A cgroup CPU cap starves the consensus client and the signing path in
  the same container, so it inflates two stages together and neither can
  dominate; `p2p-degraded-prysm-r02` reached the same dead end from a netem
  fault. The taxonomy assumes one stage carries the blame. Naming a host-level
  slowdown that degrades several stages at once is a taxonomy change under
  ADR-0005, and until it exists `unknown.no_rule_matched` is the honest answer —
  which is exactly what R-999 returned.

- **`cl-slow-pause` was run for the first time on 2026-08-27 and cannot earn its
  label.** It was referenced by nothing — not `CORPUS_SCENARIOS`, not
  `CORPUS_CAMPAIGN`, no record, no bisection log — so nothing had ever checked it.
  Two faults were found, one after the other.

  Its hold was 7s while its own description claimed the node stays paused "until
  five seconds after slot start". The injector applies a fault at slotStart-8s, so
  7s released the node at slotStart-1s: r01 measured propagation of 663ms and a
  duty that earned every reward flag. The fault worked and simply stopped before
  the block it meant to delay existed. The hold is now 13s, the arithmetic the
  description implies.

  At 13s the fault bit — propagation 5.357s — and the premise failed anyway. A
  paused container is not a slow consensus client: while frozen it receives
  nothing, and on resume the block is already waiting, so r03 validated it in
  205ms, the fastest stage in the record. The delay lands entirely in propagation,
  never in the validation latency the recipe claims to isolate. With no
  `baseline_target` configured, R-110 correctly returned
  `unknown.insufficient_data` rather than guessing between a slow node and a slow
  network.

  Adding a baseline would make it worse: against this devnet's peer count of 2 and
  a `peer_count_min` of 40, R-200 would report `local.p2p_degraded` — confident
  and wrong, since peering was never at fault. The recipe is recommended for
  deletion; `local.cl_slow` already has five records from `cl-slow-cpu`, which
  caps CPU and so makes the client slow *while running*. It is kept only so the
  measurement survives, stays out of both batch lists, and no record from it
  should be admitted under this label.

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
