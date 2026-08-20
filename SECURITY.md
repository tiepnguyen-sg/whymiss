# Security policy

whymiss runs on staking infrastructure. That places it in a position of trust, and
the invariants below are part of the security contract, not implementation detail.

## Design guarantees

These hold for every released version. A build that violates one is a security bug,
not a feature regression.

| Guarantee | Invariant |
|---|---|
| Never reads, requests, or references validator keys, keystores, remote-signer credentials, or mnemonics. No configuration key accepts a secret of this class. | I-2 |
| Read-only against the beacon node. No mutating calls. | I-1 |
| Runs unprivileged — non-root, no added Linux capabilities. | I-3 |
| No egress by default. No telemetry, no version checks, no analytics, no crash reporting. Network access only to endpoints the operator configured explicitly. | I-4 |
| Bounded memory, disk, and connections, with documented ceilings. | I-12 |
| Single static binary, `CGO_ENABLED=0`, no external database process. | I-13 |

## Supported versions

While the project is at `v0.x`, only the latest released minor version receives
security fixes.

## Reporting a vulnerability

**Do not open a public issue for a security report.**

Use GitHub's private vulnerability reporting:
<https://github.com/CHANGEME/whymiss/security/advisories/new>

Please include the affected version, a reproduction, and the impact you believe it
has. If the finding is that one of the guarantees above does not hold, say which one
— that framing gets triaged fastest.

## Response SLA

| Stage | Target |
|---|---|
| Acknowledgement of report | 3 working days |
| Initial assessment with severity | 10 working days |
| Fix released, or a public mitigation if a fix needs longer | 90 days from acknowledgement |

We will credit reporters in the advisory unless asked not to. Coordinated disclosure
is preferred; we will agree a date with you rather than impose one.

## Out of scope

- Vulnerabilities in the beacon node, execution client, or validator client whymiss
  observes. Report those to the relevant client team.
- Findings that require the operator to have already granted whymiss privileges it
  refuses by design (root, key access) — but a path by which whymiss *acquires* such
  privileges is very much in scope.
