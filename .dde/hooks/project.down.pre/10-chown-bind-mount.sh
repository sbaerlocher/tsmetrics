#!/usr/bin/env bash
# Hand root-owned files in the bind mount back to the host user before the
# stack goes away.
#
# dde starts the tsmetrics container as root, so anything written into the
# bind mount — Air's `bin/tsmetrics` build, `dde project:exec --root`, git
# inside the container — lands root-owned on the host and breaks host git
# (`.git/index`) and lefthook with EACCES.
#
# Runs in the tsmetrics container, which bind-mounts the repo at /app. In a
# git worktree dde also mounts the main clone's .git at its host path, so root
# git ops write there (e.g. .git/worktrees/<name>/index); that dir is included
# when it lies outside the project dir and is visible in the container.
#
# Anonymous-volume mountpoints are pruned: inside the container /app/tmp is the
# volume itself. Their host dirs are kept user-owned by
# project.up.pre/10-volume-mountpoints.sh.
#
# Best effort: when the container is not running, or root-owned files remain
# afterwards, a warning is printed and `project:down` continues.

set -euo pipefail

hook_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
project_dir=$(CDPATH='' cd -- "$hook_dir/../../.." && pwd -P)
cd "$project_dir"

retry_hint="run 'dde project:up && dde project:down' to retry"

in_app() {
	dde project:exec --root --service=tsmetrics -- "$@"
}

if ! in_app test -d /app >/dev/null 2>&1; then
	echo "warn: tsmetrics container not running, skipped chown of root-owned files in the bind mount; $retry_hint" >&2
	exit 0
fi

paths=(/app)
if common_dir=$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null) &&
	common_dir=$(CDPATH='' cd -- "$common_dir" && pwd -P); then
	case "$common_dir" in
	"$project_dir"/*) ;;
	*) in_app test -d "$common_dir" >/dev/null 2>&1 && paths+=("$common_dir") ;;
	esac
fi

# Container paths of the anonymous volumes nested in the repo bind mount at
# /app; same selection as project.up.pre/10-volume-mountpoints.sh.
prune=()
if mountpoints=$(docker compose config --format json | jq -r --arg root "$project_dir" '
	.services[] | (.volumes // []) as $vols
	| $vols[] | select(.type == "volume" and ((.source // "") == "")) | .target as $t
	| $vols[] | select(.type == "bind" and .source == $root and .target == "/app")
	| select($t | startswith("/app/")) | $t' | sort -u); then
	while IFS= read -r dir; do
		[ -n "$dir" ] || continue
		[ ${#prune[@]} -eq 0 ] || prune+=(-o)
		prune+=(-path "$dir")
	done <<<"$mountpoints"
else
	echo "warn: could not derive anonymous volume mountpoints (needs docker compose and jq); /app/tmp is not pruned" >&2
fi

match=(-user 0)
if [ ${#prune[@]} -gt 0 ]; then
	match=(\( "${prune[@]}" \) -prune -o -user 0)
fi

# Files that vanish mid-walk (Air rebuilds) make find exit non-zero; that is
# expected, so the result is checked by a second walk instead.
# `chown -h` changes a root-owned symlink itself instead of its target.
in_app find "${paths[@]}" "${match[@]}" -exec chown -h "$(id -u):$(id -g)" {} + >/dev/null 2>&1 || true

leftover=$(in_app find "${paths[@]}" "${match[@]}" -print 2>/dev/null || true)
if [ -n "$leftover" ]; then
	echo "warn: root-owned files remain in the bind mount; $retry_hint" >&2
	printf '%s\n' "$leftover" | head -n 10 >&2 || true
fi
