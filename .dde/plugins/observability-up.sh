#!/usr/bin/env bash
# @command observability:up
# @description Start local observability stack (Prometheus + Grafana)
set -euo pipefail

# dde runs plugins as plain scripts: the runtime overlay that `dde project:up`
# applies (per-project network, worktree hostnames) is not in effect for a
# bare `docker compose` call. Derive both from the running app container
# instead, so Prometheus and Grafana join the same per-project network as the
# app (and can scrape it) and get routed under the checkout's own hostname.
# Without this, a git worktree would register the same Traefik routers as the
# main checkout and both stacks would share the global `dde` network.
app_cid="$(docker compose ps -q tsmetrics 2>/dev/null | head -n1)"
if [ -z "$app_cid" ]; then
    echo "App container is not running. Start it first: dde project:up" >&2
    exit 1
fi

network="$(docker inspect "$app_cid" --format '{{range $name, $_ := .NetworkSettings.Networks}}{{println $name}}{{end}}' \
    | grep -m1 '^dde-services-' || true)"
# shellcheck disable=SC2016 # backticks are literal Traefik rule syntax
app_host="$(docker inspect "$app_cid" --format '{{json .Config.Labels}}' \
    | grep -oE 'Host\(`[^`]+`\)' | head -n1 | sed -E 's/Host\(`([^`]+)`\)/\1/')"
if [ -z "$network" ] || [ -z "$app_host" ]; then
    echo "Could not detect dde network or hostname of the app container." >&2
    exit 1
fi

override="$(mktemp)"
trap 'rm -f "$override"' EXIT

# Router names follow the `<host with dots as dashes>-<service>` convention,
# identical to the labels in docker-compose.yml for the main checkout.
{
    echo "networks:"
    echo "    ${network}:"
    echo "        external: true"
    echo "services:"
    for entry in prometheus:9090 grafana:3000; do
        svc="${entry%%:*}"
        port="${entry##*:}"
        host="${svc}.${app_host}"
        router="${host//./-}-${svc}"
        echo "    ${svc}:"
        echo "        networks: !override"
        echo "            ${network}: {}"
        echo "        labels: !override"
        echo "            - 'traefik.enable=true'"
        echo "            - 'traefik.docker.network=${network}'"
        echo "            - 'traefik.http.routers.${router}.rule=Host(\`${host}\`)'"
        echo "            - 'traefik.http.routers.${router}.tls=true'"
        echo "            - 'traefik.http.services.${router}.loadbalancer.server.port=${port}'"
    done
} > "$override"

docker compose -f docker-compose.yml -f "$override" --profile observability up -d "$@"

if [ -t 1 ]; then
    blue=$'\033[34m'
    reset=$'\033[0m'
else
    blue=""
    reset=""
fi

echo ""
echo "Available at:"
for svc in "" prometheus. grafana.; do
    echo "  ${blue}https://${svc}${app_host}${reset}"
done
