#!/bin/bash
# Development environment setup script for tsmetrics
# Best practices implementation with proper error handling and modularity

set -euo pipefail
IFS=$'\n\t'

# Script configuration - Declare and assign separately
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly SCRIPT_DIR
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
readonly PROJECT_ROOT
readonly ENV_FILE="$PROJECT_ROOT/.env"
readonly AIR_CONFIG="$PROJECT_ROOT/.air.toml"

# Colors for output
readonly RED='\033[0;31m'
readonly GREEN='\033[0;32m'
readonly YELLOW='\033[1;33m'
readonly BLUE='\033[0;34m'
readonly PURPLE='\033[0;35m'
readonly CYAN='\033[0;36m'
readonly NC='\033[0m' # No Color

# Logging functions
log_info() {
	echo -e "${CYAN}ℹ️  $1${NC}"
}

log_success() {
	echo -e "${GREEN}✅ $1${NC}"
}

log_warning() {
	echo -e "${YELLOW}⚠️  $1${NC}"
}

log_error() {
	echo -e "${RED}❌ $1${NC}" >&2
}

log_header() {
	echo -e "${PURPLE}🚀 $1${NC}"
}

log_config() {
	echo -e "${BLUE}  $1${NC}"
}

# Load environment variables from .env file
load_env_file() {
	if [[ -f "$ENV_FILE" ]]; then
		log_info "Loading environment variables from .env"
		# Safely load .env file
		while IFS= read -r line; do
			# Skip comments and empty lines
			[[ $line =~ ^[[:space:]]*# ]] && continue
			[[ -z "${line// /}" ]] && continue
			# Export valid variable assignments
			if [[ $line =~ ^[A-Za-z_][A-Za-z0-9_]*= ]]; then
				export "${line?}"
			fi
		done <"$ENV_FILE"
		log_success "Environment variables loaded"
	else
		log_warning "No .env file found, using defaults only"
	fi
}

# Set development defaults
set_development_defaults() {
	log_info "Setting development defaults"

	# Core configuration
	export USE_TSNET="${USE_TSNET:-true}"
	export TSNET_HOSTNAME="${TSNET_HOSTNAME:-tsmetrics-dev}"
	export TSNET_STATE_DIR="${TSNET_STATE_DIR:-/tmp/tsnet-state}"
	export TSNET_TAGS="${TSNET_TAGS:-exporter}"
	export REQUIRE_EXPORTER_TAG="${REQUIRE_EXPORTER_TAG:-true}"

	# Application configuration
	export ENV="${ENV:-development}"
	export PORT="${PORT:-9100}"
	export LOG_LEVEL="${LOG_LEVEL:-info}"
	export LOG_FORMAT="${LOG_FORMAT:-text}"

	# Device and performance configuration
	export TARGET_DEVICES="${TARGET_DEVICES:-gateway-140207,gateway-130104}"
	export CLIENT_METRICS_TIMEOUT="${CLIENT_METRICS_TIMEOUT:-10s}"
	export MAX_CONCURRENT_SCRAPES="${MAX_CONCURRENT_SCRAPES:-10}"
	export CLIENT_METRICS_PORT="${CLIENT_METRICS_PORT:-5252}"

	# Build metadata
	export VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
	export BUILD_TIME="${BUILD_TIME:-$(date -u '+%Y-%m-%d_%H:%M:%S')}"

	# Tailscale version metadata
	export TSNET_VERSION="${TSNET_VERSION:-$(go list -m -f '{{.Version}}' tailscale.com 2>/dev/null | sed 's/^v//' || echo "unknown")}"
	export VERSION_CLEAN="${VERSION_CLEAN:-${VERSION#v}}"
	export VERSION_LONG="${VERSION_LONG:-${TSNET_VERSION}-${VERSION_CLEAN}}"
	export VERSION_SHORT="${VERSION_SHORT:-$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")}"
}

# Display current configuration
show_configuration() {
	log_info "Current configuration:"
	echo "  Environment: $ENV"
	echo "  Use tsnet: $USE_TSNET"
	echo "  Hostname: $TSNET_HOSTNAME"
	echo "  Port: $PORT"
	echo "  Log Level: $LOG_LEVEL"
	echo "  Target Devices: $TARGET_DEVICES"
}

# Check and install air if needed
ensure_air() {
	if command -v air >/dev/null 2>&1; then
		log_success "air is already installed"
	else
		log_info "Installing air..."
		if go install github.com/air-verse/air@latest; then
			log_success "air installed successfully"
		else
			log_error "Failed to install air"
			exit 1
		fi
	fi
}

# Start development server
start_development() {
	log_info "Starting development environment with air"
	log_info "Note: When using tsnet, initial device scraping errors are normal"
	log_info "Connection will stabilize after tsnet authentication completes"

	if [[ ! -f "$AIR_CONFIG" ]]; then
		log_error "Air configuration file not found: $AIR_CONFIG"
		exit 1
	fi

	cd "$PROJECT_ROOT"
	exec air -c .air.toml
}

# Main function
main() {
	log_info "🚀 Starting tsmetrics development environment"

	# Change to project root
	cd "$PROJECT_ROOT"

	# Setup environment
	load_env_file
	set_development_defaults
	show_configuration

	# Prepare development tools
	ensure_air

	# Start development server
	start_development
}

# Execute main function
main "$@"
