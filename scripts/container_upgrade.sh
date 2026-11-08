#!/bin/sh
# Actual released image -> candidate, using an isolated synthetic-data volume.
set -eu
candidate=${1:?Usage: scripts/container_upgrade.sh candidate-image}
volume="ranjit-upgrade-$$"
temporary=$(mktemp -d)
cleanup() {
    docker volume rm "$volume" >/dev/null 2>&1 || true
    rm -rf "$temporary"
}
trap cleanup EXIT HUP INT TERM
docker volume create "$volume" >/dev/null
docker run --rm -v "$volume:/data" ghcr.io/getcortexlabs/ranjit:v0.3.0 demo
docker run --rm -v "$volume:/data" ghcr.io/getcortexlabs/ranjit:v0.3.0 transactions --json > "$temporary/before.json"
docker run --rm -v "$volume:/data" ghcr.io/getcortexlabs/ranjit:v0.3.0 backup /data/before.db
docker run --rm -v "$volume:/data" "$candidate" doctor
docker run --rm -v "$volume:/data" "$candidate" transactions --json > "$temporary/after.json"
cmp "$temporary/before.json" "$temporary/after.json"
docker run --rm -v "$volume:/data" "$candidate" transactions --json > "$temporary/restarted.json"
cmp "$temporary/before.json" "$temporary/restarted.json"
printf '%s\n' 'Released container upgrade and restart preserved all transactions.'
