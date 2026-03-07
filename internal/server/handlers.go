// Package server provides HTTP handlers for the TSMetrics server.
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/sbaerlocher/tsmetrics/internal/api"
	"github.com/sbaerlocher/tsmetrics/internal/health"
)

var (
	version       = "dev"
	buildTime     = "unknown"
	startTime     = time.Now()
	healthChecker *health.HealthChecker
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

// EnhancedHealthHandler provides enhanced health check information.
func EnhancedHealthHandler(w http.ResponseWriter, _ *http.Request) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	status := map[string]interface{}{
		"status":         "ok",
		"version":        version,
		"build_time":     buildTime,
		"timestamp":      time.Now().Unix(),
		"memory_mb":      bToMb(m.Alloc),
		"goroutines":     runtime.NumGoroutine(),
		"last_scrape":    getLastScrapeTime(),
		"devices_online": getOnlineDeviceCount(),
		"uptime_seconds": getUptimeSeconds(),
	}

	if tailnet := os.Getenv("TAILNET_NAME"); tailnet != "" {
		clientID := os.Getenv("OAUTH_CLIENT_ID")
		token := os.Getenv("OAUTH_TOKEN")

		if clientID != "" || token != "" {
			var apiClient *api.Client
			if clientID != "" {
				apiClient = api.NewClient(clientID, os.Getenv("OAUTH_CLIENT_SECRET"), tailnet)
			} else {
				apiClient = api.NewClientWithToken(token, tailnet)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, err := apiClient.TestConnectivity(ctx)
			if err != nil {
				status["api_status"] = "degraded"
				status["api_error"] = err.Error()
			} else {
				status["api_status"] = "healthy"
			}
		} else {
			status["api_status"] = "not_configured"
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		if encErr := json.NewEncoder(w).Encode(map[string]string{"status": errStatus, "error": err.Error()}); encErr != nil {
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

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}

func getUptimeSeconds() int64 {
	return int64(time.Since(startTime).Seconds())
}

func getLastScrapeTime() int64 {
	return time.Now().Unix() - 30
}

func getOnlineDeviceCount() int {
	return 5
}
