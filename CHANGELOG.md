# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.1.2] - 2026-04-04

### Fixed

- Prevent nil pointer dereference in deferred `server.Close()` when tsnet
  server initialization fails

## [1.1.1] - 2026-04-04

### Fixed

- Register `kubestore` provider via blank import (`_ "tailscale.com/ipn/store/kubestore"`)
  so `store.New("kube:...")` recognizes the `kube:` prefix at runtime

## [1.1.0] - 2026-04-04

### Added

- Support Kubernetes Secret as tsnet state backend via `TSNET_STATE_SECRET` env var; uses
  Tailscale's built-in `kube:` store prefix (`tailscale.com/ipn/store`) — no PVC required
- Helm: new `storage.type: k8s-secret` (now default) with `storage.kubeSecret` configuration
- Helm: ServiceAccount, Role, and RoleBinding templates for K8s Secret state store access
- Helm: empty state Secret created automatically when `storage.kubeSecret.create: true`
- Kustomize: RBAC resources (ServiceAccount, Role, RoleBinding) in base layer
- `TSNET_STATE_SECRET` fallback: existing `TSNET_STATE_DIR`/PVC path still supported

### Security

- Truncate all API-sourced Prometheus label values to 128 characters to prevent cardinality bombs
  from compromised devices (`truncateLabel` on `d.OS`, `d.User`, `d.ClientVersion`, routes, regions)
- Limit client metrics response body to 10 MB via `io.LimitReader` to prevent OOM from
  malicious device responses
- Remove version, build time, memory usage, goroutine count, and device count from
  unauthenticated `/health` endpoint to prevent information disclosure
- Scope RBAC Role `resourceNames` to only the state Secret for get/update/patch operations
- Add `sizeLimit: 100Mi` to all tmp emptyDir volumes to prevent node disk exhaustion
- Remove open NetworkPolicy ingress rule (`from: []`) that bypassed namespace restrictions
- Add conflict warning when both `TSNET_STATE_SECRET` and `TSNET_STATE_DIR` are set

### Fixed

- Helm Secret name now uses `fullname`-secrets template instead of hardcoded `tsmetrics-secrets`
- Helm Secret keys aligned between `secret.yaml` and `deployment.yaml` secretKeyRef
- Remove `TAILNET_NAME` from ConfigMap (was duplicated with Secret, could cause override conflicts)
- Add `/tmp` emptyDir volume in Helm k8s-secret storage mode (was missing, broke
  `readOnlyRootFilesystem: true`)
- Delete orphaned `pvc.yaml` from production overlay
- Pin Kustomize image tags to `v1.0.4` instead of `latest`
- Remove dead code: `runtime.ReadMemStats`, `bToMb`, `getOnlineDeviceCount` and
  unused `runtime` import from health handler
- Fix `truncateLabel` to slice by runes instead of bytes (UTF-8 safety)

### Changed

- Kustomize production overlay: replaced PVC volume mount with `TSNET_STATE_SECRET` env var
- Helm default `storage.type` changed from `pvc` to `k8s-secret`
- Downgrade kube store internal log level from Info to Debug (noisy Tailscale library logs)

### Dependencies

- Bump `tailscale.com` to v1.96.5

## [1.0.4] - 2026-03-22

### Security

- Activate `SecurityHeadersMiddleware → RateLimitMiddleware → AuthenticationMiddleware` on all
  endpoints in both standalone and tsnet server modes; previously no middleware was applied
- Add opt-in Bearer token authentication via `METRICS_TOKEN` env var; `/metrics` and `/debug`
  now require authorization when the variable is set
- Fix auth whitelist to cover `/startupz` and `/healthz` in addition to `/health`, `/livez`, and
  `/readyz`; Kubernetes startup probes returned 401 with token auth enabled
- Replace timing-unsafe `ValidateToken` map lookup with `SecureValidateToken` constant-time
  comparison; the loop no longer breaks early to prevent timing oracle attacks
- Harden `getClientID` to use `net.SplitHostPort(r.RemoteAddr)` exclusively; `X-Forwarded-For`
  and `X-Real-IP` headers can be forged and were removed from rate-limit identity resolution
- Extend SSRF blocklist to all RFC 1918 ranges, CGNAT `100.64.0.0/10` (Tailscale device IPs),
  `0.0.0.0/8`, loopback, and link-local addresses
- Switch `validateHostname` from a blocklist to an allowlist regexp `[a-zA-Z0-9.\-\[\]:]+`;
  the previous blocklist was missing `/` and `:` allowing potential port manipulation
- Replace `strings.HasPrefix` auth whitelist with exact-match plus sub-path checks to prevent
  future routes like `/health-admin` from silently inheriting the bypass
- Remove internal error details (`err.Error()`) from all health probe JSON responses; errors
  are now logged server-side only
- Fix rate limiter `Cleanup()` float equality from `==` to `>=` so stale per-IP entries are
  reliably removed despite floating-point accumulation

### Fixed

- `getLastScrapeTime()` was returning a hardcoded `time.Now() - 30s` instead of the actual
  last scrape timestamp from the Prometheus gauge
- `getOnlineDeviceCount()` was returning a hardcoded `5` instead of the real device count
- `WriteHealthResponse` was constructing JSON via `fmt.Sprintf` with unsanitised message
  interpolation; replaced with `json.NewEncoder`
- API health check now uses a lightweight `TestConnectivity` call instead of a full device
  fetch, reducing unnecessary API load during health probes
- Add `first_scrape_complete` boolean to `/health` response so consumers can distinguish zero
  online devices from not-yet-scraped state

### Changed

- Add Claude Code Review via shared reusable workflow (`sbaerlocher/.github`)
- Update all shared workflow refs from `2026-03-20` to `2026-03-22`
- Switch Helm CI job from `helm-test.yml` to `ci-gitops.yml`
- Add `status` gate job to allow single required check for branch protection
- Add `CODEOWNERS` assigning all files to `@sbaerlocher`
- Add `.prettierrc` for consistent formatting config

### Dependencies

- Pin `gcr.io/distroless/static-debian12` Docker tag to `a932952`
- Bump alpine Docker image to v3.23
- Update all non-major Go and action dependencies

## [1.0.3] - 2026-03-08

### Changed

- Replace multi-stage Go build Dockerfile with GitHub Release binary download (distroless base image)
- Switch from `scratch` to `gcr.io/distroless/static-debian12:nonroot` for better security scanning
  and built-in nonroot user
- Remove Windows build targets from GoReleaser (linux and darwin only)
- Use CHANGELOG.md for release notes instead of auto-generated commit changelog
- Remove emojis from GoReleaser release template

## [1.0.2] - 2026-03-07

### Security

- Prevent XSS in health probe error responses by using JSON encoding instead of string concatenation
- Add context propagation to all HTTP requests (`http.NewRequestWithContext`)
- Fix potential log injection by sanitizing structured log values
- Fix unchecked error returns on `resp.Body.Close()` and `server.Close()`

### Changed

- Refactor duplicated health probe handlers into shared `healthProbeHandler`
- Remove unused parameters from `initializeHealthChecker`
- Remove always-nil error return from `processDeviceGroup`
- Migrate CI from Dependabot to Renovate with shared workflows

### Dependencies

- Bump Go from 1.25 to 1.26
- Bump tailscale.com from 1.92.4 to 1.96.0
- Bump golang.org/x/oauth2 from 0.34.0 to 0.35.0
- Bump filippo.io/edwards25519 from 1.1.0 to 1.2.0
- Bump golang Docker image from 1.25-alpine to 1.26-alpine

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

- Multi-platform support (Linux, macOS on AMD64/ARM64)
- Container images available at ghcr.io/sbaerlocher/tsmetrics
- Helm charts for Kubernetes deployment
- Kustomize overlays for environment-specific configurations
- Comprehensive health checks and monitoring endpoints

[1.1.2]: https://github.com/sbaerlocher/tsmetrics/compare/v1.1.1...v1.1.2
[1.1.1]: https://github.com/sbaerlocher/tsmetrics/compare/v1.1.0...v1.1.1
[1.1.0]: https://github.com/sbaerlocher/tsmetrics/compare/v1.0.4...v1.1.0
[1.0.4]: https://github.com/sbaerlocher/tsmetrics/compare/v1.0.3...v1.0.4
[1.0.3]: https://github.com/sbaerlocher/tsmetrics/releases/tag/v1.0.3
[1.0.2]: https://github.com/sbaerlocher/tsmetrics/releases/tag/v1.0.2
[1.0.1]: https://github.com/sbaerlocher/tsmetrics/releases/tag/v1.0.1
[1.0.0]: https://github.com/sbaerlocher/tsmetrics/releases/tag/v1.0.0
