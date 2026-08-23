#!/bin/sh
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
compose_file="$repo_root/deploy/docker/docker-compose.yml"
project_name="whymiss-freshinstall-$$"
env_file=$(mktemp)
mock_log=$(mktemp)
mock_pid=
compose_started=false

cleanup() {
	if [ "$compose_started" = true ]; then
		docker compose --project-name "$project_name" --env-file "$env_file" \
			-f "$compose_file" down -v >/dev/null 2>&1 || true
	fi
	if [ -n "$mock_pid" ]; then
		kill "$mock_pid" >/dev/null 2>&1 || true
		wait "$mock_pid" >/dev/null 2>&1 || true
	fi
	rm -f "$env_file" "$mock_log"
}
trap cleanup EXIT INT TERM

cp "$repo_root/deploy/docker/.env.example" "$env_file"
if docker compose --project-name "$project_name" --env-file "$env_file" \
	-f "$compose_file" config >/dev/null 2>&1; then
	echo ".env.example unexpectedly satisfies required operator configuration"
	exit 1
fi
sed -i.bak \
	-e 's#^WHYMISS_IMAGE=.*#WHYMISS_IMAGE=whymiss:local#' \
	-e 's#^BEACON_API=.*#BEACON_API=http://host.docker.internal:5052#' \
	-e 's#^CL_METRICS_API=.*#CL_METRICS_API=#' \
	-e 's#^VALIDATOR_INDICES=.*#VALIDATOR_INDICES=24#' \
	-e 's#^NTP_SERVER=.*#NTP_SERVER=time.cloudflare.com#' \
	-e 's#^GRAFANA_ADMIN_PASSWORD=.*#GRAFANA_ADMIN_PASSWORD=fresh-install-only#' \
	"$env_file"
rm -f "$env_file.bak"

python3 "$repo_root/test/freshinstall/mock_beacon.py" >"$mock_log" 2>&1 &
mock_pid=$!
ready=false
for _ in $(seq 1 10); do
	if curl -sf http://127.0.0.1:5052/eth/v1/beacon/genesis >/dev/null; then
		ready=true
		break
	fi
	sleep 1
done
if [ "$ready" != true ]; then
	cat "$mock_log"
	exit 1
fi

compose_started=true
docker compose --project-name "$project_name" --env-file "$env_file" \
	-f "$compose_file" build --quiet whymiss
docker compose --project-name "$project_name" --env-file "$env_file" \
	-f "$compose_file" up --no-build -d

ready=false
for _ in $(seq 1 30); do
	if curl -sf http://127.0.0.1:3000/api/health >/dev/null; then
		ready=true
		break
	fi
	sleep 2
done
if [ "$ready" != true ]; then
	echo "Grafana never became healthy"
	exit 1
fi

GRAFANA_ADMIN_PASSWORD=$(sed -n 's/^GRAFANA_ADMIN_PASSWORD=//p' "$env_file")
curl -sf -u "admin:${GRAFANA_ADMIN_PASSWORD}" http://127.0.0.1:3000/api/search \
	| grep -q whymiss-duty-causes

service_id=$(docker compose --project-name "$project_name" --env-file "$env_file" \
	-f "$compose_file" ps -q whymiss)
test -n "$service_id"
test "$(docker inspect --format '{{.Config.User}}' "$service_id")" = "65532:65532"
test "$(docker inspect --format '{{.HostConfig.ReadonlyRootfs}}' "$service_id")" = "true"
test "$(docker inspect --format '{{json .HostConfig.CapDrop}}' "$service_id")" = '["ALL"]'
test "$(docker inspect --format '{{json .HostConfig.SecurityOpt}}' "$service_id")" = '["no-new-privileges:true"]'
test "$(docker inspect --format '{{.HostConfig.Memory}}' "$service_id")" = "268435456"
test "$(docker inspect --format '{{.HostConfig.PidsLimit}}' "$service_id")" = "64"
test "$(docker inspect --format '{{json .HostConfig.PortBindings}}' "$service_id")" = '{}'

ready=false
for _ in $(seq 1 30); do
	state=$(docker inspect --format '{{.State.Status}}' "$service_id")
	restarts=$(docker inspect --format '{{.RestartCount}}' "$service_id")
	result=$(curl -sf -u "admin:${GRAFANA_ADMIN_PASSWORD}" -G \
		http://127.0.0.1:3000/api/datasources/proxy/uid/prometheus/api/v1/query \
		--data-urlencode 'query=up{job="whymiss"}' || true)
	if [ "$state" = running ] && [ "$restarts" = 0 ] \
		&& echo "$result" | grep -q '"value":\[[^]]*,"1"\]'; then
		ready=true
		break
	fi
	sleep 2
done
if [ "$ready" != true ]; then
	docker compose --project-name "$project_name" --env-file "$env_file" \
		-f "$compose_file" ps
	docker compose --project-name "$project_name" --env-file "$env_file" \
		-f "$compose_file" logs whymiss prometheus
	exit 1
fi

if docker run --rm --entrypoint /bin/sh whymiss:local -c true; then
	echo "distroless whymiss image unexpectedly contains /bin/sh"
	exit 1
fi

echo "fresh install passed"
