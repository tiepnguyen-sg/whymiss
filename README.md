# whymiss

A read-only sidecar that runs next to an Ethereum node and, when a validator misses or
is late on a duty, produces a forensic post-mortem naming the responsible layer with
timestamped evidence.

The one question the product answers: **"Was it me, or was it the network — and if it
was me, which layer?"**

Read-only against the beacon node. No validator keys. Network access is limited to
the beacon/metrics/NTP endpoints you explicitly configure. Runs unprivileged.

## Quickstart

**Docker** (brings up `whymiss watch` plus a Prometheus + Grafana dashboard, all
non-root with read-only root filesystems):

```sh
git clone https://github.com/tiepnguyen-sg/whymiss.git && cd whymiss
cp deploy/docker/.env.example deploy/docker/.env   # set the exact image tag and every required value
cd deploy/docker
docker compose pull
cosign verify \
    --certificate-identity "https://github.com/tiepnguyen-sg/whymiss/.github/workflows/release.yml@refs/tags/v0.2.0" \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    ghcr.io/tiepnguyen-sg/whymiss:v0.2.0
docker compose up -d
```

Set `WHYMISS_IMAGE=ghcr.io/tiepnguyen-sg/whymiss:v0.2.0` in `.env` for this
release. The verified reference above must match it exactly.

Open `http://127.0.0.1:3000` and log in as `admin` with the password you set
(`GRAFANA_ADMIN_PASSWORD` — anonymous access is deliberately off). See
[`deploy/docker/docker-compose.yml`](deploy/docker/docker-compose.yml).

**From source:**

```sh
make build
./bin/whymiss --beacon-api http://127.0.0.1:5052 --db whymiss.db \
    doctor --ntp-server pool.ntp.org
./bin/whymiss watch --beacon-api http://127.0.0.1:5052 --db whymiss.db \
    --validator-index 24 --ntp-server pool.ntp.org --metrics-addr :9101 &
./bin/whymiss 2001 --db whymiss.db          # once a duty at slot 2001 has completed
```

**systemd**, for running the bare binary as a long-lived unprivileged service on the
node itself, is in [`deploy/systemd/`](deploy/systemd/).

**Prebuilt binary:** download the `linux_amd64` or `linux_arm64` archive from the
release's GitHub page, then verify it before running anything from it:

```sh
# 1. The checksum matches the archive you downloaded.
sha256sum -c --ignore-missing checksums.txt

# 2. Set this to the exact release tag you downloaded.
RELEASE=v0.2.0

# checksums.txt itself is genuinely signed by this project's release workflow —
#    not a stored key, a keyless Sigstore identity bound to the exact GitHub Actions
#    run that built it (docs/adr/0010-release-supply-chain.md).
cosign verify-blob \
    --bundle checksums.txt.bundle \
    --certificate-identity "https://github.com/tiepnguyen-sg/whymiss/.github/workflows/release.yml@refs/tags/${RELEASE}" \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    checksums.txt

# 3. (optional) inspect exactly what's inside the archive before running it.
cat whymiss_*_linux_amd64.tar.gz.sbom.json | jq '.packages[].name'

# 4. Verify that the archive was built from this repository at this exact tag.
#    The generic generator names multi-artifact provenance multiple.intoto.jsonl.
slsa-verifier verify-artifact whymiss_*_linux_amd64.tar.gz \
    --provenance-path multiple.intoto.jsonl \
    --source-uri github.com/tiepnguyen-sg/whymiss \
    --source-tag "$RELEASE"
```

The same release is published as a signed multi-arch OCI image. Use the exact tag,
never an implicit floating version:

```sh
RELEASE=v0.2.0
docker pull ghcr.io/tiepnguyen-sg/whymiss:${RELEASE}
cosign verify \
    --certificate-identity "https://github.com/tiepnguyen-sg/whymiss/.github/workflows/release.yml@refs/tags/${RELEASE}" \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    ghcr.io/tiepnguyen-sg/whymiss:${RELEASE}
```

The release workflow also verifies these same commands against its published assets
before taking the GitHub release out of draft. Cosign and SLSA use the public GitHub
workflow identity and transparency logs; see
[`docs/adr/0010-release-supply-chain.md`](docs/adr/0010-release-supply-chain.md).

## Usage

```sh
whymiss watch --db whymiss.db --beacon-api http://127.0.0.1:5052   # collector daemon
whymiss <slot> --db whymiss.db                                     # explain a slot
whymiss <slot> --db whymiss.db --format json                       # machine-readable
whymiss timeline <slot> --db whymiss.db                            # raw facts, no interpretation
whymiss doctor --beacon-api http://127.0.0.1:5052 --ntp-server pool.ntp.org
```

## Sample report

```
$ whymiss 2001 --db whymiss.db
```

```markdown
# Slot 2001 — local.host.memory_pressure

**Outcome:** missed
**Confidence:** medium

## Evidence

- [+10m0s] memory pressure (PSI some avg10 29.24%) exceeded the 10.00% threshold (local.host.memory_pressure: 29.24 vs 10 pct) — *hostmetrics*

## Remediation

1. add RAM or reduce the client cache settings
2. never run a validator on a box that swaps

---
Engine 0.13.0 · Taxonomy 2.0.0
```

This illustrates the operator-facing output shape using the recorded
[`test/corpus/host-memory-pressure`](test/corpus/host-memory-pressure) through the same
`Explain` path the CLI calls. RCA accuracy against the current test corpus is tracked in
[`docs/evaluation.md`](docs/evaluation.md), regenerated by `make eval`.

## Limitations

Read these before trusting whymiss on a live staking box.

- **Attester duties only, right now.** `whymiss watch --validator-index` tracks and
  explains missed/late attestations continuously. Proposer duties are not yet wired
  into that automatic pipeline.
- **Small evaluation corpus.** RCA accuracy (`docs/evaluation.md`) is measured against 9
  labelled scenarios covering 6 of the taxonomy's causes, generated on a 2-node
  Lighthouse+Prysm / geth Kurtosis devnet — not yet validated against mainnet incidents
  or other client pairings.
- **Closed taxonomy, on purpose (I-8).** whymiss prefers `unknown` over a wrong
  confident guess. If your failure mode isn't in
  [`docs/causes.md`](docs/causes.md) yet, expect `unknown`, not a plausible-looking
  wrong answer.
- **Host causes require local host access.** Memory pressure, CPU steal, and PSI I/O
  pressure require whymiss to run on the affected Linux host with host sampling
  enabled. Without those `/proc` samples, the causes fall back to lower confidence or
  `unknown`; they are never fabricated from Beacon API data.
- **EL sub-causes need client-specific evidence.** Host-wide PSI cannot identify the
  execution client or storage device responsible, so this build reports a proven
  Engine slowdown as `local.el_slow` at medium confidence without guessing a sub-cause.
- **Timing attribution requires an explicitly configured NTP server.** Without
  `--ntp-server`, observations are still collected but timing-based verdicts become
  `unknown.insufficient_data`; whymiss never assumes the host clock is correct.
- **Network-vs-local propagation requires an independent baseline node.** Configure
  both `--baseline-beacon-api` and `--baseline-metrics-api`; without them, a late
  block that cannot be localized becomes `unknown.insufficient_data`.
- **No ePBS support yet.** The slot schedule defaults to `MainnetPreEPBS()`; ePBS
  readiness is Phase 5.

## Non-goals

- No signing, validator keys, fleet management, rewards calculation, or block explorer.
- No custom alert engine: whymiss emits bounded Prometheus signals for existing tooling.
- No machine learning or opaque scoring; every verdict maps to a documented rule.

## Building

```sh
make build   # binary at bin/whymiss
make ci      # lint, invariants, tests, vuln scan, corpus/evaluation gates
make test.image  # build the Linux amd64/arm64 OCI image locally; does not publish
```

See [`AGENTS.md`](AGENTS.md) for the full engineering contract and
[`docs/BUILD_PROMPT.md`](docs/BUILD_PROMPT.md) for the phased build plan.
