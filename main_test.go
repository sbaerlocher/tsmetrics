package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestAPIClientCreation tests both OAuth and token-based client creation
func TestAPIClientCreation(t *testing.T) {
	// Test OAuth client
	client := NewAPIClient("test-id", "test-secret", "test-tailnet")
	if client == nil {
		t.Error("OAuth client creation failed")
	}

	// Test token client
	tokenClient := NewAPIClientWithToken("test-token", "test-tailnet")
	if tokenClient == nil {
		t.Error("Token client creation failed")
	}

	// Test empty credentials
	emptyClient := NewAPIClient("", "", "test-tailnet")
	if emptyClient == nil {
		t.Error("Client should be created even with empty credentials")
	}
}

// TestTailscaleAPIResponseParsing tests device struct population from API responses
func TestTailscaleAPIResponseParsing(t *testing.T) {
	// Mock API server
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mockResponse := `{
			"devices": [
				{
					"id": "device-123",
					"name": "test-device",
					"hostname": "test-host",
					"addresses": ["100.64.0.1"],
					"online": true,
					"tags": ["exporter"],
					"authorized": true,
					"lastSeen": "2024-01-01T12:00:00Z",
					"user": "user@example.com",
					"keyExpiryDisabled": false,
					"expires": "2024-12-31T23:59:59Z",
					"advertisedRoutes": ["10.0.0.0/24"],
					"enabledRoutes": ["10.0.0.0/24"],
					"isExitNode": false,
					"os": "linux",
					"clientVersion": "1.42.0"
				}
			]
		}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResponse))
	}))
	defer mockAPI.Close()

	// Create client with mock server
	client := NewAPIClientWithToken("test-token", "test")
	// Override baseURL for testing
	client.baseURL = mockAPI.URL

	devices, err := client.fetchDevicesFromAPI()
	if err != nil {
		t.Fatalf("fetchDevicesFromAPI failed: %v", err)
	}

	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}

	device := devices[0]
	if device.ID != "device-123" {
		t.Errorf("expected ID 'device-123', got '%s'", device.ID)
	}
	if device.Name != "test-device" {
		t.Errorf("expected Name 'test-device', got '%s'", device.Name)
	}
	if !device.Online {
		t.Error("expected device to be online")
	}
	if !hasTag(device, "exporter") {
		t.Error("expected device to have 'exporter' tag")
	}
}

// TestClientMetricsParsing tests Prometheus format parsing
func TestClientMetricsParsing(t *testing.T) {
	tests := []struct {
		input    string
		expected map[string]string
	}{
		{
			`key="value",other="test"`,
			map[string]string{"key": "value", "other": "test"},
		},
		{
			`path="direct",type="rx"`,
			map[string]string{"path": "direct", "type": "rx"},
		},
		{
			`reason="no_path",count="5"`,
			map[string]string{"reason": "no_path", "count": "5"},
		},
		{
			``,
			map[string]string{},
		},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			result := parseLabels(test.input)
			if len(result) != len(test.expected) {
				t.Errorf("expected %d labels, got %d", len(test.expected), len(result))
			}
			for k, v := range test.expected {
				if result[k] != v {
					t.Errorf("expected %s=%s, got %s=%s", k, v, k, result[k])
				}
			}
		})
	}
}

// TestMetricsCleanup tests device metrics cleanup functionality
func TestMetricsCleanup(t *testing.T) {
	tracker := NewDeviceMetricsTracker()

	// Mark some devices as active
	tracker.MarkDeviceActive("device-1")
	tracker.MarkDeviceActive("device-2")

	// Wait to make device-1 stale
	time.Sleep(100 * time.Millisecond)
	tracker.MarkDeviceActive("device-2") // Keep device-2 fresh

	// Check cleanup
	staleDevices := tracker.CleanupStaleDevices(50 * time.Millisecond)

	if len(staleDevices) != 1 {
		t.Errorf("expected 1 stale device, got %d", len(staleDevices))
	}

	if len(staleDevices) > 0 && staleDevices[0] != "device-1" {
		t.Errorf("expected device-1 to be stale, got %s", staleDevices[0])
	}
}

// TestEnvironmentVariableParsing tests configuration loading edge cases
func TestEnvironmentVariableParsing(t *testing.T) {
	// Save original env vars
	originalVars := map[string]string{
		"USE_TSNET":              os.Getenv("USE_TSNET"),
		"PORT":                   os.Getenv("PORT"),
		"CLIENT_METRICS_TIMEOUT": os.Getenv("CLIENT_METRICS_TIMEOUT"),
		"MAX_CONCURRENT_SCRAPES": os.Getenv("MAX_CONCURRENT_SCRAPES"),
		"TSNET_TAGS":             os.Getenv("TSNET_TAGS"),
		"REQUIRE_EXPORTER_TAG":   os.Getenv("REQUIRE_EXPORTER_TAG"),
	}

	// Cleanup function
	cleanup := func() {
		for k, v := range originalVars {
			if v == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}
		}
	}
	defer cleanup()

	tests := []struct {
		name     string
		envVars  map[string]string
		validate func(Config) error
	}{
		{
			name: "tsnet enabled",
			envVars: map[string]string{
				"USE_TSNET":            "true",
				"TSNET_TAGS":           "exporter,dev",
				"REQUIRE_EXPORTER_TAG": "true",
			},
			validate: func(cfg Config) error {
				if !cfg.UseTsnet {
					return fmt.Errorf("expected UseTsnet=true")
				}
				if len(cfg.TsnetTags) != 2 {
					return fmt.Errorf("expected 2 tags, got %d", len(cfg.TsnetTags))
				}
				if !cfg.RequireExporterTag {
					return fmt.Errorf("expected RequireExporterTag=true")
				}
				return nil
			},
		},
		{
			name: "custom timeouts",
			envVars: map[string]string{
				"CLIENT_METRICS_TIMEOUT": "30s",
				"MAX_CONCURRENT_SCRAPES": "5",
				"PORT":                   "8080",
			},
			validate: func(cfg Config) error {
				if cfg.ClientMetricsTimeout != 30*time.Second {
					return fmt.Errorf("expected 30s timeout, got %v", cfg.ClientMetricsTimeout)
				}
				if cfg.MaxConcurrentScrapes != 5 {
					return fmt.Errorf("expected 5 concurrent scrapes, got %d", cfg.MaxConcurrentScrapes)
				}
				if cfg.Port != "8080" {
					return fmt.Errorf("expected port 8080, got %s", cfg.Port)
				}
				return nil
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Set test environment variables
			for k, v := range test.envVars {
				os.Setenv(k, v)
			}

			cfg := loadConfig()

			if err := test.validate(cfg); err != nil {
				t.Error(err)
			}

			// Clean up for next test
			for k := range test.envVars {
				os.Unsetenv(k)
			}
		})
	}
}

// TestConcurrentScraping tests the concurrent scraping functionality
func TestConcurrentScraping(t *testing.T) {
	// Mock device metrics server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metrics := `# HELP tailscaled_inbound_bytes_total Inbound bytes
# TYPE tailscaled_inbound_bytes_total counter
tailscaled_inbound_bytes_total{path="direct"} 1024
tailscaled_outbound_bytes_total{path="direct"} 2048
tailscaled_health_messages{type="info"} 1
`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(metrics))
	}))
	defer mockServer.Close()

	// Extract host and port from mock server URL
	serverURL := strings.TrimPrefix(mockServer.URL, "http://")
	host, port, _ := strings.Cut(serverURL, ":")

	devices := []Device{
		{
			ID:     "test-device-1",
			Name:   "test-device-1",
			Host:   host,
			Tags:   []string{"exporter"},
			Online: true,
		},
	}

	cfg := Config{
		ClientMetricsTimeout: 5 * time.Second,
		MaxConcurrentScrapes: 2,
	}

	// Override port for testing (this is a bit hacky but works for the test)
	originalDevice := devices[0]
	devices[0].Host = fmt.Sprintf("%s:%s", host, port)

	err := scrapeClientMetrics(devices, cfg)
	if err != nil {
		t.Errorf("scrapeClientMetrics failed: %v", err)
	}

	// Restore original device
	devices[0] = originalDevice
}

// TestHasTag tests the tag checking functionality
func TestHasTag(t *testing.T) {
	device := Device{
		Tags: []string{"exporter", "production", "gateway"},
	}

	tests := []struct {
		tag      string
		expected bool
	}{
		{"exporter", true},
		{"production", true},
		{"gateway", true},
		{"nonexistent", false},
		{"", false},
	}

	for _, test := range tests {
		t.Run(test.tag, func(t *testing.T) {
			result := hasTag(device, test.tag)
			if result != test.expected {
				t.Errorf("hasTag(%q) = %v, expected %v", test.tag, result, test.expected)
			}
		})
	}
}
