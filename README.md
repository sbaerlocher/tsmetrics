# tsmetrics

A comprehensive Tailscale Prometheus exporter that combines API metadata with live device metrics for complete network observability.

## Features

- **Dual Data Sources**: Combines Tailscale REST API metadata with live device client metrics
- **Comprehensive Metrics**: Device status, network traffic, routing configuration, and health monitoring
- **tsnet Integration**: Optional Tailscale network integration for secure internal access
- **Concurrent Scraping**: Configurable parallel device metrics collection
- **Production Ready**: Docker/Kubernetes deployments with proper health checks
- **Memory Efficient**: Automatic cleanup of stale device metrics

## Quick Start

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
| `REQUIRE_EXPORTER_TAG` | Enforce "exporter" tag requirement | `false` |

### Performance Tuning

| Environment Variable | Description | Default |
|---------------------|-------------|---------|
| `CLIENT_METRICS_TIMEOUT` | Device metrics timeout | `10s` |
| `MAX_CONCURRENT_SCRAPES` | Parallel device scrapes | `10` |
| `SCRAPE_INTERVAL` | Device discovery interval | `30s` |

## Metrics Reference

### Device Management (from Tailscale API)

```
tailscale_device_count
tailscale_device_info{device_id, device_name, online, os, version}
tailscale_device_authorized{device_id, device_name}
tailscale_device_last_seen_timestamp{device_id, device_name}
tailscale_device_user{device_id, device_name, user_email}
tailscale_device_machine_key_expiry{device_id, device_name}
```

### Network Configuration (from Tailscale API)

```
tailscale_device_routes_advertised{device_id, device_name, route}
tailscale_device_routes_enabled{device_id, device_name, route}
tailscale_device_exit_node{device_id, device_name}
tailscale_device_subnet_router{device_id, device_name}
```

### Network Performance (from device client metrics)

```
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

### Exporter Health

```
tsmetrics_scrape_duration_seconds{target}
tsmetrics_scrape_errors_total{target, error_type}
```

## Kubernetes Deployment

See [deploy/kubernetes.yaml](deploy/kubernetes.yaml) for a complete example.

```bash
# Apply the deployment
make k8s-deploy

# Verify pods
kubectl get pods -l app=tsmetrics

# Check logs
kubectl logs -l app=tsmetrics -f
```

## Prometheus Configuration

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
make dev-deps

# Start development server with live reload
make dev

# Run with tsnet for testing
make dev-tsnet

# Run tests
make test

# Build and run locally
make build
make run
```

### Testing with Mock Devices

For development without real Tailscale credentials:

```bash
export TEST_DEVICES="gateway-1,gateway-2,server-3"
make run
```

## Architecture

tsmetrics operates in two phases:

1. **Device Discovery**: Fetches device inventory from Tailscale REST API
2. **Metrics Collection**: Concurrently scrapes client metrics from each online device with the "exporter" tag

### Security Considerations

- **OAuth2 Flow**: Uses client credentials for secure API access
- **Input Validation**: Validates all hostnames to prevent injection attacks
- **Tag-Based Access**: Only scrapes devices with the "exporter" tag
- **Rate Limiting**: Configurable concurrent scraping limits

### Performance Features

- **Connection Pooling**: Reuses HTTP connections for efficiency
- **Concurrent Scraping**: Parallel device metrics collection
- **Memory Management**: Automatic cleanup of stale device metrics
- **Circuit Breaker**: Protects against API failures (planned)

## Troubleshooting

### Common Issues

**OAuth2 Authentication Failed**

- Verify `OAUTH_CLIENT_ID` and `OAUTH_CLIENT_SECRET`
- Check that the OAuth client has appropriate scopes
- Ensure `TAILNET_NAME` matches your tailnet exactly

**No Devices Discovered**

- Confirm API credentials are correct
- Check that devices are online in Tailscale admin console
- Verify network connectivity to Tailscale API

**Client Metrics Not Available**

- Enable metrics on target devices: `tailscale set --metrics-listen-addr=0.0.0.0:5252`
- Ensure devices have the "exporter" tag
- Check firewall rules allow HTTP access to port 5252

**tsnet Authentication Issues**

- First run may require interactive authentication
- Check tsnet state directory permissions
- Verify `TSNET_TAGS` includes required tags

### Debug Mode

Enable debug logging and access debug endpoint:

```bash
# Check application status
curl http://localhost:9100/debug

# View detailed logs
docker logs tsmetrics -f

# Test specific device
export TEST_DEVICES="specific-device-name"
make run
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Ensure all tests pass: `make test`
5. Submit a pull request

## License

MIT License - see [LICENSE](LICENSE) for details.

## Related Projects

- [Tailscale](https://tailscale.com/) - Zero config VPN
- [Prometheus](https://prometheus.io/) - Monitoring system
- [Grafana](https://grafana.com/) - Visualization platform
