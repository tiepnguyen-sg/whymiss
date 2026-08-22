# Configuration

whymiss is configured entirely through command-line flags for now — no
config file yet. `koanf` (BUILD_PROMPT.md §3) lands with the CLI polish
phase (Phase 4), once a config file's multi-source precedence (file, env,
flag) is actually needed; until then, every flag documented here has a
safe built-in default and a plain string/duration/int value is enough.

This file exists to satisfy BUILD_PROMPT.md §10.3's Phase 2 DoD: every
option, its default, and its safe range. See [architecture.md](architecture.md)
for how these pieces fit together.

## Global flags

Accepted by every subcommand (`whymiss --beacon-api ... watch`, not just
`watch --beacon-api ...`).

| Flag | Default | Safe range | Notes |
|---|---|---|---|
| `--beacon-api` | *(none — required by `watch`)* | Any reachable beacon node base URL, e.g. `http://127.0.0.1:5052` | Standard Beacon API only (I-1: read-only). No auth is sent; if your node requires it, put it in the URL or run whymiss behind the same trust boundary as the node. |
| `--db` | `whymiss.db` | Any writable path on a filesystem with room for the retention budget below | Single SQLite file (ADR-0002, ADR-0007). Back it up with a plain `cp` while whymiss is stopped, or `sqlite3 whymiss.db ".backup ..."` while running. |

## `whymiss watch`

The collector daemon. Runs until `SIGINT`/`SIGTERM`.

| Flag | Default | Safe range | Notes |
|---|---|---|---|
| `--min-request-interval` | `200ms` | `100ms`–`2s` | Floor between successive beacon API requests (I-5). Below ~100ms risks competing with the node's own duties for CPU/IO on a Raspberry Pi 5; above a couple of seconds, per-validator duty polling (below) would miss its own deadlines. |
| `--host-sample-interval` | `10s` | `0` (disabled) or `5s`–`60s` | How often disk/memory/CPU pressure is sampled (`internal/source/hostmetrics`). `0` disables host sampling entirely — meaningful when whymiss doesn't run on the staking box itself, or on a platform without `/proc` (I-3: this degrades cleanly, not a crash). Below 5s adds sampling overhead for little extra signal on a metric that itself averages over 10s (PSI's own `avg10`). |
| `--retention-max-age` | `336h` (14 days) | `24h`–`2160h` (90 days) | `store.Prune`'s age limit (I-12). Shorter than a day makes post-mortems on a miss discovered the next morning impossible; longer than ~90 days is rarely useful once the byte cap below has already forced pruning first. |
| `--retention-max-bytes` | `1073741824` (1 GiB) | `104857600` (100 MiB) – `10737418240` (10 GiB) | `store.Prune`'s byte limit (I-12) — the harder floor on a Raspberry Pi 5's typical SD card/SSD budget. A degraded node emits far more observations per hour than a healthy one, so this is what actually bounds disk use during the incident an operator most wants recorded, not the age limit above. |
| `--retention-interval` | `1h` | `0` (disabled) or `5m`–`24h` | How often retention runs. `0` disables pruning entirely — do not run this in production; it exists for short-lived debugging sessions where the store is thrown away afterward anyway. |
| `--validator-index` | *(none)* | Any valid validator index; repeatable | Tracks this validator's attester duties: fetches each one, polls its outcome, runs it through the RCA engine, and records the verdict for `--metrics-addr` to export. Empty (the default) disables duty tracking and the exporter entirely — `watch` still runs as a pure observation collector, exactly as before task 4.1. |
| `--metrics-addr` | *(none)* | An address `net.Listen` accepts, e.g. `:9101` or `127.0.0.1:9101` | Serves Prometheus metrics (`whymiss_duty_verdicts_total`, see ADR-0009) at `<addr>/metrics`. Ignored unless `--validator-index` is set at least once — there's nothing to export otherwise. Bind to `127.0.0.1` unless your Prometheus scraper is on a different host and you've otherwise secured the port (I-4: no egress by default, but this is an inbound listener the operator opts into). |

## `whymiss timeline <slot>`

Prints the raw recorded facts for one slot — no interpretation (Phase 3's
RCA engine is what interprets).

| Flag | Default | Safe range | Notes |
|---|---|---|---|
| `--format` | `json` | `json` (only value supported today) | A human-readable table format is Phase 4 scope, alongside `whymiss <slot>`'s full report. |
