#!/bin/bash
# Systemd deployment helper script for tsmetrics

set -euo pipefail

INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/tsmetrics"
DATA_DIR="/var/lib/tsmetrics"
SERVICE_NAME="tsmetrics"
USER="tsmetrics"
GROUP="tsmetrics"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

check_root() {
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root (use sudo)"
        exit 1
    fi
}

create_user() {
    if ! id "$USER" &>/dev/null; then
        log_info "Creating user $USER..."
        useradd --system --shell /bin/false --home-dir "$DATA_DIR" --create-home "$USER"
    else
        log_info "User $USER already exists"
    fi
}

install_binary() {
    local binary_path="$1"

    if [[ ! -f "$binary_path" ]]; then
        log_error "Binary not found: $binary_path"
        log_info "Build the binary first with: make build"
        exit 1
    fi

    log_info "Installing binary to $INSTALL_DIR..."
    cp "$binary_path" "$INSTALL_DIR/tsmetrics"
    chmod +x "$INSTALL_DIR/tsmetrics"
    chown root:root "$INSTALL_DIR/tsmetrics"
}

create_directories() {
    log_info "Creating directories..."
    mkdir -p "$CONFIG_DIR"
    mkdir -p "$DATA_DIR/tsnet-state"
    chown -R "$USER:$GROUP" "$DATA_DIR"
    chmod 750 "$DATA_DIR"
    chmod 750 "$CONFIG_DIR"
}

install_service() {
    log_info "Installing systemd service..."
    cp "deploy/systemd.service" "/etc/systemd/system/$SERVICE_NAME.service"
    systemctl daemon-reload
}

create_config() {
    local config_file="$CONFIG_DIR/config"

    if [[ ! -f "$config_file" ]]; then
        log_info "Creating configuration file..."
        cat > "$config_file" << 'EOF'
# TSMetrics Configuration
# Copy this file and edit with your Tailscale credentials

# Required: Tailscale OAuth2 credentials
OAUTH_CLIENT_ID=your_client_id_here
OAUTH_CLIENT_SECRET=your_client_secret_here
TAILNET_NAME=your_company_or_user

# Optional: Auth key for automatic device registration
# TS_AUTHKEY=tskey-auth-xxxxx

# Optional: Advanced configuration
# CLIENT_METRICS_TIMEOUT=10s
# MAX_CONCURRENT_SCRAPES=10
# SCRAPE_INTERVAL=30s
# REQUIRE_EXPORTER_TAG=false
EOF
        chmod 640 "$config_file"
        chown root:"$GROUP" "$config_file"

        log_warn "Configuration file created at $config_file"
        log_warn "Please edit this file with your Tailscale credentials before starting the service"
    else
        log_info "Configuration file already exists at $config_file"
    fi
}

start_service() {
    log_info "Enabling and starting service..."
    systemctl enable "$SERVICE_NAME"
    systemctl start "$SERVICE_NAME"
}

stop_service() {
    log_info "Stopping and disabling service..."
    systemctl stop "$SERVICE_NAME" || true
    systemctl disable "$SERVICE_NAME" || true
}

uninstall() {
    log_info "Uninstalling tsmetrics..."

    # Stop service
    stop_service

    # Remove service file
    rm -f "/etc/systemd/system/$SERVICE_NAME.service"
    systemctl daemon-reload

    # Remove binary
    rm -f "$INSTALL_DIR/tsmetrics"

    # Ask about data and config
    echo -n "Remove configuration and data directories? [y/N]: "
    read -r response
    if [[ "$response" =~ ^[Yy]$ ]]; then
        rm -rf "$CONFIG_DIR"
        rm -rf "$DATA_DIR"
        userdel "$USER" 2>/dev/null || true
        log_info "Configuration and data removed"
    else
        log_info "Configuration and data preserved"
    fi

    log_info "Uninstall complete"
}

show_status() {
    echo "=== TSMetrics Service Status ==="
    systemctl status "$SERVICE_NAME" --no-pager || true
    echo
    echo "=== Recent Logs ==="
    journalctl -u "$SERVICE_NAME" --no-pager -n 10 || true
}

usage() {
    echo "Usage: $0 {install|uninstall|start|stop|restart|status}"
    echo
    echo "Commands:"
    echo "  install   - Install tsmetrics as systemd service"
    echo "  uninstall - Remove tsmetrics service and optionally data"
    echo "  start     - Start the service"
    echo "  stop      - Stop the service"
    echo "  restart   - Restart the service"
    echo "  status    - Show service status and logs"
    echo
    echo "Examples:"
    echo "  sudo $0 install bin/tsmetrics"
    echo "  sudo $0 start"
    echo "  sudo $0 status"
}

main() {
    local command="${1:-}"
    local binary_path="${2:-bin/tsmetrics}"

    case "$command" in
        install)
            check_root
            create_user
            install_binary "$binary_path"
            create_directories
            install_service
            create_config
            log_info "Installation complete!"
            log_warn "Edit $CONFIG_DIR/config with your Tailscale credentials"
            log_info "Then start the service with: sudo systemctl start $SERVICE_NAME"
            ;;
        uninstall)
            check_root
            uninstall
            ;;
        start)
            check_root
            systemctl start "$SERVICE_NAME"
            log_info "Service started"
            ;;
        stop)
            check_root
            systemctl stop "$SERVICE_NAME"
            log_info "Service stopped"
            ;;
        restart)
            check_root
            systemctl restart "$SERVICE_NAME"
            log_info "Service restarted"
            ;;
        status)
            show_status
            ;;
        *)
            usage
            exit 1
            ;;
    esac
}

main "$@"
