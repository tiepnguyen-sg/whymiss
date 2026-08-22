# Threat model

Who this is for: `docs/BUILD_PROMPT.md` §1.2 — "a solo staker or a small professional
operator... deeply suspicious of new software touching their staking box, and will
uninstall anything that costs them an attestation." This document is the answer to the
question that trust decision actually turns on: **what can whymiss touch, and what
happens if it's compromised, buggy, or simply wrong?**

Read this before installing whymiss on a box that runs a validator.

## What whymiss is

A separate, unprivileged process that periodically calls a beacon node's REST API
and (optionally) reads a few files under `/proc`, writes what it observed to a local
SQLite file, and — when a duty is missed or late — runs a pure, offline rule engine
over those observations to name a cause.

## What whymiss is not

Not a validator client. Not a signer. Not in the hot path of any duty. If whymiss
crashes, hangs, or is killed, no attestation or block proposal is affected — it only
ever *watches and explains after the fact*. This is the single most important property
in this document; everything else follows from it.

## Assets and non-assets

| | |
|---|---|
| **In scope to protect** | The observations and verdicts whymiss stores locally; the host it runs on (no lateral movement, no privilege gain); the beacon node's availability (no degradation from whymiss's own traffic). |
| **Explicitly never handled** | Validator signing keys, keystores, remote-signer credentials, mnemonics (I-2). There is no code path, no config key, no flag that accepts a secret of this class. whymiss cannot leak what it never reads. |

## Trust boundary

```
┌─────────────────────────────────────────────────────────┐
│ staking box                                              │
│                                                           │
│   validator client ──(signs)──> beacon node ──> network  │
│         │  (keys live here — whymiss never touches this) │
│         │                                                 │
│         │                    ┌──────────────┐            │
│         └───(no connection)──│   whymiss    │            │
│                               │ unprivileged │            │
│  beacon node <──(read-only REST, operator-configured)──┤ │
│  host /proc  <──(read-only, local only)─────────────────┤ │
│  SQLite file <──(read/write, local only)─────────────────┤ │
│  operator's Prometheus <──(scrapes /metrics if enabled)──┤ │
└─────────────────────────────────────────────────────────┘
```

whymiss has no connection to the validator client or its keys at all — not "read-only
access to keys," no access whatsoever. It sits entirely on the observability side of
the box.

## Threats considered

Each row maps to an enforced, CI-checked invariant in `docs/BUILD_PROMPT.md` §2 —
these are not aspirations, they're checked by `make check` and `make ci` on every
change (`internal/app/duty_tracking.go`, `internal/source/**`, `Makefile`
`check.*` targets).

| Threat | Mitigation |
|---|---|
| **Key or credential exposure.** whymiss is compromised or has a bug that leaks secrets. | Nothing to leak (I-2). No keystore path, password, or signer URL is ever accepted, read, or logged. Verified by code review convention (`AGENTS.md` "Never") and by there being no such flag in `cmd/whymiss/*.go`. |
| **whymiss degrades the beacon node it watches.** A retry storm or unbounded polling starves the validator client's own requests. | Every outbound call has a timeout and a rate-limit floor (`--min-request-interval`, default 200ms; I-5). Backoff on an unhealthy node is exponential with jitter, never a retry storm. `internal/source/beaconapi`'s tests assert request rate stays under the configured ceiling. |
| **Privilege escalation / lateral movement if whymiss is compromised.** An attacker who gets code execution inside whymiss tries to pivot to the rest of the box. | Runs unprivileged, no Linux capabilities beyond default (I-3), enforced by `make check.nonroot` in CI and, at deploy time, by the Docker image's baked-in non-root user (`deploy/docker/Dockerfile`, `gcr.io/distroless/static-debian12:nonroot`, no shell) and the systemd unit's `DynamicUser=yes` + full sandboxing directives (`deploy/systemd/whymiss.service`: empty `CapabilityBoundingSet`, `ProtectSystem=strict`, `RestrictNamespaces`, `MemoryDenyWriteExecute`, `SystemCallFilter=@system-service`, ...). |
| **Data exfiltration / unwanted network egress.** whymiss phones home, checks for updates, or is tricked into talking to an attacker-controlled host. | Zero outbound calls except to endpoints the operator explicitly configured (I-4): `--beacon-api` and, if set, `--cl-metrics-api`. No telemetry, no version checks, no analytics — `make check.egress` greps for any `http.Get`/`http.Post`/`http.DefaultClient` construction outside `internal/source/**` and fails the build if found. Host-level observations (`/proc/pressure/*`, `/proc/stat`) never leave the box — they're local file reads, not network calls. |
| **Resource exhaustion on a small box.** whymiss's local SQLite store or memory use grows without bound and starves the validator client (which must never lose a duty because of a sidecar). | Disk uses rolling retention with a byte cap, not just a time cap — `--retention-max-age` (default 14 days) and `--retention-max-bytes` (default 1 GiB), enforced by `internal/store.Prune` (I-12). Must be safe on a Raspberry Pi 5 alongside a node; the Docker Compose deployment additionally caps container memory (`mem_limit`) and process count (`pids_limit`), and the systemd unit caps `MemoryMax`/`TasksMax`. |
| **A wrong, confident diagnosis leads an operator to take the wrong corrective action** (e.g. "restart the validator client" when the real cause was the network). | I-7: every verdict carries evidence with a timestamp and source attribution — never an unsupported claim. I-8: rules that don't conclusively match emit `unknown`, never a guess dressed up as a confident answer — a wrong confident verdict is treated as worse than no verdict. I-9: timing-derived rules are suppressed under measured clock drift rather than emitting an untrustworthy timing verdict. See `docs/evaluation.md` for measured accuracy against the current corpus, and `README.md`'s Limitations section for what's not yet covered. |
| **Supply-chain compromise via a dependency.** A malicious or compromised third-party module ships inside the binary. | Dependency austerity (I-14): every new module needs an ADR justifying it (`docs/adr/`); target is fewer than 15 direct dependencies at v1.0. `govulncheck` runs in every `make ci`. Release provenance (signed checksums, SBOM, SLSA attestation, cosign signatures) is task 4.7 — see `SECURITY.md` (task 4.8) for the disclosure process once that lands. |
| **The client-adapter layer leaking a client-specific bug or assumption into shared code**, causing a wrong verdict that looks client-agnostic but isn't. | I-11: no package outside `internal/source/**` may import a client-specific type or branch on a client name — enforced by `make check.isolation`. |

## What whymiss deliberately does not defend against

Being explicit about this is part of being trustworthy, not a weakness to hide:

- **A beacon node that lies to whymiss.** whymiss trusts the REST API it's pointed at.
  If the beacon node itself is compromised or misconfigured, whymiss's observations
  (and therefore its verdicts) are only as good as that input. This is the same trust
  relationship the validator client already has with its own beacon node.
- **A host that's already root-compromised.** No unprivileged process, including this
  one, defends against an attacker who already has root on the box.
- **A malicious or spoofed host-metrics source**, if the operator points `--cl-metrics-api`
  at something other than their own consensus client.
- **Availability of the beacon node or validator client.** whymiss is a diagnostic tool,
  not a monitor of last resort — it explains misses after they happen; it does not
  prevent them, and its own downtime never causes one (see "What whymiss is not").

## Verifying these claims yourself

Don't take this document's word for it — every claim above is checkable in minutes:

```sh
make check.egress     # fails if outbound HTTP exists outside internal/source/
make check.isolation  # fails if a client name leaks outside internal/source/
make check.nonroot    # fails if the binary needs root or carries capabilities
grep -rn "keystore\|mnemonic\|private.*key" cmd/ internal/ --include='*.go'   # expect nothing
```

For the container and systemd hardening specifically, the exact commands used to
verify them (read-only rootfs, no shell present, `systemd-analyze verify`) are in
`CHANGELOG.md`'s Phase 4 entries for tasks 4.3 and 4.4.
