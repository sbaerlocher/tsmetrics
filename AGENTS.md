# Agent Instructions — tsmetrics

## Purpose

Comprehensive Tailscale Prometheus Exporter that combines API metadata with live device metrics for complete network observability.

## Context

- **Project:** tsmetrics - Tailscale Prometheus Exporter
- **Language:** Go with Prometheus Client Library and Tailscale tsnet
- **Architecture:** Single binary aggregates Tailscale API data + device client metrics
- **Deployment:** Docker/Kubernetes ready with optional tsnet integration
- **Entry Point:** `main.go`

## Core Functionality

### What tsmetrics does

1. **Device Discovery:** Fetch all Tailscale devices via REST API (`/api/v2/tailnet/{tailnet}/devices`)
2. **API Metrics Collection:** Extract device metadata, authorization status, routing configuration, user assignments from API
3. **Client Metrics Scraping:** Collect live performance data from each device's metrics endpoint (`http://device:5252/metrics`)
4. **Metrics Aggregation:** Combine API and client data into unified Prometheus metrics
5. **Single Endpoint:** Expose all metrics at `/metrics` for Prometheus scraping
6. **Optional tsnet Integration:** Join Tailnet as device for secure internal access

### Data Sources

**Tailscale REST API:**

- Device inventory and online status
- Authorization and authentication state
- Subnet routing and exit node configuration
- User assignments and machine ownership
- Version information and OS details
- Machine key expiry dates

**Device Client Metrics (Port 5252):**

- Network traffic statistics (bytes/packets)
- Connection path information (direct/DERP)
- Health messages and connectivity status
- Route advertisement status

### Output Metrics

```
# Device Management (from Tailscale API)
tailscale_device_count
tailscale_device_info{device_id, device_name, online, os, version}
tailscale_device_authorized{device_id, device_name}
tailscale_device_last_seen_timestamp{device_id, device_name}
tailscale_device_user{device_id, device_name, user_email}
tailscale_device_machine_key_expiry{device_id, device_name}

# Network Configuration (from Tailscale API)
tailscale_device_routes_advertised{device_id, device_name, route}
tailscale_device_routes_enabled{device_id, device_name, route}
tailscale_device_exit_node{device_id, device_name}
tailscale_device_subnet_router{device_id, device_name}

# Network Performance (from device client metrics)
tailscaled_inbound_bytes_total{device_id, device_name, path}
tailscaled_outbound_bytes_total{device_id, device_name, path}
tailscaled_inbound_packets_total{device_id, device_name, path}
tailscaled_outbound_packets_total{device_id, device_name, path}
tailscaled_inbound_dropped_packets_total{device_id, device_name}
tailscaled_outbound_dropped_packets_total{device_id, device_name, reason}
tailscaled_health_messages{device_id, device_name, type}
tailscaled_advertised_routes{device_id, device_name}
tailscaled_approved_routes{device_id, device_name}

# Exporter Health
tsmetrics_scrape_duration_seconds{target}
tsmetrics_scrape_errors_total{target, error_type}
tsmetrics_api_requests_total{endpoint}
```

## Build & Development

### Build System

- **Build:** `make build` (uses ldflags for version/buildTime)
- **Development:** `make dev` (live reload via `air` if available)
- **Testing:** `make test`
- **Docker:** `make docker-build`, `make docker-run`

### Key Files

- `main.go` — Main application with exporter logic and tsnet integration
- `Makefile` — Build/dev/docker targets
- `.air.toml` — Live-reload configuration
- `.env.example` — All environment variables
- `deploy/kubernetes.yaml` — K8s deployment manifest
- `deploy/docker-compose.yaml` — Docker Compose setup

## Environment Variables

### Core Configuration

- `PORT` — HTTP listen port (default: 9100)
- `ENV` — `production`/`prod` binds 0.0.0.0, otherwise 127.0.0.1
- `SCRAPE_INTERVAL` — Device discovery interval (default: 30s)

### Tailscale API Access

- `OAUTH_CLIENT_ID` — Tailscale OAuth Client ID (required)
- `OAUTH_CLIENT_SECRET` — Tailscale OAuth Client Secret (required)
- `TAILNET_NAME` — Tailnet name or "-" for default (required)
- `OAUTH_TOKEN` — Direct Bearer token (alternative to OAuth2 flow)

### tsnet Configuration

- `USE_TSNET` (true|false) — Enable Tailscale tsnet integration
- `TSNET_HOSTNAME` — Hostname in Tailnet (default: tsmetrics)
- `TSNET_STATE_DIR` — Persistent state directory (default: /tmp/tsnet-tsmetrics)
- `TSNET_TAGS` — Comma-separated tags for tsnet device
- `REQUIRE_EXPORTER_TAG` — Enforce "exporter" tag requirement

### Scraping Configuration

- `CLIENT_METRICS_TIMEOUT` — Timeout for client metrics (default: 10s)
- `MAX_CONCURRENT_SCRAPES` — Parallel scrapes (default: 10)
- `TEST_DEVICES` — Comma-separated device filter for development

### Build Metadata

- `VERSION` — Override build version
- `BUILD_TIME` — Override build time

## Agent Tasks

### 1. Build & Test

```bash
# Dependency management
go mod tidy

# Testing
go test -v ./...

# Build with metadata
go build -ldflags "-X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}" -o bin/tsmetrics .
```

### 2. Development Runtime Checks

**Standalone Mode:**

```bash
# Environment setup
export ENV=development
export PORT=9100
export OAUTH_CLIENT_ID=mock_client_id
export OAUTH_CLIENT_SECRET=mock_client_secret
export TAILNET_NAME=mock_tailnet
export TEST_DEVICES=gateway-1,gateway-2

# Start development server
make dev

# Verification
curl http://127.0.0.1:9100/health    # -> 200 OK
curl http://127.0.0.1:9100/metrics   # -> Prometheus format
curl http://127.0.0.1:9100/debug     # -> JSON debug info
```

**tsnet Mode:**

```bash
# Environment setup
export USE_TSNET=true
export TSNET_HOSTNAME=tsmetrics-dev
export TSNET_TAGS=exporter
export REQUIRE_EXPORTER_TAG=true

# Start with tsnet
make dev-tsnet

# Verification
# 1. Check tsnet startup logs
# 2. Verify listener on Tailscale IP
# 3. Confirm device appears in `tailscale status`
# 4. Test HTTP endpoints over Tailnet
```

### 3. Testing Requirements

**Essential Tests:**

```go
// HTTP endpoints
func TestHealthEndpoint(t *testing.T) {
    // Verify /health returns 200 when API accessible
    // Verify /health returns 500 when API unreachable
}

func TestMetricsEndpoint(t *testing.T) {
    // Verify /metrics returns valid Prometheus format
    // Verify required metric families are present
}

// Data processing
func TestTailscaleAPIResponse(t *testing.T) {
    // Test API response parsing with mock data
    // Verify device struct population
    // Test input validation
}

func TestClientMetricsParsing(t *testing.T) {
    // Test Prometheus format parsing
    // Verify label extraction
    // Test malformed input handling
}

func TestAPIMetricsGeneration(t *testing.T) {
    // Test API data -> Prometheus metrics conversion
    // Verify metric types and labels
    // Test edge cases (missing fields, etc.)
}

// Configuration
func TestConfigLoading(t *testing.T) {
    // Test environment variable parsing
    // Test default values
    // Test validation
}
```

**Optional Integration Tests:**

```go
//go:build integration

func TestTailscaleAPIIntegration(t *testing.T) {
    // Only run with real OAuth credentials
    if os.Getenv("INTEGRATION_TEST") != "true" {
        t.Skip()
    }
    // Test real API connectivity
}
```

## Deployment

### Docker

**Standalone:**

```bash
docker run -d \
  -e OAUTH_CLIENT_ID=${OAUTH_CLIENT_ID} \
  -e OAUTH_CLIENT_SECRET=${OAUTH_CLIENT_SECRET} \
  -e TAILNET_NAME=${TAILNET_NAME} \
  -p 9100:9100 \
  ghcr.io/sbaerlocher/tsmetrics:latest
```

**With tsnet:**

```bash
docker run -d \
  -e USE_TSNET=true \
  -e TSNET_HOSTNAME=tsmetrics \
  -e TSNET_TAGS=exporter \
  -e OAUTH_CLIENT_ID=${OAUTH_CLIENT_ID} \
  -e OAUTH_CLIENT_SECRET=${OAUTH_CLIENT_SECRET} \
  -e TAILNET_NAME=${TAILNET_NAME} \
  -v tsnet-state:/tmp/tsnet-state \
  ghcr.io/sbaerlocher/tsmetrics:latest
```

### Kubernetes

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: tailscale-oauth
type: Opaque
stringData:
  client-id: "your-oauth-client-id"
  client-secret: "your-oauth-client-secret"
  tailnet-name: "your-tailnet"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tsmetrics
spec:
  replicas: 1
  selector:
    matchLabels:
      app: tsmetrics
  template:
    metadata:
      labels:
        app: tsmetrics
    spec:
      containers:
      - name: tsmetrics
        image: ghcr.io/sbaerlocher/tsmetrics:latest
        ports:
        - containerPort: 9100
        env:
        - name: USE_TSNET
          value: "true"
        - name: TSNET_HOSTNAME
          value: "tsmetrics"
        - name: TSNET_TAGS
          value: "exporter"
        - name: OAUTH_CLIENT_ID
          valueFrom:
            secretKeyRef:
              name: tailscale-oauth
              key: client-id
        - name: OAUTH_CLIENT_SECRET
          valueFrom:
            secretKeyRef:
              name: tailscale-oauth
              key: client-secret
        - name: TAILNET_NAME
          valueFrom:
            secretKeyRef:
              name: tailscale-oauth
              key: tailnet-name
        volumeMounts:
        - name: tsnet-state
          mountPath: /tmp/tsnet-state
      volumes:
      - name: tsnet-state
        emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: tsmetrics
  labels:
    app: tsmetrics
spec:
  ports:
  - port: 9100
    targetPort: 9100
    name: metrics
  selector:
    app: tsmetrics
```

### Prometheus Configuration

```yaml
scrape_configs:
  - job_name: 'tailscale-metrics'
    static_configs:
      - targets: ['tsmetrics.tailnet.ts.net:9100']  # tsnet mode
      # - targets: ['tsmetrics:9100']                # k8s mode
    scrape_interval: 60s
    metrics_path: /metrics
```

## Implementation Architecture

### Core Components

```go
// Main exporter structure
type Exporter struct {
    apiClient           *APIClient
    httpClient          *http.Client
    metricsTracker      *DeviceMetricsTracker
    config              Config
}

// Device discovery and API metrics
func (e *Exporter) updateMetrics() error {
    // 1. Fetch devices from API
    devices := e.fetchDevices()

    // 2. Update API-based metrics
    e.updateAPIMetrics(devices)

    // 3. Scrape client metrics concurrently
    e.scrapeClientMetrics(devices)

    return nil
}

// Concurrent client metrics collection
func (e *Exporter) scrapeClientMetrics(devices []Device) error {
    sem := make(chan struct{}, e.config.MaxConcurrentScrapes)
    var wg sync.WaitGroup

    for _, device := range devices {
        if !device.Online || !hasRequiredTag(device) {
            continue
        }
        // Scrape device metrics with semaphore
    }
}
```

### Error Handling Strategy

- **API Errors:** Log, increment metrics, continue operation with stale data
- **Individual Device Errors:** Skip failed devices, don't fail entire collection
- **Metric Registry:** Clean up stale device metrics to prevent memory leaks
- **Network Errors:** Retry with exponential backoff (for API calls)

### Security Considerations

- **Input Validation:** Validate all hostnames to prevent injection attacks
- **OAuth Token Security:** Use OAuth2 client credentials flow, handle token refresh
- **tsnet State:** Secure state directory permissions in production
- **Network Access:** Restrict scraping to devices with appropriate tags

## Performance Requirements

- **Concurrent Scraping:** Configurable parallelism (default 10)
- **Timeouts:** 10s for client metrics, 30s for API calls
- **Memory Management:** Automatic cleanup of stale device metrics
- **HTTP Client:** Connection pooling and reuse

## Success Criteria

An agent implementation is successful when it can:

1. **Build:** Clean build with correct ldflags and version metadata
2. **Test:** All essential tests pass with meaningful coverage
3. **Run Standalone:** HTTP server responds correctly on 127.0.0.1:9100
4. **Run tsnet:** Successfully joins Tailnet and serves metrics over Tailscale
5. **Metrics:** Exports valid Prometheus metrics with both API and client data
6. **Health:** Health endpoint accurately reflects API connectivity
7. **Error Handling:** Graceful degradation when devices are unreachable
8. **Production:** Stable operation in Docker/Kubernetes environments

### Minimal Viable Implementation

Without real Tailscale credentials, the implementation should:

- Successfully build and start HTTP server
- Handle TEST_DEVICES environment variable for mock data
- Export well-formed Prometheus metrics
- Pass all unit tests
- Demonstrate proper error handling

### Full Production Implementation

With real Tailscale credentials:

- Complete API integration with OAuth2 flow
- Successful client metrics collection from live devices
- tsnet device registration and operation
- Comprehensive metrics coverage as specified above

## Development Workflow

```bash
# Initial setup
cp .env.example .env
# Edit .env with your Tailscale credentials

# Development cycle
make dev          # Standalone development with live reload
make dev-tsnet    # tsnet development mode
make test         # Run test suite
make build        # Production build

# Docker workflow
make docker-build
make docker-run

# Deployment
make k8s-deploy   # Deploy to Kubernetes
```

## Troubleshooting

**Common Issues:**

- **OAuth Errors:** Verify client ID/secret and tailnet name
- **tsnet Auth:** May require interactive login on first run
- **Port Conflicts:** Ensure 9100 is available or set PORT environment variable
- **Device Scraping:** Check that target devices have metrics enabled on port 5252
- **Network Connectivity:** Verify firewall rules allow HTTP access to device metrics

This specification balances comprehensive functionality with practical implementation constraints. The focus is on delivering reliable observability for Tailscale networks through a combination of API metadata and live device metrics.
