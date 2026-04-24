// Package server provides HTTP handlers for the TSMetrics server.
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/sbaerlocher/tsmetrics/internal/api"
	"github.com/sbaerlocher/tsmetrics/internal/health"
	"github.com/sbaerlocher/tsmetrics/internal/metrics"
)

var (
	version       = "dev"
	buildTime     = "unknown"
	startTime     = time.Now()
	healthChecker *health.HealthChecker
	// apiClient is initialized once at startup when OAuth credentials are
	// configured; reused across every /health request to avoid re-running the
	// OAuth2 client-credentials flow and allocating a new http.Transport per
	// probe (Prometheus/Kubernetes can hit /health every few seconds).
	//
	// Contract: SetAPIClient MUST be called from main() before the HTTP server
	// starts serving traffic, and MUST NOT be called again afterwards. Handler
	// goroutines read this pointer without synchronisation, so a post-startup
	// rewrite would be a data race. Runtime credential rotation is out of
	// scope today; if it is ever added, convert this to an atomic.Pointer.
	apiClient *api.Client
)

// SetVersion sets the global version and build time for handlers.
func SetVersion(v string, bt string) {
	version = v
	buildTime = bt
}

// SetHealthChecker sets the global health checker for handlers.
func SetHealthChecker(hc *health.HealthChecker) {
	healthChecker = hc
}

// SetAPIClient stores the singleton API client used by health handlers.
// Passing nil marks the API as not configured; subsequent health probes
// will report "not_configured" instead of attempting a connectivity check.
func SetAPIClient(c *api.Client) {
	apiClient = c
}

// EnhancedHealthHandler provides enhanced health check information.
func EnhancedHealthHandler(w http.ResponseWriter, _ *http.Request) {
	lastScrape := getLastScrapeTime()
	status := map[string]interface{}{
		"status":                "ok",
		"timestamp":             time.Now().Unix(),
		"first_scrape_complete": lastScrape != 0,
		"uptime_seconds":        getUptimeSeconds(),
	}

	if apiClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if _, err := apiClient.TestConnectivity(ctx); err != nil {
			slog.Warn("API connectivity check failed", "error", err)
			status["api_status"] = "degraded"
		} else {
			status["api_status"] = "healthy"
		}
	} else {
		status["api_status"] = "not_configured"
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		slog.Error("failed to encode health status response", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func DebugHandler(w http.ResponseWriter, _ *http.Request) {
	info := map[string]interface{}{
		"version":    version,
		"build_time": buildTime,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(info); err != nil {
		slog.Error("failed to encode debug info response", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// healthProbeHandler handles a health probe check with JSON response encoding to prevent XSS.
func healthProbeHandler(w http.ResponseWriter, r *http.Request, timeout time.Duration, nullStatus, okStatus, errStatus string, checkFn func(ctx context.Context) error) {
	if healthChecker == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(map[string]string{"status": nullStatus}); err != nil {
			slog.Error("failed to write health response", "error", err)
		}
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	if err := checkFn(ctx); err != nil {
		slog.Warn("health probe check failed", "probe", errStatus, "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		if encErr := json.NewEncoder(w).Encode(map[string]string{"status": errStatus}); encErr != nil {
			slog.Error("failed to write health error response", "error", encErr)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": okStatus}); err != nil {
		slog.Error("failed to write health ok response", "error", err)
	}
}

// Kubernetes Health Endpoints

// LivenessHandler provides liveness probe endpoint for Kubernetes.
func LivenessHandler(w http.ResponseWriter, r *http.Request) {
	healthProbeHandler(w, r, 5*time.Second, "ok", "ok", "unhealthy", func(ctx context.Context) error {
		return healthChecker.LivenessCheck(ctx)
	})
}

// ReadinessHandler provides readiness probe endpoint for Kubernetes.
func ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	healthProbeHandler(w, r, 10*time.Second, "not configured", "ready", "not ready", func(ctx context.Context) error {
		return healthChecker.ReadinessCheck(ctx)
	})
}

// StartupHandler provides startup probe endpoint for Kubernetes.
func StartupHandler(w http.ResponseWriter, r *http.Request) {
	healthProbeHandler(w, r, 30*time.Second, "ok", "started", "not started", func(ctx context.Context) error {
		return healthChecker.StartupCheck(ctx)
	})
}

func DetailedHealthHandler(w http.ResponseWriter, r *http.Request) {
	if healthChecker == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		if _, err := w.Write([]byte(`{"status":"not configured"}`)); err != nil {
			slog.Error("failed to write detailed health not configured response", "error", err)
		}
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	status := healthChecker.GetHealthStatus(ctx)
	httpStatus := health.DetermineHTTPStatus(status.Overall)

	health.WriteHealthResponse(w, status, httpStatus)
}

func getUptimeSeconds() int64 {
	return int64(time.Since(startTime).Seconds())
}

func getLastScrapeTime() int64 {
	return metrics.GetLastScrapeTime()
}
