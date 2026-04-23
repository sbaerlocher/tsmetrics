#!/usr/bin/env bash
# @command observability:down
# @description Stop local observability stack
docker compose --profile observability down "$@"
