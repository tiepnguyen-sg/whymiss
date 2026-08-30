# Configuration

Configuration precedence is deterministic:

1. built-in defaults;
2. one YAML file selected with `--config`;
3. `WHYMISS_*` environment variables;
4. explicitly supplied CLI flags.

Unknown YAML keys, duplicate keys, multiple YAML documents, malformed values, and
unsafe RCA thresholds fail startup. Empty optional values disable their source; they
never trigger implicit network access.

## Complete YAML example

```yaml
beacon_api: http://127.0.0.1:5052
db: /var/lib/whymiss/whymiss.db

watch:
  min_request_interval: 200ms
  host_sample_interval: 10s
  cl_metrics_api: http://127.0.0.1:5054/metrics
  peer_sample_interval: 15s
  baseline_beacon_api: ""
  baseline_metrics_api: ""
  ntp_servers: [pool.ntp.org]
  clock_sample_interval: 1m
  retention_max_age: 336h
  retention_max_bytes: 1073741824
  retention_interval: 1h
  validator_indices: [24, 40]
  metrics_addr: 127.0.0.1:9101

schedule:
  seconds_per_slot: 12s
  attestation_deadline: 4s
  aggregation_deadline: 8s
  # Post-ePBS only, and commented out on purpose: this example is meant to be
  # safe to copy onto a node running today, where the payload ships with the
  # block and neither deadline exists. Illustrative values, not spec constants.
  # payload_reveal_deadline: 6s
  # ptc_deadline: 9s

thresholds:
  dominance: 0.5
  clock_offset_max: 100ms
  clock_sample_max_age: 2m
  network_deviation: 750ms
  engine_spike_multiplier: 3.0
  peer_count_min: 40
  iowait_pct: 20.0
  cpu_steal_pct: 5.0
  psi_mem_avg10: 10.0
```

Run the same file through preflight and the daemon:

```sh
whymiss --config /etc/whymiss/config.yaml doctor
whymiss --config /etc/whymiss/config.yaml watch
```

## Global options

| YAML / flag | Environment | Default | Constraint |
|---|---|---:|---|
| `beacon_api` / `--beacon-api` | `WHYMISS_BEACON_API` | empty | Required by `watch` and `doctor`; absolute HTTP(S) URL without credentials, query, or fragment. |
| `db` / `--db` | `WHYMISS_DB` | `whymiss.db` | Writable file path. Physical byte accounting includes SQLite WAL and SHM sidecars. |
| `--config` | — | empty | One strict YAML document. |

Authenticated URLs are rejected so credentials cannot leak into logs. Put whymiss
inside the node's trust boundary or use a local reverse proxy that injects auth.

## Watch options

| YAML key / flag | Environment | Default | Safe range |
|---|---|---:|---|
| `watch.min_request_interval` / `--min-request-interval` | `WHYMISS_MIN_REQUEST_INTERVAL` | `200ms` | `100ms`–`2s` |
| `watch.host_sample_interval` / `--host-sample-interval` | `WHYMISS_HOST_SAMPLE_INTERVAL` | `10s` | `0` or `5s`–`60s` |
| `watch.cl_metrics_api` / `--cl-metrics-api` | `WHYMISS_CL_METRICS_API` | empty | Absolute HTTP(S) URL. Supplies measured block-arrival timing and Engine-call durations — the inputs every timing-based cause is attributed from. Empty means no stage of a duty is timed, so `local.cl_slow`, `local.el_slow`, `local.vc_*`, `network.late_block`, and `local.p2p_degraded` can never be reported (ADR-0024). It no longer governs peer sampling, which reads `/eth/v1/node/peer_count` (ADR-0023) |
| `watch.peer_sample_interval` / `--peer-sample-interval` | `WHYMISS_PEER_SAMPLE_INTERVAL` | `15s` | `5s`–`60s`. How often the watched node's connected peer count is read from `/eth/v1/node/peer_count` |
| `watch.baseline_beacon_api` / `--baseline-beacon-api` | `WHYMISS_BASELINE_BEACON_API` | empty | Absolute HTTP(S) URL of a **different** beacon node you can reach; empty disables the network baseline. Sufficient on its own |
| `watch.baseline_metrics_api` / `--baseline-metrics-api` | `WHYMISS_BASELINE_METRICS_API` | empty | That same node's Prometheus endpoint. **Optional** — without it the baseline is polled from the node's own `/eth/v1/beacon/headers/{slot}` at 500ms resolution instead of read from its metrics at millisecond resolution (ADR-0025). Set it when the baseline node is yours |
| `watch.ntp_servers` / `--ntp-server` | `WHYMISS_NTP_SERVERS` | empty | Non-empty hostnames/IPs; YAML list, comma-separated env, repeatable flag |
| `watch.clock_sample_interval` / `--clock-sample-interval` | `WHYMISS_CLOCK_SAMPLE_INTERVAL` | `1m` | `10s`–`1m` when NTP is enabled |
| `watch.retention_max_age` / `--retention-max-age` | `WHYMISS_RETENTION_MAX_AGE` | `336h` | `24h`–`2160h` when retention is enabled |
| `watch.retention_max_bytes` / `--retention-max-bytes` | `WHYMISS_RETENTION_MAX_BYTES` | `1073741824` | 100 MiB–10 GiB when retention is enabled |
| `watch.retention_interval` / `--retention-interval` | `WHYMISS_RETENTION_INTERVAL` | `1h` | `0` or `5m`–`24h`; `0` disables pruning and is unsafe for long-running production |
| `watch.validator_indices` / `--validator-index` | `WHYMISS_VALIDATOR_INDICES` | empty | At most 64 unique indices; YAML list, comma-separated env, repeatable flag |
| `watch.metrics_addr` / `--metrics-addr` | `WHYMISS_METRICS_ADDR` | empty | `net.Listen` address; empty disables metrics; prefer loopback |

No NTP server means no clock egress and no timing attribution: affected verdicts are
`unknown.insufficient_data`. Host sampling degrades cleanly when `/proc` metrics are
unavailable. The CL metrics endpoint is optional and only corroborates peer health.

The baseline pair must describe an independent node on the same chain: startup
rejects equivalent watched/baseline Beacon API URLs and mismatched genesis time or
slot duration. Its Prometheus response must contain both the arrival gauge and a
matching `beacon_head_slot`; stale or cross-slot latest-value gauges are discarded.

## Slot schedule

These values are data, not hard-coded RCA constants. Change them only to match the
active consensus specification.

| YAML key | Environment | Default | Constraint |
|---|---|---:|---|
| `schedule.seconds_per_slot` | `WHYMISS_SECONDS_PER_SLOT` | `12s` | Positive, at most 1m |
| `schedule.attestation_deadline` | `WHYMISS_ATTESTATION_DEADLINE` | `4s` | Positive and not after aggregation deadline |
| `schedule.aggregation_deadline` | `WHYMISS_AGGREGATION_DEADLINE` | `8s` | At or before slot end |
| `schedule.payload_reveal_deadline` | `WHYMISS_PAYLOAD_REVEAL_DEADLINE` | `0s` (off) | After the attestation deadline, at or before slot end |
| `schedule.ptc_deadline` | `WHYMISS_PTC_DEADLINE` | `0s` (off) | After the payload-reveal deadline, at or before slot end; rejected if that deadline is unset |

The last two describe a fork that separates the consensus block from the
execution payload (EIP-7732). **Both default to off, and whymiss ships no
post-ePBS defaults at all** — a plausible-looking constant compiled in would be
indistinguishable from a measured one at the point where it produced a wrong
verdict, and the spec values are not final. On such a network an operator sets
them from that network's own specification; everywhere else they stay unset and
nothing about the timing model changes.

Setting `ptc_deadline` alone is a configuration error, not a partial
configuration: `whymiss` refuses to start rather than attribute lateness against
a payload deadline nobody supplied.

## RCA thresholds

| YAML key | Environment | Default | Accepted range |
|---|---|---:|---|
| `thresholds.dominance` | `WHYMISS_DOMINANCE` | `0.5` | `0.5`–`0.9` |
| `thresholds.clock_offset_max` | `WHYMISS_CLOCK_OFFSET_MAX` | `100ms` | `10ms`–`1s` |
| `thresholds.clock_sample_max_age` | `WHYMISS_CLOCK_SAMPLE_MAX_AGE` | `2m` | `30s`–`10m` |
| `thresholds.network_deviation` | `WHYMISS_NETWORK_DEVIATION` | `750ms` | `50ms`–`5s` |
| `thresholds.engine_spike_multiplier` | `WHYMISS_ENGINE_SPIKE_MULTIPLIER` | `3.0` | `1.1`–`20` |
| `thresholds.peer_count_min` | `WHYMISS_PEER_COUNT_MIN` | `40` | `1`–`500` |
| `thresholds.iowait_pct` | `WHYMISS_IOWAIT_PCT` | `20.0` | `0`–`100` |
| `thresholds.cpu_steal_pct` | `WHYMISS_CPU_STEAL_PCT` | `5.0` | `0`–`100` |
| `thresholds.psi_mem_avg10` | `WHYMISS_PSI_MEM_AVG10` | `10.0` | `0`–`100` |

`thresholds.iowait_pct` is a compatibility name: whymiss compares it with Linux
PSI I/O `some avg10`, not `/proc/stat` CPU iowait.

`doctor` uses the configured request interval and clock-offset threshold, so its
result matches the settings `watch` will enforce.

`doctor` checks every endpoint that is configured, not only the ones collection
needs. `cl_metrics_api`, `baseline_beacon_api`, and `baseline_metrics_api` are
contacted and reported on, because leaving them unset is what decides whether a
cause can ever be attributed rather than whether whymiss runs. Left unset they
report `WARN` naming what becomes unreportable; configured and unreachable they
report `FAIL`, since the operator asked for something that is not there. Only a
`FAIL` makes the command exit non-zero, so a deliberately minimal deployment
still passes.

## Other commands

`whymiss <slot>` supports `--format markdown|json`. `whymiss timeline <slot>`
supports `--format json`. Both use the configured database, slot schedule, and RCA
thresholds; neither performs network access.
