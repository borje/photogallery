#!/usr/bin/env bash
# Throwaway Smugbox server for load testing: the production image, a fresh
# volume, and one API key printed on stdout.
#
#   ./testserver.sh up          # build if needed, start, mint an API key
#   ./testserver.sh down        # remove the container and its data volume
#   ./testserver.sh reset       # down + up
#   ./testserver.sh key         # mint another API key
#   ./testserver.sh logs [-f]   # server log
#   ./testserver.sh stats       # live CPU/memory of the container
#   ./testserver.sh env         # print the saved URL and key as exports
#
# `up` writes the URL and key to .testserver.env next to this script;
# smugbox_loadtest.py reads that file when no --url/--api-key is given.
#
# Plain `docker` on purpose: this host has no compose plugin, and the stack
# is a single container anyway.
set -euo pipefail

NAME=${SMUGBOX_TEST_NAME:-smugbox-loadtest}
IMAGE=${SMUGBOX_TEST_IMAGE:-smugbox:loadtest}
PORT=${SMUGBOX_TEST_PORT:-8099}
VOLUME=$NAME-data
CPUS=${SMUGBOX_TEST_CPUS:-}
MEMORY=${SMUGBOX_TEST_MEMORY:-}
MAX_UPLOAD_MB=${MAX_UPLOAD_MB:-100}
LOG_LEVEL=${LOG_LEVEL:-info}

here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd -- "$here/../.." && pwd)
envfile=$here/.testserver.env
url=http://127.0.0.1:$PORT

log() { printf '%s\n' "$*" >&2; }

build_if_missing() {
	if [ "${1:-}" != "--rebuild" ] && docker image inspect "$IMAGE" >/dev/null 2>&1; then
		log "using existing image $IMAGE (pass --rebuild to rebuild)"
		return
	fi
	log "building $IMAGE from $repo/deploy/Dockerfile (this takes a while)"
	docker build -f "$repo/deploy/Dockerfile" -t "$IMAGE" "$repo"
}

mint_key() {
	local out
	if ! out=$(docker exec "$NAME" smugbox admin create-api-key --label "${1:-loadtest}" 2>&1); then
		log "admin create-api-key failed in container $NAME:"
		log "$out"
		if printf '%s' "$out" | grep -q "executable file not found"; then
			log "image $IMAGE has no smugbox binary - it predates this tree."
			log "rebuild it with: $0 up --rebuild"
		fi
		return 1
	fi
	# "API key:     <plain>" is the line to keep; see cmd/smugbox/admin.go.
	printf '%s\n' "$out" | sed -n 's/^API key: *//p'
}

wait_healthy() {
	local i
	for i in $(seq 1 120); do
		if docker exec "$NAME" wget -qO- http://127.0.0.1:8080/api/healthz 2>/dev/null | grep -q '"ok"'; then
			return 0
		fi
		if ! docker inspect -f '{{.State.Running}}' "$NAME" 2>/dev/null | grep -q true; then
			log "container exited during startup:"
			docker logs "$NAME" >&2 || true
			return 1
		fi
		sleep 0.5
	done
	log "server did not become healthy in 60s"
	docker logs --tail 50 "$NAME" >&2 || true
	return 1
}

cmd_up() {
	build_if_missing "${1:-}"
	cmd_down >/dev/null 2>&1 || true
	local limits=()
	if [ -n "$CPUS" ]; then limits+=(--cpus "$CPUS"); fi
	if [ -n "$MEMORY" ]; then limits+=(--memory "$MEMORY"); fi
	docker volume create "$VOLUME" >/dev/null
	docker run -d --name "$NAME" \
		-p "$PORT:8080" \
		-v "$VOLUME:/data" \
		-e DATA_DIR=/data \
		-e FRONTEND_DIR=/srv/frontend \
		-e LOG_FORMAT=text \
		-e LOG_LEVEL="$LOG_LEVEL" \
		-e PUBLIC_BASE_URL="$url" \
		-e SITE_TITLE="Smugbox load test" \
		-e MAX_UPLOAD_MB="$MAX_UPLOAD_MB" \
		"${limits[@]}" \
		"$IMAGE" serve >/dev/null
	wait_healthy
	local key
	key=$(mint_key loadtest) || return 1
	if [ -z "$key" ]; then
		log "could not read the API key from admin create-api-key output"
		return 1
	fi
	umask 077
	cat > "$envfile" <<EOF
SMUGBOX_URL=$url
SMUGBOX_API_KEY=$key
EOF
	log "smugbox up at $url (container $NAME, volume $VOLUME)"
	cmd_env
}

cmd_down() {
	docker rm -f "$NAME" >/dev/null 2>&1 || true
	docker volume rm -f "$VOLUME" >/dev/null 2>&1 || true
	rm -f "$envfile"
	log "removed container $NAME and volume $VOLUME"
}

cmd_env() {
	[ -f "$envfile" ] || { log "no $envfile; run: $0 up"; return 1; }
	sed 's/^/export /' "$envfile"
}

case "${1:-}" in
	up)    shift; cmd_up "${1:-}" ;;
	down)  cmd_down ;;
	reset) cmd_down; cmd_up "${2:-}" ;;
	key)   mint_key "${2:-loadtest}" ;;
	logs)  shift; docker logs "$@" "$NAME" ;;
	stats) docker stats "$NAME" ;;
	env)   cmd_env ;;
	url)   printf '%s\n' "$url" ;;
	*)     sed -n '2,17p' "$0" | sed 's/^# \{0,1\}//'; exit 2 ;;
esac
