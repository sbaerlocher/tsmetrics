package health

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sbaerlocher/tsmetrics/internal/api"
	"github.com/sbaerlocher/tsmetrics/internal/cache"
	"github.com/sbaerlocher/tsmetrics/internal/metrics"
	"github.com/sbaerlocher/tsmetrics/pkg/device"
)

func TestHealthChecker_NewHealthChecker(t *testing.T) {
	hc := NewHealthChecker()

	if hc == nil {
		t.Fatal("NewHealthChecker returned nil")
	}

	if hc.components == nil {
		t.Error("components map should be initialized")
	}

	if hc.lastChecks == nil {
		t.Error("lastChecks map should be initialized")
	}

	if hc.startupTime.IsZero() {
		t.Error("startupTime should be set")
	}
}

func TestHealthChecker_LivenessCheck(t *testing.T) {
	hc := NewHealthChecker()

	tests := []struct {
		name        string
		ctx         context.Context
		expectError bool
	}{
		{
			name:        "normal context",
			ctx:         context.Background(),
			expectError: false,
		},
		{
			name:        "cancelled context",
			ctx:         func() context.Context { ctx, cancel := context.WithCancel(context.Background()); cancel(); return ctx }(),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := hc.LivenessCheck(tt.ctx)
			if (err != nil) != tt.expectError {
				t.Errorf("LivenessCheck() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestHealthChecker_StartupCheck(t *testing.T) {
	tests := []struct {
		name        string
		startupTime time.Time
		expectReady bool
	}{
		{
			name:        "recent startup",
			startupTime: time.Now().Add(-10 * time.Second),
			expectReady: true, // During grace period, should pass
		},
		{
			name:        "old startup",
			startupTime: time.Now().Add(-60 * time.Second),
			expectReady: false, // After grace period, requires readiness
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hc := NewHealthChecker()
			hc.startupTime = tt.startupTime

			// Add a failing component for the readiness check
			hc.RegisterComponent(&mockComponentChecker{
				name:      "test",
				shouldErr: true,
			})

			err := hc.StartupCheck(context.Background())
			if tt.expectReady && err != nil {
				t.Errorf("StartupCheck() expected to pass but got error: %v", err)
			}
			if !tt.expectReady && err == nil {
				t.Error("StartupCheck() expected to fail but passed")
			}
		})
	}
}

func TestHealthChecker_ReadinessCheck(t *testing.T) {
	tests := []struct {
		name        string
		components  []ComponentChecker
		expectError bool
	}{
		{
			name:        "no components",
			components:  nil,
			expectError: false,
		},
		{
			name: "all healthy components",
			components: []ComponentChecker{
				&mockComponentChecker{name: "comp1", shouldErr: false},
				&mockComponentChecker{name: "comp2", shouldErr: false},
			},
			expectError: false,
		},
		{
			name: "one unhealthy component",
			components: []ComponentChecker{
				&mockComponentChecker{name: "comp1", shouldErr: false},
				&mockComponentChecker{name: "comp2", shouldErr: true},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hc := NewHealthChecker()

			for _, comp := range tt.components {
				hc.RegisterComponent(comp)
			}

			err := hc.ReadinessCheck(context.Background())
			if (err != nil) != tt.expectError {
				t.Errorf("ReadinessCheck() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestHealthChecker_GetHealthStatus(t *testing.T) {
	hc := NewHealthChecker()

	// Add mock components
	hc.RegisterComponent(&mockComponentChecker{name: "healthy", shouldErr: false})
	hc.RegisterComponent(&mockComponentChecker{name: "unhealthy", shouldErr: true})
	hc.RegisterComponent(&mockComponentChecker{name: "slow", shouldErr: false, delay: 6 * time.Second})

	status := hc.GetHealthStatus(context.Background())

	if status.Overall != StatusUnhealthy {
		t.Errorf("Expected overall status to be unhealthy, got %s", status.Overall)
	}

	if len(status.Checks) != 3 {
		t.Errorf("Expected 3 checks, got %d", len(status.Checks))
	}

	if status.Checks["healthy"].Status != StatusHealthy {
		t.Error("healthy component should be healthy")
	}

	if status.Checks["unhealthy"].Status != StatusUnhealthy {
		t.Error("unhealthy component should be unhealthy")
	}

	if status.Checks["slow"].Status != StatusDegraded {
		t.Error("slow component should be degraded")
	}
}

func TestAPIHealthChecker(t *testing.T) {
	// Create mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/api/v2/device") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"devices": []}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := api.NewClientWithToken("test-token", "test-tailnet")

	checker := NewAPIHealthChecker(client)

	if checker.ComponentName() != "tailscale_api" {
		t.Errorf("Expected component name 'tailscale_api', got %s", checker.ComponentName())
	}

	// Note: This will fail because we don't have a real API, but it tests the flow
	err := checker.CheckHealth(context.Background())
	if err == nil {
		t.Error("Expected error with mock server, but got none")
	}

	// Test with nil client
	nilChecker := NewAPIHealthChecker(nil)
	err = nilChecker.CheckHealth(context.Background())
	if err == nil {
		t.Error("Expected error with nil client")
	}
}

func TestCacheHealthChecker(t *testing.T) {
	cache := cache.NewDeviceCache(5*time.Minute, 10*time.Minute, func() ([]device.Device, error) {
		return []device.Device{}, nil
	})

	checker := NewCacheHealthChecker(cache)

	if checker.ComponentName() != "device_cache" {
		t.Errorf("Expected component name 'device_cache', got %s", checker.ComponentName())
	}

	err := checker.CheckHealth(context.Background())
	if err != nil {
		t.Errorf("Cache health check failed: %v", err)
	}

	// Test with nil cache
	nilChecker := NewCacheHealthChecker(nil)
	err = nilChecker.CheckHealth(context.Background())
	if err == nil {
		t.Error("Expected error with nil cache")
	}
}

func TestPerformanceHealthChecker(t *testing.T) {
	monitor := metrics.NewPerformanceMonitor(5 * time.Second)
	monitor.Start()
	defer monitor.Stop()

	checker := NewPerformanceHealthChecker(monitor)

	if checker.ComponentName() != "performance_monitor" {
		t.Errorf("Expected component name 'performance_monitor', got %s", checker.ComponentName())
	}

	err := checker.CheckHealth(context.Background())
	if err != nil {
		t.Errorf("Performance health check failed: %v", err)
	}

	// Test with nil monitor
	nilChecker := NewPerformanceHealthChecker(nil)
	err = nilChecker.CheckHealth(context.Background())
	if err == nil {
		t.Error("Expected error with nil monitor")
	}
}

func TestWriteHealthResponse(t *testing.T) {
	status := HealthStatus{
		Overall: StatusHealthy,
		Checks: map[string]CheckResult{
			"test": {
				Component: "test",
				Status:    StatusHealthy,
				Message:   "OK",
				Duration:  100 * time.Millisecond,
				Timestamp: time.Now(),
			},
		},
	}

	w := httptest.NewRecorder()
	WriteHealthResponse(w, status, http.StatusOK)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got %s", contentType)
	}

	body := w.Body.String()
	if !strings.Contains(body, `"status": "healthy"`) {
		t.Error("Response should contain status")
	}

	if !strings.Contains(body, `"test": {`) {
		t.Error("Response should contain check results")
	}
}

func TestDetermineHTTPStatus(t *testing.T) {
	tests := []struct {
		status   Status
		expected int
	}{
		{StatusHealthy, http.StatusOK},
		{StatusDegraded, http.StatusOK},
		{StatusUnhealthy, http.StatusServiceUnavailable},
		{Status("unknown"), http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			result := DetermineHTTPStatus(tt.status)
			if result != tt.expected {
				t.Errorf("DetermineHTTPStatus(%s) = %d, expected %d", tt.status, result, tt.expected)
			}
		})
	}
}

func TestHealthChecker_RegisterComponent(t *testing.T) {
	hc := NewHealthChecker()
	comp := &mockComponentChecker{name: "test", shouldErr: false}

	hc.RegisterComponent(comp)

	hc.mu.RLock()
	if _, exists := hc.components["test"]; !exists {
		t.Error("Component should be registered")
	}
	hc.mu.RUnlock()
}

func TestHealthChecker_ConcurrentAccess(t *testing.T) {
	hc := NewHealthChecker()

	// Register components concurrently
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			comp := &mockComponentChecker{
				name:      fmt.Sprintf("comp-%d", id),
				shouldErr: false,
			}
			hc.RegisterComponent(comp)
			done <- true
		}(i)
	}

	// Wait for all registrations
	for i := 0; i < 10; i++ {
		<-done
	}

	// Check health status concurrently
	for i := 0; i < 10; i++ {
		go func() {
			status := hc.GetHealthStatus(context.Background())
			if len(status.Checks) != 10 {
				t.Errorf("Expected 10 checks, got %d", len(status.Checks))
			}
			done <- true
		}()
	}

	// Wait for all checks
	for i := 0; i < 10; i++ {
		<-done
	}
}

// Mock component checker for testing
type mockComponentChecker struct {
	name      string
	shouldErr bool
	delay     time.Duration
}

func (m *mockComponentChecker) ComponentName() string {
	return m.name
}

func (m *mockComponentChecker) CheckHealth(ctx context.Context) error {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}

	if m.shouldErr {
		return fmt.Errorf("mock error for %s", m.name)
	}

	return nil
}
