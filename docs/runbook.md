# Runbook

Operational guidance for running `whymiss watch` continuously against a live
validator. Read `README.md`'s Limitations section first — several entries here exist
because of gaps documented there.

## Preflight

Run the same checks with the exact endpoints and database path the service will use:

```sh
whymiss --beacon-api http://127.0.0.1:5052 --db /var/lib/whymiss/whymiss.db \
  doctor --ntp-server pool.ntp.org
```

Do not start duty attribution until all three checks report `OK`. In particular,
missing or unreachable NTP is not treated as a healthy default: observations remain
available, but timing rules return `unknown.insufficient_data`.

## Alerting: on cause, not outcome

`whymiss_duty_verdicts_total{cause,outcome}` (`internal/exporter`, `docs/adr/0009-prometheus-exporter.md`)
is the metric to alert on. Alert on `cause`, not just `outcome` — "missed" alone
doesn't tell you whether to look at your own box or shrug it off as the network's
problem. Two starter rules:

```yaml
# Any local (self-inflicted) cause firing at all in the last hour deserves a look —
# these are the ones you can fix.
- alert: WhymissLocalCause
  expr: sum by (cause) (increase(whymiss_duty_verdicts_total{cause=~"local\\..*", outcome=~"missed|degraded"}[1h])) > 0
  labels: {severity: warning}
  annotations:
    summary: "whymiss attributed a missed/degraded duty to {{ $labels.cause }}"

# unknown.no_rule_matched on a *missed* or *degraded* duty means the taxonomy has a
# gap and needs a look. The outcome filter is deliberate: a healthy duty carries no
# cause at all (cause="none"), never this one, so it can't reach this alert.
- alert: WhymissUnknownCause
  expr: sum(increase(whymiss_duty_verdicts_total{cause="unknown.no_rule_matched", outcome=~"missed|degraded"}[1h])) > 0
  labels: {severity: info}
  annotations:
    summary: "whymiss saw a missed/degraded duty it couldn't explain — check docs/causes.md for a taxonomy gap"
```

The bundled Grafana dashboard (`deploy/grafana/whymiss-dashboard.json`) covers the
same data visually: missed/degraded duties by cause over time, an outcome breakdown,
and a "last hour" stat panel.

## Known-benign situations — not incidents

**Healthy duties scrape as `cause="none"`, `outcome="ok"`.**
A duty with nothing wrong carries no cause at all — the same shape `no_duty` uses,
distinguished from it by the `outcome` label. This is the normal steady state for a
healthy validator, not a gap. (Before the fix in `CHANGELOG.md`'s Unreleased section,
these reported as `local.unknown.no_rule_matched` with "file an issue" remediation; if
you see that on an `ok` outcome, you're running a pre-fix build.)

**A real cause on an `ok` outcome is not a contradiction.** A validator client that
was measurably slow but still beat the deadline reports `local.vc_slow` with
`outcome: ok` — an early warning worth watching, not a miss. Alert on
`outcome=~"missed|degraded"` for pages; treat local causes on `ok` as a trend signal.

**`whymiss watch` running with neither `--validator-index` nor `--metrics-addr` set.**
This is the default — a pure observation collector, no duty tracking, no exporter.
Expected if you're only using `whymiss <slot>`/`whymiss timeline <slot>` ad hoc rather
than continuous monitoring.

## Diagnosing whymiss itself

**No metrics appearing at `/metrics` after startup.**
1. Confirm `--validator-index` was actually set — `--metrics-addr` alone does nothing
   (`cmd/whymiss/watch.go`'s help text says so: "ignored unless --validator-index is
   set").
2. Metrics only appear once a tracked duty's full Deneb inclusion window has played
   out (assigned → watched → explained). The collector waits through the end of the
   final valid inclusion slot plus two slots of polling slack: 35–66 slots after the
   duty, depending on its position in the epoch.
3. Check the watch process's logs for `fetch genesis` or `FetchAttesterDuties` errors —
   these mean `--beacon-api` is unreachable or wrong, not that the exporter is broken.

**`whymiss <slot>` returns "no observations recorded for slot N".**
Either `--db` points at the wrong file, or `whymiss watch` wasn't running (or wasn't
tracking that validator) when the slot happened. whymiss cannot retroactively explain
a slot it never observed — there is no backfill from the beacon node's own history.

**The SQLite store is growing faster than expected.**
Check `--retention-max-age` (default 14 days) and `--retention-max-bytes` (default
1 GiB) are set to what you expect, and that `--retention-interval` (default 1 hour)
isn't disabled (`0`). `internal/store.Prune` deletes oldest-first until both are
satisfied (I-12) — if the store still grows past the byte cap, that's a bug, not
expected behavior; file an issue with the store's actual size and your retention flags.

Stores first created by older pre-release builds use SQLite's full `VACUUM`
compatibility path when reclaiming pages. For routine incremental reclaim without
the temporary full-file rewrite, stop whymiss, archive the old database if needed,
and start the release candidate with a new database path.

**The beacon node seems to be getting hammered by whymiss's requests.**
It shouldn't be — every call is rate-limited (`--min-request-interval`, default
200ms; I-5) with exponential backoff on an unhealthy node. If you suspect otherwise,
capture the request rate whymiss is actually issuing (e.g. from the beacon node's own
access log or its own Prometheus metrics) and compare against `--min-request-interval`
before assuming whymiss is the cause — most apparent request storms turn out to be
the validator client or another sidecar.

**Verdicts report `unknown.insufficient_data` with a clock note.**
Run `whymiss doctor` with the service's `--ntp-server`. Confirm UDP/123 is permitted,
the hostname resolves inside the container/service sandbox, and the measured offset
is within the configured 100ms trust limit. whymiss deliberately does not reuse a
stale last-known-good sample for new observations.

## Restarting and upgrading

`whymiss watch` is stateless between restarts except for the SQLite store — restarting
it (or upgrading the binary/image) loses no history and requires no migration step
beyond whatever `internal/store`'s own schema migration does automatically on open.
A duty in flight when whymiss restarts is simply not tracked for that one slot; it
resumes tracking from the next epoch boundary.

- **Docker Compose:** `docker compose pull && docker compose up -d` (or, building
  from source, `docker compose up -d --build`).
- **systemd:** replace `/usr/local/bin/whymiss`, then `sudo systemctl restart whymiss`.

## Running the Hoodi soak gate

Run the release soak on Linux, against the same local Hoodi Beacon API and validator
indices the operator deployment will use. Run `make ci` first; its `internal/app`
suite uses `goleak` to enforce clean daemon shutdown.

```sh
BEACON_API=http://127.0.0.1:5052 \
VALIDATOR_INDICES=24,187 \
NTP_SERVER=pool.ntp.org \
make test.soak
```

The target runs for 72 hours by default, samples `/proc` once a minute, fails if RSS
exceeds 256 MiB or the SQLite database plus WAL/SHM exceeds the configured 100 MiB
retention cap, and preserves its log, CSV samples, and summary under
`soak-results/`. Optional `CL_METRICS_API`, `BASELINE_BEACON_API`,
`BASELINE_METRICS_API`, and `METRICS_ADDR` values exercise those collectors and the
Prometheus exporter too. Set `METRICS_ADDR` to a loopback address
(`127.0.0.1:9101`): the endpoint is unauthenticated by design, and a soak host with
a public interface would otherwise publish it. For a short harness check, set
`SOAK_DURATION_SECONDS`; a release sign-off must use the 72-hour default.

The soak measures whichever collectors you enable and nothing more. A run against a
Beacon API gateway that does not serve `/eth/v1/events` exercises the reconnect
backoff and the REST duty path but never the SSE path, and a run without
`CL_METRICS_API`/`BASELINE_*` leaves block timing and the network baseline
uncollected. Record which of them were enabled alongside the summary, and record the
`sha256` of the exact binary the run used — a soak whose binary predates a
collection fix measures code that is not being released.

Read that log by level, not top to bottom. Against a gateway answering `/eth/v1/events`
with `501` the reconnect backoff logs one identical `WARN` per attempt, roughly every
15 seconds for as long as the run lasts: the 72-hour release soak produced 17,275 of
them out of 18,006 lines — 96% of the file, 2.79 MB, about 0.93 MB per day. That is
the backoff working as designed rather than a fault (`internal/source/beaconapi`
retries indefinitely so a node that gains the endpoint later is picked up without
operator action), but it will bury the lines that matter, so filter with
`grep '"level":"ERROR"'` first and only then read around what it finds. The daemon
logs to stdout, so rotation is journald's or the container runtime's job, not
whymiss's.

## Publishing a release

Do not create or push the release tag until every item below is complete:

1. Regenerate the live corpus, then run `make corpus.validate`, `make eval`, and
   `make eval.check`. The committed report must cover at least 50 records, reach
   at least 90% top-1 accuracy, include ambiguous `unknown.*` cases, and contain
   zero wrong high-confidence verdicts.
2. Archive a passing 72-hour Hoodi soak directory and run `make ci`,
   `make test.freshinstall`, `make test.image`, `make test.faults.clock`, and
   `make release.snapshot` from the release commit.
3. Move the release notes from `[Unreleased]` to the exact version and date in
   `CHANGELOG.md`; update the README's exact tag and measured corpus/sample data.
   Commit and push this state to `main`.
4. Make the GitHub repository public, enable private vulnerability reporting,
   and verify the setting:

   ```sh
   gh api repos/tiepnguyen-sg/whymiss/private-vulnerability-reporting --jq .enabled
   ```

   It must print `true`.
5. Create and push the exact immutable tag. The release workflow reruns `make ci`,
   creates a draft release, signs binaries and the GHCR image, verifies SBOM and
   SLSA provenance, and only then removes the draft flag. A failed workflow must
   not be worked around by manually publishing its draft.

## Uninstalling completely

**Docker Compose** (from `deploy/docker/`):

```sh
docker compose down -v
```

Removes the containers, the network, and the named volumes (`whymiss-data`,
`prometheus-data`, `grafana-data`) — nothing whymiss created is left behind. Delete
the cloned repo directory and `deploy/docker/.env` yourself if you don't intend to
reinstall.

**systemd:**

```sh
sudo systemctl disable --now whymiss
sudo rm /etc/systemd/system/whymiss.service /usr/local/bin/whymiss
sudo rm -rf /etc/whymiss /var/lib/private/whymiss /var/lib/whymiss
sudo systemctl daemon-reload
```

`DynamicUser=yes` means there is no system user to remove — systemd already deleted
the ephemeral UID/GID the moment the unit stopped. `StateDirectory=` (the SQLite
store) is deliberately *not* auto-removed by systemd on stop — state is meant to
survive restarts — so it's the one thing this uninstall removes explicitly.

## Escalating

- A wrong or missing RCA verdict (not a security issue): file a GitHub issue with the
  slot number, `whymiss timeline <slot>` output, and what you expected — this is
  exactly what `test/corpus` scenarios are built from.
- A security vulnerability: follow `SECURITY.md`; do not open a public issue.
