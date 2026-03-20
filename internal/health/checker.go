// Package health provides health checking functionality for TSMetrics components.
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/sbaerlocher/tsmetrics/internal/api"
	"github.com/sbaerlocher/tsmetrics/internal/cache"
	"github.com/sbaerlocher/tsmetrics/internal/metrics"
)

// Status represents the health status of a component.
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusUnhealthy Status = "unhealthy"
	StatusDegraded  Status = "degraded"
)

// CheckResult represents the result of a health check for a specific component.
type CheckResult struct {
	Component   string        `json:"component"`
	Status      Status        `json:"status"`
	Message     string        `json:"message,omitempty"`
	Duration    time.Duration `json:"duration"`
	Timestamp   time.Time     `json:"timestamp"`
	LastSuccess *time.Time    `json:"last_success,omitempty"`
}

// HealthStatus represents the overall health status and individual component checks.
type HealthStatus struct {
	Overall Status                 `json:"overall"`
	Checks  map[string]CheckResult `json:"checks"`
}

// Checker defines the interface for health checking functionality.
type Checker interface {
	LivenessCheck(ctx context.Context) error
	ReadinessCheck(ctx context.Context) error
	StartupCheck(ctx context.Context) error
	GetHealthStatus(ctx context.Context) HealthStatus
}

// ComponentChecker defines the interface for individual component health checks.
type ComponentChecker interface {
	CheckHealth(ctx context.Context) error
	ComponentName() string
}

// HealthChecker manages health checks for multiple components.
type HealthChecker struct {
	components  map[string]ComponentChecker
	mu          sync.RWMutex
	lastChecks  map[string]CheckResult
	startupTime time.Time
}

// NewHealthChecker creates a new health checker instance.
func NewHealthChecker() *HealthChecker {
	return &HealthChecker{
		components:  make(map[string]ComponentChecker),
		lastChecks:  make(map[string]CheckResult),
		startupTime: time.Now(),
	}
}

func (hc *HealthChecker) RegisterComponent(checker ComponentChecker) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.components[checker.ComponentName()] = checker
}

func (hc *HealthChecker) LivenessCheck(ctx context.Context) error {
	// Basic liveness - just check if the process is responsive
	// No external dependencies
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (hc *HealthChecker) ReadinessCheck(ctx context.Context) error {
	// Readiness checks all critical components
	hc.mu.RLock()
	components := make(map[string]ComponentChecker, len(hc.components))
	for name, comp := range hc.components {
		components[name] = comp
	}
	hc.mu.RUnlock()

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	for name, component := range components {
		if err := component.CheckHealth(ctx); err != nil {
			return fmt.Errorf("component %s not ready: %w", name, err)
		}
	}

	return nil
}

func (hc *HealthChecker) StartupCheck(ctx context.Context) error {
	// Startup probe - allows more time for initialization
	if time.Since(hc.startupTime) < 30*time.Second {
		// Still in startup grace period
		return hc.LivenessCheck(ctx)
	}

	// After grace period, use readiness check
	return hc.ReadinessCheck(ctx)
}

func (hc *HealthChecker) GetHealthStatus(ctx context.Context) HealthStatus {
	hc.mu.RLock()
	components := make(map[string]ComponentChecker, len(hc.components))
	for name, comp := range hc.components {
		components[name] = comp
	}
	hc.mu.RUnlock()

	results := make(map[string]CheckResult)
	overallHealthy := true
	degraded := false

	for name, component := range components {
		start := time.Now()
		err := component.CheckHealth(ctx)
		duration := time.Since(start)

		var status Status
		var message string
		var lastSuccess *time.Time

		if err != nil {
			status = StatusUnhealthy
			message = err.Error()
			overallHealthy = false

			// Check if we have a previous successful check
			hc.mu.RLock()
			if prev, exists := hc.lastChecks[name]; exists && prev.Status == StatusHealthy {
				lastSuccess = &prev.Timestamp
			}
			hc.mu.RUnlock()
		} else {
			status = StatusHealthy
			now := time.Now()
			lastSuccess = &now
		}

		if duration > 5*time.Second {
			degraded = true
			if status == StatusHealthy {
				status = StatusDegraded
			}
		}

		result := CheckResult{
			Component:   name,
			Status:      status,
			Message:     message,
			Duration:    duration,
			Timestamp:   time.Now(),
			LastSuccess: lastSuccess,
		}

		results[name] = result
	}

	// Store current results
	hc.mu.Lock()
	hc.lastChecks = results
	hc.mu.Unlock()

	var overall Status
	if !overallHealthy {
		overall = StatusUnhealthy
	} else if degraded {
		overall = StatusDegraded
	} else {
		overall = StatusHealthy
	}

	return HealthStatus{
		Overall: overall,
		Checks:  results,
	}
}

// API Client Health Checker
// APIHealthChecker checks the health of the Tailscale API connection.
type APIHealthChecker struct {
	client *api.Client
}

// NewAPIHealthChecker creates a new API health checker.
func NewAPIHealthChecker(client *api.Client) *APIHealthChecker {
	return &APIHealthChecker{client: client}
}

func (ac *APIHealthChecker) ComponentName() string {
	return "tailscale_api"
}

func (ac *APIHealthChecker) CheckHealth(ctx context.Context) error {
	if ac.client == nil {
		return fmt.Errorf("API client not initialized")
	}

	// Use a lightweight HEAD request instead of a full device fetch
	_, err := ac.client.TestConnectivity(ctx)
	if err != nil {
		return fmt.Errorf("API connectivity check failed: %w", err)
	}

	return nil
}

// Cache Health Checker
// CacheHealthChecker checks the health of the device cache.
type CacheHealthChecker struct {
	cache *cache.DeviceCache
}

// NewCacheHealthChecker creates a new cache health checker.
func NewCacheHealthChecker(cache *cache.DeviceCache) *CacheHealthChecker {
	return &CacheHealthChecker{cache: cache}
}

func (cc *CacheHealthChecker) ComponentName() string {
	return "device_cache"
}

func (cc *CacheHealthChecker) CheckHealth(ctx context.Context) error {
	if cc.cache == nil {
		return fmt.Errorf("cache not initialized")
	}

	// Check cache stats to verify it's functioning
	stats := cc.cache.GetCacheStats()

	// Cache is considered healthy if we can retrieve stats
	// Additional checks could be added here for specific conditions
	if stats.DeviceCount < 0 {
		return fmt.Errorf("cache in invalid state")
	}

	return nil
}

// Performance Monitor Health Checker
// PerformanceHealthChecker checks the health of the performance monitor.
type PerformanceHealthChecker struct {
	monitor *metrics.PerformanceMonitor
}

// NewPerformanceHealthChecker creates a new performance health checker.
func NewPerformanceHealthChecker(monitor *metrics.PerformanceMonitor) *PerformanceHealthChecker {
	return &PerformanceHealthChecker{monitor: monitor}
}

func (pc *PerformanceHealthChecker) ComponentName() string {
	return "performance_monitor"
}

func (pc *PerformanceHealthChecker) CheckHealth(ctx context.Context) error {
	if pc.monitor == nil {
		return fmt.Errorf("performance monitor not initialized")
	}

	if !pc.monitor.IsHealthy() {
		status := pc.monitor.GetHealthStatus()
		if healthy, ok := status["healthy"].(bool); ok && !healthy {
			return fmt.Errorf("performance monitor unhealthy")
		}
	}

	return nil
}

// HTTP Response Helpers
func WriteHealthResponse(w http.ResponseWriter, status HealthStatus, httpStatus int) {
	response := struct {
		Status    Status                 `json:"status"`
		Timestamp string                 `json:"timestamp"`
		Checks    map[string]CheckResult `json:"checks"`
	}{
		Status:    status.Overall,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Checks:    status.Checks,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("failed to write health response", "error", err)
	}
}

func DetermineHTTPStatus(status Status) int {
	switch status {
	case StatusHealthy:
		return http.StatusOK
	case StatusDegraded:
		return http.StatusOK // Still considered healthy for K8s
	case StatusUnhealthy:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
