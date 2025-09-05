#!/bin/bash
# Environment configuration utilities for tsmetrics
# Centralized environment variable management

set -euo pipefail

# Default configuration values
readonly DEFAULT_CONFIG="
USE_TSNET=true
TSNET_HOSTNAME=tsmetrics-dev
TSNET_STATE_DIR=/tmp/tsnet-state
TSNET_TAGS=exporter
TS_AUTHKEY=
REQUIRE_EXPORTER_TAG=true
ENV=development
PORT=9100
LOG_LEVEL=info
LOG_FORMAT=text
TARGET_DEVICES=gateway-140207,gateway-130104
CLIENT_METRICS_TIMEOUT=10s
MAX_CONCURRENT_SCRAPES=10
CLIENT_METRICS_PORT=5252
"

# Load configuration from string
load_default_config() {
	while IFS= read -r line; do
		[[ -z "${line// /}" ]] && continue
		if [[ $line =~ ^[A-Za-z_][A-Za-z0-9_]*= ]]; then
			local var_name="${line%%=*}"
			local var_value="${line#*=}"
			# Only set if not already defined
			if [[ -z "${!var_name:-}" ]]; then
				export "$var_name=$var_value"
			fi
		fi
	done <<<"$DEFAULT_CONFIG"
}

# Set build metadata
set_build_metadata() {
	export VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
	export BUILD_TIME="${BUILD_TIME:-$(date -u '+%Y-%m-%d_%H:%M:%S')}"

	# Tailscale version metadata
	export TSNET_VERSION="${TSNET_VERSION:-$(go list -m -f '{{.Version}}' tailscale.com 2>/dev/null | sed 's/^v//' || echo "unknown")}"
	export VERSION_CLEAN="${VERSION_CLEAN:-${VERSION#v}}"
	export VERSION_LONG="${VERSION_LONG:-${TSNET_VERSION}-${VERSION_CLEAN}}"
	export VERSION_SHORT="${VERSION_SHORT:-$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")}"
}

# Export functions for use in other scripts
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
	# Script is being executed directly
	load_default_config
	set_build_metadata
	echo "Environment configuration loaded"
fi
