#!/usr/bin/env bash
# @command observability:down
# @description Stop local observability stack
set -euo pipefail
docker compose --profile observability down "$@"
