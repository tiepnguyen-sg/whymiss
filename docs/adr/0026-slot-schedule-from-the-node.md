# ADR-0026: the slot schedule is read from the node, and ePBS is decided by the fork epoch

- Status: accepted
- Date: 2026-08-30

## Context

`domain.SlotSchedule` exists so that a fork which moves the timing model is a
configuration change rather than a rewrite (BUILD_PROMPT task 5.4). Until now
that meant an operator typing the deadlines into YAML, with
`domain.MainnetPreEPBS()` as the default.

Two things measured against a live Glamsterdam devnet on 2026-08-30 changed the
picture.

**The node already publishes the whole timing model.** `GET /eth/v1/config/spec`
returns `SECONDS_PER_SLOT` along with the deadlines as basis points of the slot:
`ATTESTATION_DUE_BPS` 3333, `AGGREGATE_DUE_BPS` 6667, and — post-ePBS —
`ATTESTATION_DUE_BPS_GLOAS` 2500, `PAYLOAD_DUE_BPS` 5000,
`PAYLOAD_ATTESTATION_DUE_BPS` 7500. This is the same situation ADR-0023 (peer
count) and ADR-0025 (network baseline) found: a fact the standardised API already
carries, which whymiss was obtaining by a worse route.

**Presence of the ePBS keys does not mean the network runs ePBS.** The public
Hoodi gateway — with Gloas unscheduled — publishes `PAYLOAD_DUE_BPS` and
`ATTESTATION_DUE_BPS_GLOAS` too, because the client binary knows the constants
for a fork the network has not reached. Worse, its `PAYLOAD_DUE_BPS` was **7500**
where the Glamsterdam devnet's was **5000**. A design that inferred "post-ePBS"
from the presence of those keys would have produced a confident, wrong payload
deadline on every pre-fork node, and a different wrong one depending on the
client build.

## Decision

**Derive the schedule from `GET /eth/v1/config/spec`, and let an explicitly
configured schedule win.**

- Adoption happens once at daemon start, and only when the configured schedule is
  exactly `domain.MainnetPreEPBS()` — indistinguishable from having configured
  nothing. Any other schedule is the operator's statement and is kept, with the
  fact logged.
- **`GLOAS_FORK_EPOCH` decides whether a schedule is post-ePBS**, and only when
  the chain has reached it. Unscheduled forks report the sentinel `2^64-1`, so
  this is a comparison, never a presence check.
- Basis points are converted to durations **rounded to the nearest millisecond**.
  They are a fixed-point approximation: 3333 bps of a 12s slot is 3.9996s, not
  the 4s `docs/causes.md` documents. At millisecond resolution every mainnet
  constant lands exactly where the document says, and Gloas's 2500/5000/7500 land
  on 3s, 6s and 9s.
- **Every failure keeps the configured schedule and the daemon keeps running.** A
  node that does not publish the keys, answers with something unusable, or fails
  the request changes nothing.

## Consequences

On existing networks this is a no-op, and that is verified rather than asserted:
the recorded Hoodi spec yields exactly `domain.MainnetPreEPBS()`
(`TestFetchSchedule`). On a Glamsterdam chain the daemon picks up a 3s
attestation deadline, a 6s payload-reveal deadline and a 9s PTC deadline with no
operator action.

`docs/configuration.md`'s schedule settings become an override rather than the
only source. The daemon logs the schedule whenever it is not the mainnet default
— an adoption that changed something, an operator override, or a node that could
not be read — and stays silent when the node agrees with the defaults, which is
every pre-Glamsterdam network. An operator debugging a timing verdict can
therefore see the deadline it was measured against in exactly the cases where it
is not the documented one.

**One schedule per process, which is wrong inside a fork transition.** The epoch
is read once at start-up, so a daemon that was running before the fork keeps
pre-ePBS timing until restarted, and one started after it applies post-ePBS
timing to any earlier duty still inside the retention window. Around the
transition the attestation deadline moves by a second, so verdicts for duties on
the other side of the boundary can be attributed against the wrong deadline. This
is recorded rather than fixed because the correct fix — a schedule resolved per
slot — changes `domain.Timeline`'s shape, which is frozen while the corpus
depends on it. Operators should restart whymiss after a fork.

## Alternatives considered

**Keep configuration as the only source.** Rejected: it makes the operator retype
what the node already knows, and a fork then silently attributes against the old
deadlines until somebody notices.

**Infer post-ePBS from the presence of the payload keys.** Rejected on the
measurement above — it would have been wrong on every pre-fork node, confidently.

**Ship `MainnetPostEPBS()` constants in `internal/domain`.** Rejected: the spec is
still being refined, and a plausible-looking constant compiled into the binary is
indistinguishable from a measured one at the moment it produces a wrong verdict
(I-8). The node is the authority for its own network.
