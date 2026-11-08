#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
config="$root/examples/quickstart"
compose() {
    docker compose --env-file "$config/.env" -f "$config/compose.yaml" "$@"
}
case "${1:-start}" in
    start)
        docker build --target tools -f "$config/Dockerfile" -t ranjit-quickstart-tools "$root"
        docker run --rm --user "$(id -u):$(id -g)" -v "$config:/config" ranjit-quickstart-tools init
        compose up --build --force-recreate --wait --wait-timeout 120 ranjit backend receiver
        compose run --rm --no-deps verify
        ;;
    verify) compose run --rm --no-deps verify ;;
    stop) compose down ;;
    reset)
        # Explicitly requested demo reset. Keep the generated credentials.
        compose down --volumes
        ;;
    *) printf '%s\n' 'Usage: scripts/quickstart.sh [start|verify|stop|reset]' >&2; exit 2 ;;
esac
