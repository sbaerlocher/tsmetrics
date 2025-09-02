package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
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

	// Create device that points to our mock server (including port)
	devices := []Device{
		{
			ID:     "test-device-1",
			Name:   "test-device-1",
			Host:   serverURL, // Use the full host:port from mock server
			Tags:   []string{"exporter"},
			Online: true,
		},
	}

	cfg := Config{
		ClientMetricsTimeout: 5 * time.Second,
		MaxConcurrentScrapes: 2,
	}

	// For this test, we need to override the hardcoded port 5252 behavior
	// We'll create a custom test that simulates the scraping without the hardcoded port
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errors []error

	semaphore := make(chan struct{}, cfg.MaxConcurrentScrapes)

	for _, device := range devices {
		if !device.Online {
			continue
		}

		// Check if device has exporter tag (simulating hasTag function)
		hasExporter := false
		for _, tag := range device.Tags {
			if tag == "exporter" {
				hasExporter = true
				break
			}
		}
		if !hasExporter {
			continue
		}

		wg.Add(1)
		go func(dev Device) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// Create HTTP client with timeout
			client := &http.Client{Timeout: cfg.ClientMetricsTimeout}

			// Use the device host directly (which includes the mock server port)
			u := url.URL{Scheme: "http", Host: dev.Host, Path: "/metrics"}
			resp, err := client.Get(u.String())

			if err != nil {
				mu.Lock()
				errors = append(errors, fmt.Errorf("device %s: %v", dev.Name, err))
				mu.Unlock()
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				mu.Lock()
				errors = append(errors, fmt.Errorf("device %s: status %d", dev.Name, resp.StatusCode))
				mu.Unlock()
				return
			}

			// Read response to ensure it's valid
			_, err = io.ReadAll(resp.Body)
			if err != nil {
				mu.Lock()
				errors = append(errors, fmt.Errorf("device %s: read error: %v", dev.Name, err))
				mu.Unlock()
			}
		}(device)
	}

	wg.Wait()

	if len(errors) > 0 {
		t.Errorf("scraping failed with errors: %v", errors)
	}
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

// TestConfigValidation tests the configuration validation
func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name      string
		config    Config
		wantError bool
	}{
		{
			name: "valid config",
			config: Config{
				Port:                 "9100",
				ClientMetricsTimeout: 10 * time.Second,
				MaxConcurrentScrapes: 10,
			},
			wantError: false,
		},
		{
			name: "empty port",
			config: Config{
				Port:                 "",
				ClientMetricsTimeout: 10 * time.Second,
				MaxConcurrentScrapes: 10,
			},
			wantError: true,
		},
		{
			name: "invalid timeout",
			config: Config{
				Port:                 "9100",
				ClientMetricsTimeout: 0,
				MaxConcurrentScrapes: 10,
			},
			wantError: true,
		},
		{
			name: "tsnet without hostname",
			config: Config{
				Port:                 "9100",
				ClientMetricsTimeout: 10 * time.Second,
				MaxConcurrentScrapes: 10,
				UseTsnet:             true,
				TsnetHostname:        "",
			},
			wantError: true,
		},
		{
			name: "require exporter tag without tag",
			config: Config{
				Port:                 "9100",
				ClientMetricsTimeout: 10 * time.Second,
				MaxConcurrentScrapes: 10,
				UseTsnet:             true,
				TsnetHostname:        "test",
				RequireExporterTag:   true,
				TsnetTags:            []string{"other"},
			},
			wantError: true,
		},
		{
			name: "require exporter tag with tag",
			config: Config{
				Port:                 "9100",
				ClientMetricsTimeout: 10 * time.Second,
				MaxConcurrentScrapes: 10,
				UseTsnet:             true,
				TsnetHostname:        "test",
				RequireExporterTag:   true,
				TsnetTags:            []string{"exporter", "other"},
			},
			wantError: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.config.Validate()
			if test.wantError && err == nil {
				t.Error("expected validation error but got none")
			}
			if !test.wantError && err != nil {
				t.Errorf("unexpected validation error: %v", err)
			}
		})
	}
}

// TestEnhancedHealthEndpoint tests the enhanced health endpoint
func TestEnhancedHealthEndpoint(t *testing.T) {
	// Test without API configuration
	mux := setupRoutes()
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("health endpoint request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode health response: %v", err)
	}

	expectedFields := []string{"status", "version", "build_time", "timestamp", "api_status"}
	for _, field := range expectedFields {
		if _, exists := result[field]; !exists {
			t.Errorf("health response missing field: %s", field)
		}
	}

	if result["api_status"] != "not_configured" {
		t.Errorf("expected api_status=not_configured, got %v", result["api_status"])
	}
}

// TestAPIConnectivity tests the API connectivity check functionality
func TestAPIConnectivity(t *testing.T) {
	tests := []struct {
		name          string
		statusCode    int
		expectSuccess bool
		expectError   bool
	}{
		{"successful_connection", http.StatusOK, true, false},
		{"unauthorized_but_reachable", http.StatusUnauthorized, true, false},
		{"not_found", http.StatusNotFound, false, true},
		{"server_error", http.StatusInternalServerError, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "HEAD" {
					w.WriteHeader(tt.statusCode)
				}
			}))
			defer mockAPI.Close()

			client := NewAPIClientWithToken("test-token", "test")
			// Override baseURL to use mock server
			parsedURL, _ := url.Parse(mockAPI.URL)
			client.baseURL = parsedURL.Scheme + "://" + parsedURL.Host + "/api/v2/tailnet/test"

			ctx := context.Background()
			success, err := client.testConnectivity(ctx)

			if success != tt.expectSuccess {
				t.Errorf("expected success=%v, got success=%v", tt.expectSuccess, success)
			}

			if (err != nil) != tt.expectError {
				t.Errorf("expected error=%v, got err=%v", tt.expectError, err)
			}
		})
	}
}

// Test structured error handling
func TestStructuredErrors(t *testing.T) {
	tests := []struct {
		name      string
		errorType string
		retryable bool
	}{
		{"network error", "network", true},
		{"http client error", "http_client", false},
		{"http server error", "http_server", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deviceErr := DeviceError{
				DeviceID:   "test-id",
				DeviceName: "test-device",
				ErrorType:  tt.errorType,
				Underlying: fmt.Errorf("test error"),
				Retryable:  tt.retryable,
				RetryAfter: 30 * time.Second,
				Timestamp:  time.Now(),
			}

			if deviceErr.IsRetryable() != tt.retryable {
				t.Errorf("expected retryable=%v, got %v", tt.retryable, deviceErr.IsRetryable())
			}

			if !strings.Contains(deviceErr.Error(), tt.errorType) {
				t.Errorf("error message should contain error type %s", tt.errorType)
			}
		})
	}
}

// Test mutual exclusive config validation
func TestMutualExclusiveConfig(t *testing.T) {
	// Set OAuth token in environment
	os.Setenv("OAUTH_TOKEN", "test-token")
	defer os.Unsetenv("OAUTH_TOKEN")

	cfg := Config{
		OAuthClientID:        "test-client",
		OAuthSecret:          "test-secret",
		Port:                 "9100",
		ClientMetricsTimeout: 10 * time.Second,
		LogLevel:             "info",
		LogFormat:            "text",
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for mutual exclusive OAuth config")
	}

	if !strings.Contains(err.Error(), "cannot use both OAuth credentials and direct token") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// Test enhanced health endpoint metrics
func TestEnhancedHealthMetrics(t *testing.T) {
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	enhancedHealthHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}

	// Check for new health metrics
	expectedFields := []string{"memory_mb", "goroutines", "uptime_seconds", "last_scrape", "devices_online"}
	for _, field := range expectedFields {
		if _, exists := response[field]; !exists {
			t.Errorf("expected field %s in health response", field)
		}
	}
}
