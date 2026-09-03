# Soak evidence — 2026-08-30T10:32:11Z, 72 hours, PASS

The release soak for v0.3.0. Kept so the figures quoted in `CHANGELOG.md` and
`docs/BUILD_PROMPT.md` can be checked rather than believed, and because the host
it ran on is disposable.

Binary under test: `99210b69c79b73f2245066db9afe0102804e832c5a62dd98429a40787967cf6d`
(`v0.2.1-9-gf0bcbb3`, built by go1.26.6). Target: the public Hoodi gateway
`ethereum-hoodi-beacon-api.publicnode.com`, validator index 0, with no
`--cl-metrics-api` and no baseline flags.

**Why that shape matters this time.** v0.3.0 carries the post-ePBS collection
fix, ADR-0026's schedule adoption, and ADR-0027's `network.payload_late`. On
Hoodi all three are inert: Gloas is unscheduled there, so the node-derived
schedule is exactly `domain.MainnetPreEPBS()` and R-120 declines on every duty.
That inertness is the property being soaked. None of the ePBS work may change
behaviour on a network that has not forked.

## Files

| File | What it is |
|---|---|
| `summary.txt` | Written by `test/soak/run.sh` itself, including the `result=` line |
| `samples.csv.gz` | 4321 samples, one a minute: RSS in KiB and database bytes |
| `whymiss.log.gz` | 1026 log lines, the daemon's entire output |
| `BINARY.md` | The sha256 recorded on the host before the run started |
| `SOAK_V030_STATUS.txt` | The watcher's verdict, written when the daemon exited |

Not kept: the 22.8 MB SQLite database. Nothing below needs it.

## Checking the claims

`run.sh` decides pass/fail itself, exiting non-zero the moment RSS crosses
262144 KiB or the database crosses 104857600 bytes:

```sh
grep -E 'result|max_' summary.txt
```

The ceilings, recomputed from the raw samples rather than trusted:

```sh
gunzip -c samples.csv.gz |
  awk -F, 'NR>1 {if ($3>r) r=$3; if ($4>d) d=$4} END {print "max_rss_kib="r, "max_database_bytes="d}'
```

Expect `max_rss_kib=34076` (13.0% of the ceiling) and
`max_database_bytes=25361656` (24.2% of the cap), over 673 verdicts.

Every one of the 55 errors, and that none of them is whymiss's:

```sh
gunzip -c whymiss.log.gz | grep -c '"level":"ERROR"'
gunzip -c whymiss.log.gz | grep '"level":"ERROR"' |
  sed -E 's/.*"msg":"([^"]*)".*/\1/' | sort | uniq -c
```

45 are `poll block_seen` failing because the node behind the public gateway was
not fully synced and execution-valid past the slot; 10 are `check inclusion`
failing on the gateway's own `/eth/v1/beacon/headers/head`. **No `context
canceled` line appears at shutdown**, which the previous release soak did produce
— that fix landed in this build.

## The log-volume fix, measured over a full run

The previous release soak wrote 18,006 lines against the same gateway, 17,275 of
them the same warning, because it answers `/eth/v1/events` with `501` and the
reconnect loop retries for the life of the process. This run, same gateway, same
72 hours:

| | v0.2.1 soak | this run |
|---|---|---|
| Log lines | 18,006 | **1,026** |
| Log size | 2.79 MB | **180 KB** |
| Stream warnings | 17,275 | **296** |

The 296 split as 13 `event stream error, reconnecting` and 283 `event stream
still failing, still reconnecting`: thirteen distinct failure onsets, each then
repeated at the fifteen-minute reminder interval rather than every fifteen
seconds.

## The binary is the one being released

Its sha does not match a fresh build, because `-buildvcs` stamps the commit and
HEAD moved after it. Rebuilding HEAD for `linux/amd64` with this binary's own
version string gives the same size, 17068194 bytes, differing in **190 bytes
across 5 regions, all of them build metadata**: the build IDs, the module
pseudo-version, and `vcs.revision` with `vcs.time`. No instruction byte differs,
and `git diff f0bcbb3..HEAD -- cmd internal` excluding tests is empty.
