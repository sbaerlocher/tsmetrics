# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.1] - 2025-12-21

### Fixed

- Update Dockerfile git dependency to version 2.52 for Alpine Linux 3.23 compatibility
- Fix CI dependency review to allow Google Patent License for golang.org/x packages

### Changed

- Remove unnecessary ingress resources from Helm charts and Kustomize deployments (tsnet handles external access)

### Dependencies

- Bump golang.org/x/oauth2 from 0.31.0 to 0.34.0
- Bump golang.org/x/time from 0.13.0 to 0.14.0
- Bump tailscale.com from 1.86.5 to 1.92.2
- Bump actions/checkout from 5 to 6
- Bump actions/upload-artifact from 4 to 6
- Bump github/codeql-action from 3 to 4
- Bump helm/kind-action from 1.12.0 to 1.13.0

## [1.0.0] - 2025-09-07

### Added

- **Complete Tailscale Prometheus Exporter**: Comprehensive monitoring solution for Tailscale networks
- **Dual Data Sources**: Combines Tailscale REST API metadata with live device client metrics
- **tsnet Integration**: Optional Tailscale network integration for secure internal access
- **Concurrent Scraping**: Configurable parallel device metrics collection with rate limiting
- **Production-Ready Deployments**:
  - Docker/Docker Compose support with multi-stage builds
  - Kubernetes deployments via Helm charts and Kustomize
  - Systemd service configuration
- **Modern Go Architecture**: Standard Go project structure with clear package boundaries
- **Comprehensive Metrics Collection**:
  - Device management and inventory from Tailscale API
  - Network performance metrics from device clients
  - Connectivity and routing configuration
  - Health monitoring and alerting
- **Security Features**:
  - OAuth2 authentication with Tailscale API
  - Tag-based device filtering and access control
  - Input validation and injection prevention
  - No hardcoded secrets
- **Development Tools**:
  - Live reload development environment
  - Comprehensive test suite
  - CI/CD pipeline with GitHub Actions
  - Automated releases with GoReleaser
- **Documentation**:
  - Complete README with all deployment scenarios
  - Metrics reference and Grafana dashboard examples
  - Troubleshooting guide and migration documentation
  - API reference and advanced usage examples

### Security

- OAuth2 client credentials authentication
- Tag-based access control for device scraping
- Input validation for all external data
- Secure container builds with distroless base images
- Vulnerability scanning in CI/CD pipeline

### Performance

- Connection pooling for HTTP clients
- Configurable concurrent device scraping
- Memory-efficient metrics collection with automatic cleanup
- Optimized Docker builds with layer caching
- Rate limiting and circuit breaker patterns

### Deployment

- Multi-platform support (Linux, macOS, Windows on AMD64/ARM64)
- Container images available at ghcr.io/sbaerlocher/tsmetrics
- Helm charts for Kubernetes deployment
- Kustomize overlays for environment-specific configurations
- Comprehensive health checks and monitoring endpoints

[1.0.1]: https://github.com/sbaerlocher/tsmetrics/releases/tag/v1.0.1
[1.0.0]: https://github.com/sbaerlocher/tsmetrics/releases/tag/v1.0.0
