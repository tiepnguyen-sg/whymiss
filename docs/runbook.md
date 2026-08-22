# Runbook

Operational guidance for running `whymiss watch` continuously against a live
validator. Read `README.md`'s Limitations section first — several entries here exist
because of gaps documented there.

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
# gap and needs a look — but it also fires on every healthy duty right now (see the
# known gap below), so don't alert on outcome="ok" until that's fixed.
- alert: WhymissUnknownCause
  expr: sum(increase(whymiss_duty_verdicts_total{cause="local.unknown.no_rule_matched", outcome=~"missed|degraded"}[1h])) > 0
  labels: {severity: info}
  annotations:
    summary: "whymiss saw a missed/degraded duty it couldn't explain — check docs/causes.md for a taxonomy gap"
```

The bundled Grafana dashboard (`deploy/grafana/whymiss-dashboard.json`) covers the
same data visually: missed/degraded duties by cause over time, an outcome breakdown,
and a "last hour" stat panel.

## Known-benign situations — not incidents

**Every healthy duty shows up as `local.unknown.no_rule_matched`.**
This is a known engine gap (`internal/rca`'s `Analyze` only short-circuits for
`OutcomeNoDuty`, not `OutcomeOK` — see `CHANGELOG.md`'s Phase 4 entry), not a sign
anything is actually wrong. Filter dashboards and alerts to `outcome=~"missed|degraded"`
until it's fixed; don't page on `outcome="ok"` regardless of cause.

**`whymiss watch` running with neither `--validator-index` nor `--metrics-addr` set.**
This is the default — a pure observation collector, no duty tracking, no exporter.
Expected if you're only using `whymiss <slot>`/`whymiss timeline <slot>` ad hoc rather
than continuous monitoring.

## Diagnosing whymiss itself

**No metrics appearing at `/metrics` after startup.**
1. Confirm `--validator-index` was actually set — `--metrics-addr` alone does nothing
   (`cmd/whymiss/watch.go`'s help text says so: "ignored unless --validator-index is
   set").
2. Metrics only appear once a tracked duty's slot has fully played out (assigned →
   watched → explained) — for attesters, allow at least `3 × SecondsPerSlot` past the
   duty's slot start before expecting a data point.
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

**The beacon node seems to be getting hammered by whymiss's requests.**
It shouldn't be — every call is rate-limited (`--min-request-interval`, default
200ms; I-5) with exponential backoff on an unhealthy node. If you suspect otherwise,
capture the request rate whymiss is actually issuing (e.g. from the beacon node's own
access log or its own Prometheus metrics) and compare against `--min-request-interval`
before assuming whymiss is the cause — most apparent request storms turn out to be
the validator client or another sidecar.

## Restarting and upgrading

`whymiss watch` is stateless between restarts except for the SQLite store — restarting
it (or upgrading the binary/image) loses no history and requires no migration step
beyond whatever `internal/store`'s own schema migration does automatically on open.
A duty in flight when whymiss restarts is simply not tracked for that one slot; it
resumes tracking from the next epoch boundary.

- **Docker Compose:** `docker compose pull && docker compose up -d` (or, building
  from source, `docker compose up -d --build`).
- **systemd:** replace `/usr/local/bin/whymiss`, then `sudo systemctl restart whymiss`.

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
- A security vulnerability: see `SECURITY.md` (task 4.8) once it lands; until then,
  do not open a public issue for a suspected vulnerability.
