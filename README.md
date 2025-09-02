# tsmetrics

A comprehensive Tailscale Prometheus exporter that combines API metadata with live device metrics for complete network observability.

[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![Docker](https://img.shields.io/badge/Docker-Available-2496ED?style=flat&logo=docker)](https://hub.docker.com/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

## 🚀 Features

- **Dual Data Sources**: Combines Tailscale REST API metadata with live device client metrics
- **Comprehensive Metrics**: Device status, network traffic, routing configuration, and health monitoring
- **tsnet Integration**: Optional Tailscale network integration for secure internal access
- **Concurrent Scraping**: Configurable parallel device metrics collection
- **Production Ready**: Docker/Kubernetes deployments with proper health checks
- **Memory Efficient**: Automatic cleanup of stale device metrics
- **Modern Go Architecture**: Standard Go project structure with clear package boundaries

## Table of Contents

- [Quick Start](#quick-start)
- [Installation](#installation)
- [Configuration](#configuration)
- [Project Structure](#project-structure)
- [Metrics Reference](#metrics-reference)
- [Deployment](#deployment)
- [Development](#development)
- [Architecture](#architecture)
- [Migration Guide](#migration-guide)
- [Troubleshooting](#troubleshooting)
- [Contributing](#contributing)
- [Changelog](#changelog)

## Installation

### Binary Installation

1. **Download release binary** (when available):

   ```bash
   # Download latest release
   curl -L https://github.com/sbaerlocher/tsmetrics/releases/latest/download/tsmetrics-linux-amd64 -o tsmetrics
   chmod +x tsmetrics
   ./tsmetrics
   ```

2. **Build from source:**

   ```bash
   git clone https://github.com/sbaerlocher/tsmetrics
   cd tsmetrics
   make build
   ./bin/tsmetrics
   ```

3. **Configure environment:**

   ```bash
   cp .env.example .env
   # Edit .env with your Tailscale credentials
   ```

4. **Verify installation:**

   ```bash
   curl http://localhost:9100/metrics
   curl http://localhost:9100/health
   ```

### Docker Installation

**Standalone mode:**

```bash
docker run -d \
  --name tsmetrics \
  -e OAUTH_CLIENT_ID=your_client_id \
  -e OAUTH_CLIENT_SECRET=your_client_secret \
  -e TAILNET_NAME=your-company \
  -p 9100:9100 \
  ghcr.io/sbaerlocher/tsmetrics:latest
```

**tsnet mode (recommended for production):**

```bash
docker run -d \
  --name tsmetrics \
  -e USE_TSNET=true \
  -e TSNET_HOSTNAME=tsmetrics \
  -e TSNET_TAGS=exporter \
  -e OAUTH_CLIENT_ID=your_client_id \
  -e OAUTH_CLIENT_SECRET=your_client_secret \
  -e TAILNET_NAME=your-company \
  -v tsnet-state:/tmp/tsnet-state \
  ghcr.io/sbaerlocher/tsmetrics:latest
```

### Quick Start

### Prerequisites

1. Tailscale account with API access
2. OAuth2 client credentials from [Tailscale Admin Console](https://login.tailscale.com/admin/settings/oauth)
3. Target devices with client metrics enabled (`tailscale set --metrics-listen-addr=0.0.0.0:5252`)

### Basic Setup

1. **Clone and build:**

   ```bash
   git clone https://github.com/sbaerlocher/tsmetrics
   cd tsmetrics
   make build
   ```

2. **Configure environment:**

   ```bash
   cp .env.example .env
   # Edit .env with your Tailscale credentials
   ```

3. **Run standalone:**

   ```bash
   make run
   ```

4. **Verify metrics:**

   ```bash
   curl http://localhost:9100/metrics
   curl http://localhost:9100/health
   ```

## Project Structure

```text
tsmetrics/
├── cmd/tsmetrics/          # Application entry point
│   └── main.go
├── internal/               # Private application packages
│   ├── api/               # Tailscale API client
│   ├── config/            # Configuration management
│   ├── errors/            # Error types and handling
│   ├── metrics/           # Metrics collection and definitions
│   └── server/            # HTTP server and handlers
├── pkg/device/            # Public device package
├── scripts/               # Build and development scripts
├── deploy/                # Deployment configurations
│   ├── docker-compose.yaml
│   ├── kubernetes.yaml
│   └── systemd.service
├── .env.example           # Environment configuration template
├── Makefile              # Build and development targets
├── Dockerfile            # Container build configuration
└── bin/                  # Compiled binaries
```

### Package Overview

| Package | Description |
|---------|-------------|
| `cmd/tsmetrics` | Application entry point and main function |
| `internal/api` | Tailscale API client with OAuth2 authentication |
| `internal/config` | Configuration loading and validation |
| `internal/errors` | Custom error types and error handling |
| `internal/metrics` | Prometheus metrics definitions and collection |
| `internal/server` | HTTP server, handlers, and tsnet integration |
| `pkg/device` | Public device data structures and utilities |

### Docker Deployment

**Standalone mode:**

```bash
docker run -d \
  --name tsmetrics \
  -e OAUTH_CLIENT_ID=your_client_id \
  -e OAUTH_CLIENT_SECRET=your_client_secret \
  -e TAILNET_NAME=your-company \
  -p 9100:9100 \
  ghcr.io/sbaerlocher/tsmetrics:latest
```

**tsnet mode (recommended for production):**

```bash
docker run -d \
  --name tsmetrics \
  -e USE_TSNET=true \
  -e TSNET_HOSTNAME=tsmetrics \
  -e TSNET_TAGS=exporter \
  -e OAUTH_CLIENT_ID=your_client_id \
  -e OAUTH_CLIENT_SECRET=your_client_secret \
  -e TAILNET_NAME=your-company \
  -v tsnet-state:/tmp/tsnet-state \
  ghcr.io/sbaerlocher/tsmetrics:latest
```

## Configuration

### Core Settings

| Environment Variable | Description | Default |
|---------------------|-------------|---------|
| `OAUTH_CLIENT_ID` | Tailscale OAuth2 Client ID | Required |
| `OAUTH_CLIENT_SECRET` | Tailscale OAuth2 Client Secret | Required |
| `TAILNET_NAME` | Tailnet name or "-" for default | Required |
| `PORT` | HTTP server port | `9100` |
| `ENV` | `production`/`prod` binds 0.0.0.0, otherwise 127.0.0.1 | `development` |

### tsnet Configuration

| Environment Variable | Description | Default |
|---------------------|-------------|---------|
| `USE_TSNET` | Enable Tailscale tsnet integration | `false` |
| `TSNET_HOSTNAME` | Hostname in Tailnet | `tsmetrics` |
| `TSNET_STATE_DIR` | Persistent state directory | `/tmp/tsnet-tsmetrics` |
| `TSNET_TAGS` | Comma-separated device tags | - |
| `TS_AUTHKEY` | Auth key for automatic device registration with tags | - |
| `REQUIRE_EXPORTER_TAG` | Enforce "exporter" tag requirement | `false` |

**Note**: To automatically assign tags to the tsnet device, create an auth key in
the Tailscale admin console with the desired tags and set `TS_AUTHKEY`.
The `TSNET_TAGS` variable is used for validation only.

### Performance Tuning

| Environment Variable | Description | Default |
|---------------------|-------------|---------|
| `CLIENT_METRICS_TIMEOUT` | Device metrics timeout | `10s` |
| `MAX_CONCURRENT_SCRAPES` | Parallel device scrapes | `10` |
| `SCRAPE_INTERVAL` | Device discovery interval | `30s` |

### Advanced Configuration

#### Logging

```bash
# Set log level (debug, info, warn, error)
LOG_LEVEL=info

# Set log format (json, text)
LOG_FORMAT=text
```

#### Security

```bash
# Enforce exporter tag requirement
REQUIRE_EXPORTER_TAG=true

# Custom metrics port for devices
CLIENT_METRICS_PORT=5252
```

#### Development/Testing

```bash
# Mock devices for testing
TEST_DEVICES=gateway-1,gateway-2,server-3

# Target specific devices only
TARGET_DEVICES=production-gateway,backup-server
```

### Environment Variables Reference

#### Required Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `OAUTH_CLIENT_ID` | Tailscale OAuth2 Client ID | `k123abc...` |
| `OAUTH_CLIENT_SECRET` | Tailscale OAuth2 Client Secret | `tskey-client-...` |
| `TAILNET_NAME` | Your tailnet name or "-" for personal | `company.ts.net` |

#### Optional Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `9100` | HTTP server port |
| `ENV` | `development` | Environment (`production`/`prod` binds 0.0.0.0) |
| `USE_TSNET` | `false` | Enable tsnet integration |
| `TSNET_HOSTNAME` | `tsmetrics` | Hostname in tailnet |
| `TSNET_STATE_DIR` | `/tmp/tsnet-tsmetrics` | Persistent state directory |
| `TSNET_TAGS` | - | Comma-separated device tags |
| `TS_AUTHKEY` | - | Auth key for automatic registration |
| `REQUIRE_EXPORTER_TAG` | `false` | Enforce "exporter" tag requirement |
| `LOG_LEVEL` | `info` | Logging level |
| `LOG_FORMAT` | `text` | Log format (`text` or `json`) |

## Metrics Reference

### Device Management (from Tailscale API)

```prometheus
tailscale_device_count
tailscale_device_info{device_id, device_name, online, os, version}
tailscale_device_authorized{device_id, device_name}
tailscale_device_last_seen_timestamp{device_id, device_name}
tailscale_device_user{device_id, device_name, user_email}
tailscale_device_machine_key_expiry{device_id, device_name}
```

### Network Configuration (from Tailscale API)

```prometheus
tailscale_device_routes_advertised{device_id, device_name, route}
tailscale_device_routes_enabled{device_id, device_name, route}
tailscale_device_exit_node{device_id, device_name}
tailscale_device_subnet_router{device_id, device_name}
```

### Network Performance (from device client metrics)

```prometheus
tailscaled_inbound_bytes_total{device_id, device_name, path}
tailscaled_outbound_bytes_total{device_id, device_name, path}
tailscaled_inbound_packets_total{device_id, device_name, path}
tailscaled_outbound_packets_total{device_id, device_name, path}
tailscaled_inbound_dropped_packets_total{device_id, device_name}
tailscaled_outbound_dropped_packets_total{device_id, device_name, reason}
tailscaled_health_messages{device_id, device_name, type}
tailscaled_advertised_routes{device_id, device_name}
tailscaled_approved_routes{device_id, device_name}
```

### Grafana Dashboards

#### Sample Dashboard Queries

**Device Count Overview:**

```promql
sum(tailscale_device_count)
```

**Online vs Offline Devices:**

```promql
sum by (online) (tailscale_device_info)
```

**Network Traffic by Device:**

```promql
rate(tailscaled_inbound_bytes_total[5m])
rate(tailscaled_outbound_bytes_total[5m])
```

**Device Health Status:**

```promql
sum by (device_name, type) (tailscaled_health_messages)
```

**Subnet Router Status:**

```promql
sum by (device_name) (tailscale_device_subnet_router)
```

#### Dashboard Creation

1. **Import Prometheus data source** in Grafana
2. **Create dashboard** with panels for:
   - Device inventory and status
   - Network traffic heatmaps
   - Health monitoring alerts
   - Route advertisement status
3. **Set up alerts** for offline devices or health issues

### Monitoring and Alerting

#### Prometheus Alerting Rules

```yaml
groups:
  - name: tailscale
    rules:
      - alert: TailscaleDeviceOffline
        expr: tailscale_device_info{online="false"} == 1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Tailscale device {{ $labels.device_name }} is offline"

      - alert: TailscaleHighPacketLoss
        expr: rate(tailscaled_inbound_dropped_packets_total[5m]) > 100
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "High packet loss on {{ $labels.device_name }}"
```

## Deployment

### Helm Chart (Recommended)

The most flexible deployment option with full configuration management:

```bash
# Install with default configuration
helm install tsmetrics deploy/helm/tsmetrics

# Install with custom values
helm install tsmetrics deploy/helm/tsmetrics \
  --set tailscale.oauthClientId=your-client-id \
  --set tailscale.oauthClientSecret=your-client-secret \
  --set tailscale.tailnetName=your-company

# Or use a values file
helm install tsmetrics deploy/helm/tsmetrics -f my-values.yaml
```

**Example values.yaml:**

```yaml
image:
  tag: "v2.0.0"

tailscale:
  oauthClientId: "k123abc..."
  oauthClientSecret: "tskey-client-..."
  tailnetName: "company.ts.net"
  tsnet:
    enabled: true
    hostname: "tsmetrics"
    tags: "exporter"

resources:
  requests:
    cpu: 200m
    memory: 256Mi
  limits:
    cpu: 1000m
    memory: 1Gi

persistence:
  enabled: true
  size: 2Gi
```

### Kustomize

Environment-specific deployments with overlay management:

```bash
# Development deployment
kubectl apply -k deploy/kustomize/overlays/development

# Production deployment
kubectl apply -k deploy/kustomize/overlays/production

# Preview changes before applying
kubectl kustomize deploy/kustomize/overlays/production
```

**Secret setup (required for Kustomize):**

```bash
kubectl create secret generic tsmetrics-secrets \
  --from-literal=OAUTH_CLIENT_ID=your-client-id \
  --from-literal=OAUTH_CLIENT_SECRET=your-client-secret \
  --from-literal=TAILNET_NAME=your-company
```

### Plain Kubernetes

Simple deployment for basic setups:

```bash
# Deploy with plain YAML
kubectl apply -f deploy/kubernetes.yaml

# Customize by editing the YAML file
kubectl edit -f deploy/kubernetes.yaml
```

### Docker Compose

```yaml
version: '3.8'
services:
  tsmetrics:
    image: ghcr.io/sbaerlocher/tsmetrics:latest
    environment:
      - OAUTH_CLIENT_ID=your_client_id
      - OAUTH_CLIENT_SECRET=your_client_secret
      - TAILNET_NAME=your-company
      - USE_TSNET=true
      - TSNET_HOSTNAME=tsmetrics
      - TSNET_TAGS=exporter
    volumes:
      - tsnet-state:/tmp/tsnet-state
    restart: unless-stopped

volumes:
  tsnet-state:
```

### Systemd Service

For running on Linux servers without Kubernetes:

```bash
# Build and install as systemd service
make build
sudo make systemd-install

# Edit configuration with your Tailscale credentials
sudo nano /etc/tsmetrics/config

# Start the service
sudo make systemd-start

# Check status
make systemd-status
```

**Manual installation:**

```bash
# Build binary
make build

# Install using the helper script
sudo deploy/install-systemd.sh install bin/tsmetrics

# Configure credentials
sudo nano /etc/tsmetrics/config

# Start service
sudo systemctl start tsmetrics
```

### Deployment Comparison

| Method | Best For | Pros | Cons |
|--------|----------|------|------|
| **Helm** | Production, Multi-env | Full lifecycle management, templating, values | Learning curve, complexity |
| **Kustomize** | GitOps, Environment overlays | Native k8s, patches, no templating | Limited logic, patch complexity |
| **Systemd** | Linux servers, VPS | Native Linux service, simple | Single host, manual scaling |
| **Plain YAML** | Simple deployments | Direct control, simplicity | No environment management |
| **Docker Compose** | Development, Single host | Easy setup, local development | No orchestration, single host |

### Makefile Targets

All deployment methods are available via Makefile:

```bash
# Helm
make helm-install        # Install with Helm
make helm-upgrade        # Upgrade deployment
make helm-template       # Preview templates

# Kustomize
make kustomize-dev       # Deploy development
make kustomize-prod      # Deploy production
make kustomize-preview-prod  # Preview changes

# Plain Kubernetes
make k8s-deploy          # Simple deployment
make k8s-delete          # Remove deployment

# Systemd
make systemd-install     # Install as service
make systemd-start       # Start service
make systemd-status      # Check status
```

### Prometheus Configuration

```yaml
scrape_configs:
  - job_name: 'tailscale-metrics'
    static_configs:
      - targets: ['tsmetrics.tailnet.ts.net:9100']  # tsnet mode
    scrape_interval: 60s
    metrics_path: /metrics
    timeout: 30s
```

## Development

### Prerequisites

- Go 1.24+
- Docker (optional)
- [air](https://github.com/air-verse/air) for live reload (optional)

### Development Workflow

```bash
# Setup development environment
cp .env.example .env
# Edit .env with your Tailscale credentials
make dev-deps

# Start development server with live reload
# Environment variables are automatically loaded from .env and set via dev.sh
make dev

# Alternative development commands:
make dev-tsnet     # Same as dev (alias)
make dev-direct    # Direct go run (no live reload)

# Run tests
make test

# Build and run locally
make build
make run-tsnet
```

### Environment Configuration

The development environment uses a dedicated `dev.sh` script that:

1. **Loads `.env` file** if present (automatically exports variables)
2. **Sets sensible defaults** for all configuration options
3. **Ensures consistency** between development runs
4. **Manages air installation** and execution

You only need to:

1. **Copy the example:** `cp .env.example .env`
2. **Configure credentials:** Edit `.env` with your Tailscale OAuth details
3. **Run development:** `make dev`

All environment variables are managed centrally through the `dev.sh` script,
eliminating the need to maintain duplicated configurations.

### Testing with Mock Devices

For development without real Tailscale credentials:

```bash
export TEST_DEVICES="gateway-1,gateway-2,server-3"
make run
```

## Architecture

### Project Structure

```text
tsmetrics/
├── cmd/tsmetrics/          # Application entry point
│   └── main.go
├── internal/               # Private application packages
│   ├── api/               # Tailscale API client
│   ├── config/            # Configuration management
│   ├── errors/            # Error types and handling
│   ├── metrics/           # Metrics collection and definitions
│   └── server/            # HTTP server and handlers
├── pkg/device/            # Public device package
├── scripts/               # Build and development scripts
├── deploy/                # Deployment configurations
│   ├── docker-compose.yaml
│   ├── kubernetes.yaml
│   └── systemd.service
└── bin/                   # Compiled binaries
```

### Operation Flow

tsmetrics operates in two phases:

1. **Device Discovery**: Fetches device inventory from Tailscale REST API
2. **Metrics Collection**: Concurrently scrapes client metrics from each online device with the "exporter" tag

### Security Features

- **OAuth2 Flow**: Uses client credentials for secure API access
- **Input Validation**: Validates all hostnames to prevent injection attacks
- **Tag-Based Access**: Only scrapes devices with the "exporter" tag
- **Rate Limiting**: Configurable concurrent scraping limits
- **No Hardcoded Secrets**: All credentials via environment variables

### Performance Features

- **Connection Pooling**: Reuses HTTP connections for efficiency
- **Concurrent Scraping**: Parallel device metrics collection
- **Memory Management**: Automatic cleanup of stale device metrics
- **Circuit Breaker**: Protects against API failures (planned)

### High Availability

#### Load Balancing

```yaml
# Multiple tsmetrics instances with different hostnames
services:
  tsmetrics-1:
    image: ghcr.io/sbaerlocher/tsmetrics:latest
    environment:
      - TSNET_HOSTNAME=tsmetrics-1

  tsmetrics-2:
    image: ghcr.io/sbaerlocher/tsmetrics:latest
    environment:
      - TSNET_HOSTNAME=tsmetrics-2
```

#### Backup and Recovery

```bash
# Backup tsnet state
docker run --rm -v tsnet-state:/data -v $(pwd):/backup \
  alpine tar czf /backup/tsnet-backup.tar.gz -C /data .

# Restore tsnet state
docker run --rm -v tsnet-state:/data -v $(pwd):/backup \
  alpine tar xzf /backup/tsnet-backup.tar.gz -C /data
```

## Troubleshooting

### Common Issues

#### OAuth2 Authentication Failed

- Verify `OAUTH_CLIENT_ID` and `OAUTH_CLIENT_SECRET`
- Check that the OAuth client has appropriate scopes
- Ensure `TAILNET_NAME` matches your tailnet exactly

#### No Devices Discovered

- Confirm API credentials are correct
- Check that devices are online in Tailscale admin console
- Verify network connectivity to Tailscale API

#### Client Metrics Not Available

- Enable metrics on target devices: `tailscale set --metrics-listen-addr=0.0.0.0:5252`
- Ensure devices have the "exporter" tag
- Check firewall rules allow HTTP access to port 5252

#### tsnet Authentication Issues

- First run may require interactive authentication
- Check tsnet state directory permissions
- Verify `TSNET_TAGS` includes required tags

#### tsnet Startup Messages

- Messages like `"routerIP/FetchRIB: sysctl: cannot allocate memory"` are normal internal tsnet logs during startup
- These are not errors but informational messages from the Tailscale networking layer
- Initial device scraping errors are expected until tsnet establishes connection
- Connection typically stabilizes within 10-30 seconds

#### Debug Mode

Enable debug logging and access debug endpoint:

```bash
# Check application status
curl http://localhost:9100/debug

# View detailed logs
docker logs tsmetrics -f

# Enable debug logging
export LOG_LEVEL=debug
make run

# Test specific device
export TEST_DEVICES="specific-device-name"
make run
```

#### Performance Troubleshooting

**High Memory Usage:**

```bash
# Monitor memory usage
docker stats tsmetrics

# Reduce concurrent scrapes
export MAX_CONCURRENT_SCRAPES=5

# Increase cleanup frequency
export SCRAPE_INTERVAL=60s
```

**Slow Device Discovery:**

```bash
# Check API response time
curl -w "%{time_total}" https://api.tailscale.com/api/v2/tailnet/{tailnet}/devices

# Reduce timeout
export CLIENT_METRICS_TIMEOUT=5s

# Target specific devices only
export TARGET_DEVICES=critical-device-1,critical-device-2
```

#### Network Troubleshooting

**Connection Issues:**

```bash
# Test device connectivity
telnet device-ip 5252

# Check firewall rules
iptables -L | grep 5252

# Test metrics endpoint
curl http://device-ip:5252/debug/metrics
```

**DNS Resolution:**

```bash
# Test device hostname resolution
nslookup device-name.tailnet.ts.net

# Check tsnet connectivity
docker exec tsmetrics ping device-name
```

#### Container Troubleshooting

**Permission Issues:**

```bash
# Check container user
docker exec tsmetrics id

# Fix volume permissions
docker run --rm -v tsnet-state:/data alpine chown -R 65534:65534 /data
```

**Resource Constraints:**

```bash
# Increase container limits
docker run --memory=512m --cpus=1.0 ghcr.io/sbaerlocher/tsmetrics:latest

# Monitor resource usage
docker exec tsmetrics top
```

## Migration Guide

### Upgrading from v1.x

This project has been restructured to follow Go best practices. If you're upgrading from an older version:

#### Major Changes in v2.0

1. **Project Structure**: Migrated from monolithic to modular structure
   - `main.go` → `cmd/tsmetrics/main.go`
   - Split into logical packages under `internal/` and `pkg/`

2. **Package Organization**:

   ```text
   Old Structure → New Structure
   config.go     → internal/config/config.go
   api.go        → internal/api/client.go
   device.go     → pkg/device/device.go
   metrics.go    → internal/metrics/{definitions,collector,scraper,tracker}.go
   server.go     → internal/server/{server,handlers,tsnet}.go
   errors.go     → internal/errors/types.go
   ```

3. **Build Process**: Now uses standard Go project layout

#### Migration Steps

1. **Backup your current setup**:

   ```bash
   # Backup your environment configuration
   cp .env .env.backup
   ```

2. **Update to new version**:

   ```bash
   git pull origin main
   make build
   ```

3. **Verify functionality**:

   ```bash
   # Test with existing configuration
   make run
   curl http://localhost:9100/health
   ```

4. **Update deployment scripts** (if custom):
   - Build commands: Use `make build`
   - Run commands: Use `make run` or `./bin/tsmetrics`

#### Compatibility

- ✅ **Configuration**: 100% compatible
- ✅ **Metrics**: Same Prometheus metrics output
- ✅ **API**: Same REST endpoints
- ✅ **Docker**: Same container interface
- ✅ **Behavior**: Identical runtime behavior

The new structure provides:

- Better testability with isolated packages
- Clearer dependencies and module boundaries
- Improved maintainability
- Enhanced IDE support
- Standard Go project conventions

## API Reference

### Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/metrics` | GET | Prometheus metrics |
| `/health` | GET | Health check |
| `/debug` | GET | Debug information |

### Health Check Response

```json
{
  "status": "healthy",
  "timestamp": "2025-09-02T21:30:00Z",
  "version": "v2.0.0",
  "uptime": "2h15m30s",
  "devices_discovered": 15,
  "devices_scraped": 12,
  "last_scrape": "2025-09-02T21:29:45Z"
}
```

### Debug Information

```bash
curl http://localhost:9100/debug
```

```json
{
  "config": {
    "use_tsnet": true,
    "tsnet_hostname": "tsmetrics",
    "max_concurrent_scrapes": 10,
    "client_metrics_timeout": "10s"
  },
  "runtime": {
    "go_version": "go1.24.0",
    "num_goroutines": 25,
    "memory_usage": "45.2MB"
  },
  "metrics": {
    "devices_total": 15,
    "devices_online": 12,
    "scrape_errors": 0,
    "last_api_call": "2025-09-02T21:29:30Z"
  }
}
```

## Advanced Usage

### Custom Device Filtering

```bash
# Only monitor specific device types
export TARGET_DEVICES="gateway-*,router-*"

# Monitor by tag (requires API support)
export DEVICE_TAGS="production,critical"

# Exclude specific devices
export EXCLUDE_DEVICES="test-device,staging-*"
```

### Integration Examples

#### Prometheus Configuration with Service Discovery

```yaml
scrape_configs:
  - job_name: 'tailscale-metrics'
    kubernetes_sd_configs:
      - role: service
        namespaces:
          names:
            - monitoring
    relabel_configs:
      - source_labels: [__meta_kubernetes_service_annotation_prometheus_io_scrape]
        action: keep
        regex: true
      - source_labels: [__meta_kubernetes_service_annotation_prometheus_io_path]
        action: replace
        target_label: __metrics_path__
        regex: (.+)
```

#### Grafana Alert Integration

```json
{
  "alert": {
    "name": "Tailscale Device Offline",
    "message": "Device {{ $labels.device_name }} has been offline for > 5 minutes",
    "frequency": "30s",
    "conditions": [
      {
        "query": {
          "queryType": "",
          "refId": "A",
          "model": {
            "expr": "tailscale_device_info{online=\"false\"} == 1",
            "interval": "",
            "legendFormat": "",
            "refId": "A"
          }
        },
        "reducer": {
          "type": "last",
          "params": []
        },
        "evaluator": {
          "params": [1],
          "type": "gt"
        }
      }
    ]
  }
}
```

### CI/CD Integration

#### GitHub Actions

```yaml
name: Deploy tsmetrics
on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Build and push Docker image
        run: |
          docker build -t ghcr.io/${{ github.repository }}:latest .
          docker push ghcr.io/${{ github.repository }}:latest

      - name: Deploy to Kubernetes
        run: |
          kubectl set image deployment/tsmetrics \
            tsmetrics=ghcr.io/${{ github.repository }}:latest
```

#### ArgoCD Application

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: tsmetrics
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/sbaerlocher/tsmetrics
    targetRevision: HEAD
    path: deploy
  destination:
    server: https://kubernetes.default.svc
    namespace: monitoring
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
```

## Contributing

### Development Setup

1. **Fork the repository**
2. **Clone your fork:**

   ```bash
   git clone https://github.com/yourusername/tsmetrics
   cd tsmetrics
   ```

3. **Set up development environment:**

   ```bash
   cp .env.example .env
   # Edit .env with your Tailscale credentials
   make dev-deps
   ```

4. **Start development server:**

   ```bash
   make dev
   ```

### Making Changes

1. **Create a feature branch:**

   ```bash
   git checkout -b feature/your-feature-name
   ```

2. **Make your changes**
3. **Add tests for new functionality**
4. **Ensure all tests pass:**

   ```bash
   make test
   make lint
   ```

5. **Update documentation if needed**

### Submitting Changes

1. **Commit your changes:**

   ```bash
   git add .
   git commit -m "feat: add your feature description"
   ```

2. **Push to your fork:**

   ```bash
   git push origin feature/your-feature-name
   ```

3. **Create a pull request**

### Code Guidelines

- Follow Go best practices and idioms
- Add tests for new functionality
- Update documentation for user-facing changes
- Use conventional commit messages
- Ensure code passes all linters

### Testing

```bash
# Run all tests
make test

# Run with coverage
go test -v -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run integration tests (planned)
make test-integration
```

## License

MIT License - see [LICENSE](LICENSE) for details.

## Changelog

### v2.0.0 (Current)

- **Major Restructure**: Migrated to standard Go project structure
- **Modular Architecture**: Split code into logical packages (`internal/`, `pkg/`)
- **Enhanced Documentation**: Comprehensive README with all deployment scenarios
- **Improved Build System**: Updated Makefile and scripts for new structure
- **Development Workflow**: Enhanced dev environment with live reload support

### v1.x

- Initial release with monolithic structure
- Basic Tailscale API integration
- Docker and Kubernetes deployment support

## Related Projects

- [Tailscale](https://tailscale.com/) - Zero config VPN
- [Prometheus](https://prometheus.io/) - Monitoring system
- [Grafana](https://grafana.com/) - Visualization platform
