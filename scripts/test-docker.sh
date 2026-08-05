#!/bin/sh
set -eu

project="tyxnet-integration-$$"
version="integration-$$"
image="ghcr.io/fbeser/tyxnet:$version"
lan_port=${TYXNET_TEST_LAN_PORT:-28444}
tunnel_port=${TYXNET_TEST_TUNNEL_PORT:-29830}
http_port=${TYXNET_TEST_HTTP_PORT:-28080}
https_port=${TYXNET_TEST_HTTPS_PORT:-28443}
data_volume="$project-data"
caddy_data_volume="$project-caddy-data"
caddy_config_volume="$project-caddy-config"

if docker compose version >/dev/null 2>&1; then
	compose() {
		docker compose "$@"
	}
elif command -v docker-compose >/dev/null 2>&1; then
	compose() {
		docker-compose "$@"
	}
else
	echo "Docker Compose is required" >&2
	exit 1
fi

cleanup() {
	compose -p "$project" down >/dev/null 2>&1 || true
	docker volume rm "$data_volume" "$caddy_data_volume" "$caddy_config_volume" >/dev/null 2>&1 || true
	docker image rm "$image" >/dev/null 2>&1 || true
}

trap cleanup EXIT
trap 'exit 130' INT TERM

docker build -t "$image" -f packaging/docker/Dockerfile .
if ! docker run --rm --entrypoint sh --device /dev/net/tun:/dev/net/tun "$image" -c 'test -c /dev/net/tun'; then
	echo "/dev/net/tun is required for the Docker integration test" >&2
	exit 1
fi

export TYXNET_VERSION=$version
export TYXNET_DOMAIN=localhost
export TYXNET_PUBLIC_IP=
export TYXNET_LAN_PORT=$lan_port
export TYXNET_TUNNEL_PORT=$tunnel_port
export TYXNET_HTTP_CHALLENGE_PORT=$http_port
export TYXNET_HTTPS_PORT=$https_port
export TYXNET_DATA_VOLUME=$data_volume
export TYXNET_CADDY_DATA_VOLUME=$caddy_data_volume
export TYXNET_CADDY_CONFIG_VOLUME=$caddy_config_volume

compose -f docker-compose.yml config -q
compose -p "$project" up -d

container_id=$(compose -p "$project" ps -q tyxnet-server)
if [ -z "$container_id" ] || [ "$(compose -p "$project" ps -q | wc -l | tr -d ' ')" != 1 ]; then
	compose -p "$project" ps >&2
	exit 1
fi

attempt=0
while ! curl -kfsS --resolve "localhost:$https_port:127.0.0.1" "https://localhost:$https_port/" | grep -q TyxNet; do
	attempt=$((attempt + 1))
	if [ "$attempt" -ge 120 ]; then
		docker logs "$container_id" >&2
		exit 1
	fi
	sleep 0.5
done

processes=$(docker exec "$container_id" ps -o comm=)
printf '%s\n' "$processes" | grep -q tyxnet-server
printf '%s\n' "$processes" | grep -q caddy

docker stop --time 10 "$container_id" >/dev/null
test "$(docker inspect --format '{{.State.Status}}' "$container_id")" = exited
test "$(docker inspect --format '{{.State.ExitCode}}' "$container_id")" = 0

echo "single-container startup, HTTPS proxy, and graceful shutdown passed"
