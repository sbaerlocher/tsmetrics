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
echo "Available at:"
# shellcheck disable=SC2086
extractTraefikDomains $running | sed "s|^|  ${blue}https://|;s|$|${reset}|"
