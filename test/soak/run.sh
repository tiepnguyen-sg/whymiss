#!/bin/sh
set -eu
umask 077

if [ "$(uname -s)" != "Linux" ]; then
	echo "test.soak requires Linux so RSS can be measured from /proc" >&2
	exit 1
fi

: "${BEACON_API:?set BEACON_API to a Hoodi beacon API URL}"
: "${VALIDATOR_INDICES:?set VALIDATOR_INDICES to one or more comma-separated Hoodi validator indices}"
: "${NTP_SERVER:?set NTP_SERVER to an operator-approved NTP server}"

duration_seconds=${SOAK_DURATION_SECONDS:-259200}
sample_seconds=${SOAK_SAMPLE_SECONDS:-60}
rss_limit_kib=${SOAK_RSS_LIMIT_KIB:-262144}
db_limit_bytes=${SOAK_DB_LIMIT_BYTES:-104857600}
output_dir=${SOAK_OUTPUT_DIR:-soak-results/$(date -u +%Y%m%dT%H%M%SZ)}

validate_positive() {
	validation_name=$1
	validation_value=$2
	case "$validation_value" in
		''|*[!0-9]*|0)
			echo "$validation_name must be a positive integer, got: $validation_value" >&2
			exit 1
			;;
	esac
}

validate_positive duration_seconds "$duration_seconds"
validate_positive sample_seconds "$sample_seconds"
validate_positive rss_limit_kib "$rss_limit_kib"
validate_positive db_limit_bytes "$db_limit_bytes"

if [ -e "$output_dir" ]; then
	echo "refusing to overwrite existing soak output: $output_dir" >&2
	exit 1
fi
mkdir -p "$output_dir"

db_path=$output_dir/whymiss.db
log_path=$output_dir/whymiss.log
samples_path=$output_dir/samples.csv
summary_path=$output_dir/summary.txt
pid=
process_status=0

process_is_running() {
	[ -n "$pid" ] || return 1
	kill -0 "$pid" 2>/dev/null || return 1
	process_state=$(awk '{print $3}' "/proc/$pid/stat" 2>/dev/null || true)
	[ "$process_state" != "Z" ]
}

stop_process() {
	if [ -z "$pid" ]; then
		return
	fi
	if process_is_running; then
		kill -TERM "$pid" 2>/dev/null || true
	fi
	i=0
	while process_is_running && [ "$i" -lt 15 ]; do
		sleep 1
		i=$((i + 1))
	done
	if process_is_running; then
		kill -KILL "$pid" 2>/dev/null || true
	fi
	process_status=0
	wait "$pid" 2>/dev/null || process_status=$?
}

cleanup() {
	stop_process
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM

set -- ./bin/whymiss \
	--beacon-api "$BEACON_API" \
	--db "$db_path" \
	watch \
	--validator-index "$VALIDATOR_INDICES" \
	--ntp-server "$NTP_SERVER" \
	--retention-max-bytes "$db_limit_bytes" \
	--retention-interval 5m

if [ -n "${CL_METRICS_API:-}" ]; then
	set -- "$@" --cl-metrics-api "$CL_METRICS_API"
fi
if [ -n "${BASELINE_BEACON_API:-}" ]; then
	set -- "$@" --baseline-beacon-api "$BASELINE_BEACON_API"
fi
if [ -n "${BASELINE_METRICS_API:-}" ]; then
	set -- "$@" --baseline-metrics-api "$BASELINE_METRICS_API"
fi
# The exporter runs its own HTTP listener and per-cause metric goroutines for the
# whole run. Leaving it off means a 72-hour soak never exercises the one
# long-lived server this daemon owns, so a leak there would ship unmeasured.
# Bind it to a loopback address: the soak host may have a public interface, and
# the metrics endpoint is unauthenticated by design.
if [ -n "${METRICS_ADDR:-}" ]; then
	set -- "$@" --metrics-addr "$METRICS_ADDR"
fi

"$@" >"$log_path" 2>&1 &
pid=$!
start_epoch=$(date +%s)
deadline=$((start_epoch + duration_seconds))
max_rss_kib=0
max_db_bytes=0
samples=0

printf 'sampled_at_utc,elapsed_seconds,rss_kib,database_bytes\n' >"$samples_path"
printf '%s\n' \
	"started_at_utc=$(date -u +%FT%TZ)" \
	"duration_seconds=$duration_seconds" \
	"sample_seconds=$sample_seconds" \
	"rss_limit_kib=$rss_limit_kib" \
	"db_limit_bytes=$db_limit_bytes" \
	"pid=$pid" >"$summary_path"

while :; do
	now=$(date +%s)
	if ! process_is_running; then
		process_status=0
		wait "$pid" || process_status=$?
		echo "whymiss exited before soak completion with status $process_status; see $log_path" >&2
		exit 1
	fi

	rss_kib=$(awk '/^VmRSS:/ {print $2}' "/proc/$pid/status")
	case "$rss_kib" in
		''|*[!0-9]*)
			echo "could not read VmRSS for process $pid" >&2
			exit 1
			;;
	esac

	db_bytes=0
	for file in "$db_path" "$db_path-wal" "$db_path-shm"; do
		if [ -f "$file" ]; then
			file_bytes=$(stat -c %s "$file")
			db_bytes=$((db_bytes + file_bytes))
		fi
	done

	[ "$rss_kib" -le "$max_rss_kib" ] || max_rss_kib=$rss_kib
	[ "$db_bytes" -le "$max_db_bytes" ] || max_db_bytes=$db_bytes
	samples=$((samples + 1))
	printf '%s,%s,%s,%s\n' "$(date -u +%FT%TZ)" "$((now - start_epoch))" "$rss_kib" "$db_bytes" >>"$samples_path"

	if [ "$rss_kib" -gt "$rss_limit_kib" ]; then
		echo "RSS limit exceeded: $rss_kib KiB > $rss_limit_kib KiB" >&2
		exit 1
	fi
	if [ "$db_bytes" -gt "$db_limit_bytes" ]; then
		echo "database byte limit exceeded: $db_bytes > $db_limit_bytes" >&2
		exit 1
	fi
	if [ "$now" -ge "$deadline" ]; then
		break
	fi
	sleep "$sample_seconds"
done

stop_process
if [ "$process_status" -ne 0 ]; then
	echo "whymiss did not shut down cleanly: status $process_status; see $log_path" >&2
	exit 1
fi
pid=
printf '%s\n' \
	"completed_at_utc=$(date -u +%FT%TZ)" \
	"samples=$samples" \
	"max_rss_kib=$max_rss_kib" \
	"max_database_bytes=$max_db_bytes" \
	"result=PASS" >>"$summary_path"
echo "soak PASS; summary: $summary_path"
