#!/usr/bin/env bash
# @command observability:up
# @description Start local observability stack (Prometheus + Grafana)
set -euo pipefail

# extractTraefikDomains [service ...]
#   Prints unique Host() domains from running containers' Traefik labels.
#   Without args: all currently running compose services.
#   With args:    only the given services.
extractTraefikDomains() {
    local services svc cid
    if [ $# -eq 0 ]; then
        services="$(docker compose ps --services --filter status=running)"
    else
        services="$*"
    fi
    for svc in $services; do
        cid="$(docker compose ps -q "$svc" 2>/dev/null | head -n1)"
        [ -z "$cid" ] && continue
        docker inspect "$cid" --format '{{json .Config.Labels}}' \
            | grep -oE 'Host\(`[^`]+`\)' \
            | sed -E 's/Host\(`([^`]+)`\)/\1/'
    done | sort -u
}

docker compose --profile observability up -d "$@"

running="$(docker compose --profile observability ps --services --filter status=running)"

if [ -t 1 ]; then
    blue=$'\033[34m'
    reset=$'\033[0m'
else
    blue=""
    reset=""
fi

echo ""
# shellcheck disable=SC2086
domains="$(extractTraefikDomains $running)"
if [ -n "$domains" ]; then
    echo "Available at:"
    printf '%s\n' "$domains" | sed "s|^|  ${blue}https://|;s|$|${reset}|"
else
    # Fallback: running container but no Traefik labels (e.g. stack deployed
    # without the shared dde proxy). Point at the default published ports
    # so the stack is not silently "up but unreachable from the CLI hint".
    echo "Running. Default ports (published by docker compose):"
    echo "  ${blue}http://localhost:9090${reset}   # Prometheus"
    echo "  ${blue}http://localhost:3000${reset}   # Grafana"
fi
