#!/bin/bash

# Development script with environment variables
# This script loads environment variables and starts air

set -e

# Load environment variables from .env file if it exists
if [ -f .env ]; then
    echo "Loading environment variables from .env"
    export $(grep -v '^#' .env | xargs)
fi

# Set default development environment variables
export USE_TSNET=${USE_TSNET:-true}
export TSNET_HOSTNAME=${TSNET_HOSTNAME:-tsmetrics-dev}
export TSNET_STATE_DIR=${TSNET_STATE_DIR:-/tmp/tsnet-state}
export TSNET_TAGS=${TSNET_TAGS:-exporter}
export REQUIRE_EXPORTER_TAG=${REQUIRE_EXPORTER_TAG:-true}
export TARGET_DEVICES=${TARGET_DEVICES:-gateway-140207,gateway-130104}
export ENV=${ENV:-development}
export PORT=${PORT:-9100}
export LOG_LEVEL=${LOG_LEVEL:-info}
export LOG_FORMAT=${LOG_FORMAT:-text}
export CLIENT_METRICS_TIMEOUT=${CLIENT_METRICS_TIMEOUT:-10s}
export MAX_CONCURRENT_SCRAPES=${MAX_CONCURRENT_SCRAPES:-10}
export CLIENT_METRICS_PORT=${CLIENT_METRICS_PORT:-5252}
export VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}
export BUILD_TIME=${BUILD_TIME:-$(date -u '+%Y-%m-%d_%H:%M:%S')}

echo "🚀 Starting development environment with air"
echo "Environment: $ENV"
echo "Use tsnet: $USE_TSNET"
echo "Hostname: $TSNET_HOSTNAME"

# Check if air is installed
if ! command -v air >/dev/null 2>&1; then
    echo "📦 Installing air..."
    go install github.com/air-verse/air@latest
fi

# Start air with live reload
exec air -c .air.toml
