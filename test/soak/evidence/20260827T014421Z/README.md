# Soak evidence — 2026-08-27T01:44:21Z, 72 hours, PASS

The run that closed Phase 2's soak item. It is kept here so the numbers quoted in
`CHANGELOG.md` and `docs/BUILD_PROMPT.md` can be checked by a reader rather than
believed, and because the host it ran on has since been deleted.

Binary under test: `1d0bdd6d5660219b73f7bb10196222f6e67cfb0b5c7750d8bd60853f6421d08d`
(`v0.1.0-24-g0c0a94b-dirty`, built by go1.26.6). Target: the public Hoodi gateway
`ethereum-hoodi-beacon-api.publicnode.com`, validator index 0, with no
`--cl-metrics-api` and no baseline flags — so this measures the collector on the
Beacon API alone.

## Files

| File | What it is |
|---|---|
| `summary.txt` | Written by `test/soak/run.sh` itself, including the `result=` line |
| `samples.csv.gz` | 4321 samples, one a minute: RSS in KiB and database bytes |
| `whymiss.log.gz` | 18,006 log lines, the daemon's entire output |
| `BINARY.md` | The sha256 recorded on the host before the run started |
| `PHASE2_STATUS.txt` | The watcher's verdict, written when the daemon exited |

Not kept here: the 22.8 MB SQLite database and the 17 MB binary itself. Neither
is needed to check a claim below, and the binary is machine-code-identical to a
build of the same tree — the only bytes that differ are the Go and GNU build IDs,
the module pseudo-version, and `vcs.revision`/`vcs.time`.

## Checking the claims

`run.sh` decides pass/fail on its own, exiting non-zero the moment RSS crosses
262144 KiB or the database crosses 104857600 bytes:

```sh
grep -E 'result|max_' summary.txt
```

The ceilings, recomputed from the raw samples rather than trusted:

```sh
gunzip -c samples.csv.gz |
  awk -F, 'NR>1 {if ($3>r) r=$3; if ($4>d) d=$4} END {print "max_rss_kib="r, "max_database_bytes="d}'
```

Expect `max_rss_kib=34688` (13.2% of the ceiling) and
`max_database_bytes=25447600` (24.3% of the cap).

The error count, and why none of the 55 is whymiss's:

```sh
gunzip -c whymiss.log.gz | grep -c '"level":"ERROR"'
gunzip -c whymiss.log.gz | grep '"level":"ERROR"' |
  sed -E 's/.*"error":"([^"]*)".*/\1/' | sed -E 's/[0-9]{6,}/<slot>/g' | cut -c1-60 | sort | uniq -c
```

49 are the node behind the gateway lagging, 5 are the gateway returning 500, and
1 is the `context canceled` raised when the soak stopped the daemon at the end.

**`PHASE2_STATUS.txt` says `errors : 0`, and that number is wrong.** The watcher
grepped `level=ERROR` in logfmt while the daemon emits JSON. The script was fixed
afterwards; the file is kept exactly as it was written, because a status file
that once lied is worth showing next to the log that contradicts it.

The 17,275 `WARN` lines are one repeated message — the gateway answers
`/eth/v1/events` with `501` and the reconnect backoff retries for the life of the
run. `CHANGELOG.md` carries this as a known issue.
