#!/usr/bin/env bash
# @command observability:down
# @description Stop local observability stack
set -euo pipefail

cd "$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
checkout="$(pwd -P)"

# Resolve the compose project the same way observability:up does: by the
# checkout directory, so a worktree never stops the main checkout's stack.
mapfile -t projects < <(docker ps -a \
    --filter "label=com.docker.compose.project.working_dir=${checkout}" \
    --format '{{.Label "com.docker.compose.project"}}' | sort -u)
if [ "${#projects[@]}" -eq 0 ]; then
    echo "No containers running for ${checkout}." >&2
    exit 0
fi
if [ "${#projects[@]}" -ne 1 ]; then
    echo "Expected one compose project for ${checkout}, found: ${projects[*]}." >&2
    exit 1
fi

# `down` would also remove the app container that dde manages; stop and
# remove only the observability services.
docker compose -p "${projects[0]}" --profile observability rm --stop --force "$@" prometheus grafana
