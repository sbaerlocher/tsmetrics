# Agent Instructions — tsmetrics

## Purpose

Tailscale Prometheus Exporter that combines API metadata with live device metrics for complete network observability.

### Device Management (from Tailscale API)

- tailscale_device_count
- tailscale_device_info{device_id, device_name, os, version}
- tailscale_device_authorized{device_id, device_name}
- tailscale_device_last_seen_timestamp{device_id, device_name}
- tailscale_device_user{device_id, device_name, user_email}
- tailscale_device_machine_key_expiry{device_id, device_name}
- tailscale_device_update_available{device_id, device_name}
- tailscale_device_created_timestamp{device_id, device_name}
- tailscale_device_external{device_id, device_name}
- tailscale_device_blocks_incoming_connections{device_id, device_name}
- tailscale_device_ephemeral{device_id, device_name}
- tailscale_device_multiple_connections{device_id, device_name}
- tailscale_device_tailnet_lock_error{device_id, device_name}

### Network Configuration (from Tailscale API)

- tailscale_device_routes_advertised{device_id, device_name, route}
- tailscale_device_routes_enabled{device_id, device_name, route}
- tailscale_device_exit_node{device_id, device_name}
- tailscale_device_subnet_router{device_id, device_name}

### Connectivity & Performance (from Tailscale API)

- tailscale_device_latency_ms{device_id, device_name, derp_region, preferred}
- tailscale_device_endpoints_total{device_id, device_name}
- tailscale_device_client_supports{device_id, device_name, feature}
- tailscale_device_posture_serial_numbers_total{device_id, device_name}

## Context

- **Project:** tsmetrics - Tailscale Prometheus Exporter
- **Language:** Go with Prometheus Client Library and Tailscale tsnet
- **Architecture:** Single binary aggregates Tailscale API data + device client metrics
- **Deployment:** Docker/Kubernetes ready with optional tsnet integration
- **Entry Point:** `cmd/tsmetrics/main.go`

## Core Functionality

### What tsmetrics does

1. **Device Discovery:** Fetch all Tailscale devices via REST API (`/api/v2/tailnet/{tailnet}/devices`)
2. **API Metrics Collection:** Extract device metadata, authorization status, routing configuration,
   user assignments from API
3. **Client Metrics Scraping:** Collect live performance data from each device's metrics endpoint
   (`http://device:5252/metrics`)
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

### Build System (dde + just)

Development runs inside a container managed by **[dde](https://dde.sh)** —
a Docker dev environment manager that owns the container lifecycle and provides a shared
Traefik reverse proxy for `*.test` hostnames (app reachable at `https://tsmetrics.test`).
Project config lives in `.dde/config.yml`; compose stack in `docker-compose.yml`.

**just** only wraps multi-step or parameterized commands; trivial one-liners are invoked directly.

Bootstrap (one-time per machine): `brew tap whatwedo/tap && brew install dde && dde system:up`.

just recipes (`just` to list):

- `just dev` — start dev container + tail logs
- `just test` — run test suite in container
- `just build` — build binary in container with version ldflags
- `just lint` — run golangci-lint in container

dde project plugins (`.dde/plugins/*.sh`, auto-registered):

- `dde project:exec:observability:up` — start local Prometheus + Grafana (compose profile `observability`)
- `dde project:exec:observability:down` — stop local observability stack

Direct invocations (no just wrapper):

- `dde project:up|down|restart|logs|exec ...` — container lifecycle
- `helm install|upgrade|uninstall|template|lint tsmetrics deploy/helm/tsmetrics` — Helm
- `kubectl apply -k deploy/kustomize/overlays/{development,production}` — Kustomize

Git staging note: `.gitignore` rule `tsmetrics` matches `cmd/tsmetrics/` —
use `git add -f cmd/tsmetrics/main.go` for already-tracked files.

### Key Files

- `cmd/tsmetrics/main.go` — Main application with exporter logic and tsnet integration
- `justfile` — Task runner (replaces former Makefile)
- `.dde/config.yml` — dde project config
- `docker-compose.yml` — Dev container (`app`) + optional `prometheus`/`grafana` under profile `observability`
- `Dockerfile` — Multi-stage: `backend-dev` (Air hot reload) + `production` (distroless release binary)
- `.air.toml` — Live-reload configuration (runs inside `backend-dev` container)
- `.env.example` — All environment variables
- `deploy/observability/` — Local Prometheus scrape + Grafana datasource config
- `deploy/helm/` — Helm chart for Kubernetes deployment
- `deploy/kustomize/` — Kustomize overlays for dev/prod

## Environment Variables

### Core Configuration

- `PORT` — HTTP listen port (default: 9100)
- `ENV` — `production`/`prod` binds 0.0.0.0, otherwise 127.0.0.1
- `SCRAPE_INTERVAL` — Device discovery interval (default: 30s)
- `METRICS_TOKEN` — Bearer token to protect all non-health endpoints (optional; omit to allow unauthenticated access)
- `RATE_LIMIT_RPS` — Rate limit requests per second (default: 10)
- `RATE_LIMIT_BURST` — Rate limit burst size (default: 20)

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

All Go commands run inside the dde container:

```bash
just test                                   # go test -v ./...
just build                                  # go build with version ldflags
dde project:exec sh -c "go mod tidy && go mod download"
```

### 2. Development Runtime Checks

**Standalone Mode (API-only, no tsnet):**

Configure `.env` with mock values:

```env
ENV=development
PORT=9100
OAUTH_CLIENT_ID=mock_client_id
OAUTH_CLIENT_SECRET=mock_client_secret
TAILNET_NAME=mock_tailnet
TEST_DEVICES=gateway-1,gateway-2
USE_TSNET=false
```

Start and verify:

```bash
just dev
curl https://tsmetrics.test/health    # -> 200 OK (via Traefik)
curl https://tsmetrics.test/metrics   # -> Prometheus format
curl https://tsmetrics.test/debug     # -> JSON debug info
```

**tsnet Mode:**

```env
USE_TSNET=true
TSNET_HOSTNAME=tsmetrics-dev
TSNET_TAGS=exporter
REQUIRE_EXPORTER_TAG=true
```

State persists in the `tsnet_state` named volume. Start with `just dev`, then:

1. Check tsnet startup logs: `just logs`
2. Verify listener on Tailscale IP
3. Confirm device appears in `tailscale status`
4. Test HTTP endpoints over Tailnet

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
  - job_name: "tailscale-metrics"
    static_configs:
      - targets: ["tsmetrics.tailnet.ts.net:9100"] # tsnet mode
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
- **SSRF:** `ValidateURL` blocks all private/loopback/link-local ranges (not just localhost) — do not weaken this
- **Rate Limiting:** `getClientID` uses `RemoteAddr` only — never trust X-Forwarded-For/X-Real-IP (forgeable)
- **Timing Attacks:** `SecureValidateToken` must never `break` early — always iterate all tokens
- **DevSkim:** Add `// DevSkim: ignore DS162092` on lines that intentionally reference private IPs in security validators

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

# Development cycle (dde container + just)
just dev                 # Start dev container with live reload + logs
just test                # Run test suite in container
just build               # Build binary in container
just lint                # Run golangci-lint in container

# Optional local Prometheus + Grafana (compose profile "observability")
dde project:exec:observability:up

# Production image is built by CI on release (GoReleaser + release.yml)

# Kubernetes deployment (host, direct tooling)
helm install tsmetrics deploy/helm/tsmetrics
kubectl apply -k deploy/kustomize/overlays/production
```

## Troubleshooting

**Common Issues:**

- **OAuth Errors:** Verify client ID/secret and tailnet name
- **tsnet Auth:** May require interactive login on first run
- **Port Conflicts:** Ensure 9100 is available or set PORT environment variable
- **Device Scraping:** Check that target devices have metrics enabled on port 5252
- **Network Connectivity:** Verify firewall rules allow HTTP access to device metrics

This specification balances comprehensive functionality with practical implementation constraints.
The focus is on delivering reliable observability for Tailscale networks through a combination of
API metadata and live device metrics.
