# ADR-0024: R-999 separates "never measured" from "measured and unexplained"

- Status: accepted
- Date: 2026-08-26

## Context

The 72-hour Hoodi soak produced 42 verdicts in its first 4h40m. Thirty-eight were
healthy duties correctly reported healthy. Two came back
`unknown.no_rule_matched`, whose remediation reads:

> this is a taxonomy gap and a project bug, not an operator problem — file an
> issue with this timeline attached

Slot 3791424 is one of them. Its recorded timeline is not short of facts:

```
slot_start              02:14:48
block_seen              02:14:54.180   (+6.18s, past the +4s deadline)  source: beaconapi
head_updated            02:14:55.319   (+7.32s)
attestation_published   02:15:00.684   (+12.68s)
attestation_included    02:27:27.793
outcome: degraded (lost timely_target, timely_head)
```

Yet the verdict's own evidence says every stage was *"unavailable because its
timing boundary was not observed"*.

The reason is `timedBlockSeen`: a stage boundary is only taken from a `block_seen`
whose source is `promscrape`. That restriction is correct — the Beacon API's
`block_seen` comes from polling every 500 ms, so its timestamp records when the
collector noticed the block, not when the block arrived, and using it as a
boundary would manufacture precision that was never measured.

But a collector run without `--cl-metrics-api` records *only* the polled
`block_seen`. So on that deployment no stage is ever timed, every timing rule
declines for want of input, and R-999 announces a taxonomy gap. That claim
contradicts `docs/causes.md`'s own definition of the cause — "data was complete
and trustworthy" — because the data was neither: the timing was never collected.

The failure mode is not rare. It is what the *default* deployment does with any
degraded duty: whymiss tells an operator their configuration gap is a bug in
whymiss, and gives them nothing to change.

## Decision

R-999 branches on whether the stage decomposition exists.

- **No stage boundary observed, and the duty lost something** → report
  `unknown.insufficient_data` at `low`, stating that no stage of the duty was
  timed and that the polled `block_seen` is deliberately not used as a boundary,
  with remediation naming `--cl-metrics-api` (and `--baseline-*` for the
  network-vs-local question).
- **Any stage measured** → unchanged: `unknown.no_rule_matched`, the full stage
  decomposition, and the file-an-issue remediation. This is now a claim that can
  be trusted, because it is only made when there was something to match against.
- **Nothing was lost** → unchanged, and deliberately so. The branch is gated on
  `dutyHasObservableLoss`, because `rca.Analyze` converts R-999's
  `no_rule_matched` into its clean-pass verdict for a healthy duty. Without the
  gate a duty that earned every reward flag would be reported as "we could not
  tell" instead of "nothing went wrong" — strictly worse, and false. This was
  caught by `TestAnalyze_HealthyDutyThroughRealRules` while implementing the
  change, not by review.

Narrowing when an existing cause fires is re-scoping under ADR-0005 §3, so the
taxonomy advances 3.0.0 → 4.0.0 and the engine 0.14.0 → 0.15.0.

## Consequences

- The default deployment gets an actionable verdict instead of a bug report. The
  two Hoodi slots above would now say what to configure.
- `unknown.no_rule_matched` becomes a meaningful project-health metric for the
  first time. Its rate was previously dominated by deployments that simply had no
  metrics endpoint, which is exactly the signal `docs/causes.md` §7 wants to
  track and exactly what that noise was hiding.
- A genuine gap is still reported as one. `p2p-degraded-prysm-r02`, held out of
  the corpus, has propagation 5.38 s and validation 4.77 s measured with neither
  dominant and no Engine samples to separate them — stages exist, no rule
  matches, and it stays `no_rule_matched`. That case remains open and unfixed;
  this ADR only stops it being drowned out.
- Not addressed here: whether the polled `block_seen` could serve as a coarse
  boundary with its resolution declared, which would let some timing attribution
  work without a metrics endpoint at all. That is a bigger question about what a
  stage duration means, and it interacts with ADR-0022's proposal to source the
  network baseline from the Beacon API for the same reason.
