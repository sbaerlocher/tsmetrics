package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthEndpoint(t *testing.T) {
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
}

func TestDebugEndpoint(t *testing.T) {
	mux := setupRoutes()
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/debug")
	if err != nil {
		t.Fatalf("debug endpoint request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode debug response: %v", err)
	}

	if _, exists := result["version"]; !exists {
		t.Error("debug response missing version field")
	}
}

func TestMetricsEndpoint(t *testing.T) {
	mux := setupRoutes()
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/metrics")
	if err != nil {
		t.Fatalf("metrics endpoint request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if contentType := resp.Header.Get("Content-Type"); !strings.Contains(contentType, "text/plain") {
		t.Errorf("expected text/plain content type, got %s", contentType)
	}
}

func TestConfigLoading(t *testing.T) {
	// Test default values
	cfg := loadConfig()

	if cfg.Port != "9100" {
		t.Errorf("expected default port 9100, got %s", cfg.Port)
	}

	if cfg.ClientMetricsTimeout != 10*time.Second {
		t.Errorf("expected default timeout 10s, got %v", cfg.ClientMetricsTimeout)
	}

	if cfg.MaxConcurrentScrapes != 10 {
		t.Errorf("expected default concurrent scrapes 10, got %d", cfg.MaxConcurrentScrapes)
	}
}

func TestValidateHostname(t *testing.T) {
	tests := []struct {
		hostname string
		valid    bool
	}{
		{"valid-hostname", true},
		{"192.168.1.1", true},
		{"example.com", true},
		{"test123", true},
		{"", false},
		{"invalid\nhost", false},
		{"invalid\rhost", false},
		{"invalid host", false},
		{"invalid\thost", false},
	}

	for _, test := range tests {
		t.Run(test.hostname, func(t *testing.T) {
			err := validateHostname(test.hostname)
			if test.valid && err != nil {
				t.Errorf("expected %q to be valid, got error: %v", test.hostname, err)
			}
			if !test.valid && err == nil {
				t.Errorf("expected %q to be invalid, got no error", test.hostname)
			}
		})
	}
}

func TestDeviceStructFields(t *testing.T) {
	// Test that Device struct has all required fields for API metrics
	device := Device{
		ID:                "test-id",
		Name:              "test-device",
		Host:              "test-host",
		Tags:              []string{"tag1", "tag2"},
		Online:            true,
		Authorized:        true,
		LastSeen:          time.Now(),
		User:              "user@example.com",
		MachineKey:        "test-key",
		KeyExpiryDisabled: false,
		Expires:           time.Now().Add(24 * time.Hour),
		AdvertisedRoutes:  []string{"10.0.0.0/24"},
		EnabledRoutes:     []string{"10.0.0.0/24"},
		IsExitNode:        false,
		ExitNodeOption:    true,
		OS:                "linux",
		ClientVersion:     "1.42.0",
	}

	// Basic validation that all fields are accessible
	if device.ID == "" {
		t.Error("Device.ID field not accessible")
	}
	if !device.Authorized {
		t.Error("Device.Authorized field not accessible")
	}
	if len(device.AdvertisedRoutes) == 0 {
		t.Error("Device.AdvertisedRoutes field not accessible")
	}
}
