#!/bin/bash
# Build script for tsmetrics
# Handles cross-platform builds and Docker images

set -euo pipefail

# Declare and assign separately to avoid masking return values
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly SCRIPT_DIR
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
readonly PROJECT_ROOT
readonly APP_NAME="tsmetrics"

# Source environment configuration
# shellcheck source=./setup-env.sh
# shellcheck disable=SC1091
source "$SCRIPT_DIR/setup-env.sh"

# Colors
readonly GREEN='\033[0;32m'
readonly BLUE='\033[0;34m'
readonly NC='\033[0m'

log_info() {
	echo -e "${BLUE}ℹ️  $1${NC}"
}

log_success() {
	echo -e "${GREEN}✅ $1${NC}"
}

# Build the application
build_app() {
	log_info "Building $APP_NAME"
	cd "$PROJECT_ROOT"

	local ldflags="-X main.version=$VERSION -X main.buildTime=$BUILD_TIME -X tailscale.com/version.longStamp=$VERSION_LONG -X tailscale.com/version.shortStamp=$VERSION_SHORT"

	if go build -ldflags "$ldflags" -o "bin/$APP_NAME" "./cmd/tsmetrics"; then
		log_success "Build completed: bin/$APP_NAME"
	else
		echo "❌ Build failed" >&2
		exit 1
	fi
}

# Main execution
main() {
	load_default_config
	set_build_metadata
	build_app
}

# Execute if run directly
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
	main "$@"
fi
