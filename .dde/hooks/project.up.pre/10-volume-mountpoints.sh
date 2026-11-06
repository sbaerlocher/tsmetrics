#!/usr/bin/env bash
# Pre-create the host-side mountpoints of the anonymous volumes that sit inside
# a bind mount of this repo (currently tsmetrics: /app/tmp).
#
# When such a mountpoint is missing, Docker creates it as root, leaving a
# root-owned ./tmp on the host. Host tools then fail with EACCES (lefthook,
# git), and the dir cannot be fixed from inside the container because the
# volume shadows it. Creating the dirs as the host user first keeps them
# user-owned; the volume content itself stays inside the container.
#
# An already root-owned mountpoint is normally empty on the host (the data lives
# in the volume), so it is removed and recreated; the parent dir is
# user-owned, which makes the rmdir legal. A non-empty one gets a hint instead.
#
# The list is derived from `docker compose config`, so new anonymous volumes
# are picked up without touching this hook. Runs after 00-env.sh, which
# provides the .env that `docker compose config` needs. Best effort: a failure
# here warns but never blocks `project:up`.

set -euo pipefail

hook_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
project_dir=$(CDPATH='' cd -- "$hook_dir/../../.." && pwd -P)
cd "$project_dir"

# Relative host paths of anonymous volumes nested in a bind mount of the repo.
if ! mountpoints=$(docker compose config --format json | jq -r --arg root "$project_dir" '
	.services[] | (.volumes // []) as $vols
	| $vols[] | select(.type == "volume" and ((.source // "") == "")) | .target as $t
	| $vols[] | select(.type == "bind") | .target as $b
	| select($t | startswith($b + "/"))
	| (.source + "/" + ($t | ltrimstr($b + "/")))
	| select(startswith($root + "/")) | ltrimstr($root + "/")' | sort -u); then
	echo "warn: could not derive anonymous volume mountpoints from docker compose config" >&2
	exit 0
fi

while IFS= read -r dir; do
	[ -n "$dir" ] || continue
	if [ -d "$dir" ] && [ ! -O "$dir" ] && ! rmdir -- "$dir" 2>/dev/null; then
		echo "warn: $dir is not owned by you and not empty; fix once with:" >&2
		echo "  docker run --rm -v \"$project_dir/$dir:/w\" alpine chown -R $(id -u):$(id -g) /w" >&2
		continue
	fi
	mkdir -p -- "$dir"
done <<<"$mountpoints"
