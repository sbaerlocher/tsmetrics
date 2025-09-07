package metrics

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/sbaerlocher/tsmetrics/internal/config"
	"github.com/sbaerlocher/tsmetrics/internal/types"
	"github.com/sbaerlocher/tsmetrics/pkg/device"
)

func TestNewCollector(t *testing.T) {
	cfg := config.Config{
		Port:                 "9100",
		ClientMetricsTimeout: 10 * time.Second,
		MaxConcurrentScrapes: 10,
	}

	collector := NewCollector(cfg)

	if collector == nil {
		t.Fatal("Expected collector to be created")
	}

	if collector.cfg.Port != "9100" {
		t.Errorf("Expected port 9100, got %s", collector.cfg.Port)
	}

	if collector.tracker == nil {
		t.Error("Expected tracker to be initialized")
	}
}

func TestFetchDevicesWithoutAPI(t *testing.T) {
	cfg := config.Config{
		Port:                 "9100",
		ClientMetricsTimeout: 10 * time.Second,
		MaxConcurrentScrapes: 10,
	}

	collector := NewCollector(cfg)
	devices, err := collector.FetchDevices()

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if len(devices) != 0 {
		t.Errorf("Expected 0 devices without API config, got %d", len(devices))
	}
}

func TestHasTag(t *testing.T) {
	exporterTag, _ := types.NewTagName("exporter")
	monitoringTag, _ := types.NewTagName("monitoring")

	deviceID, _ := types.NewDeviceID("device1")
	deviceName, _ := types.NewDeviceName("test-device")

	device := device.Device{
		ID:   deviceID,
		Name: deviceName,
		Tags: []types.TagName{exporterTag, monitoringTag},
	}

	if !hasTag(device, "exporter") {
		t.Error("Expected device to have 'exporter' tag")
	}

	if !hasTag(device, "monitoring") {
		t.Error("Expected device to have 'monitoring' tag")
	}

	if hasTag(device, "nonexistent") {
		t.Error("Expected device to not have 'nonexistent' tag")
	}
}

func TestValidateHostname(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		wantErr  bool
	}{
		{"valid domain", "example.com", false},
		{"valid IP", "8.8.8.8", false},
		{"empty hostname", "", true},
		{"newline character", "example\n.com", true},
		{"carriage return", "example\r.com", true},
		{"valid long hostname", string(make([]byte, 200)), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateHostname(tt.hostname)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateHostname() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseLabels(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{
			name:     "single label without braces",
			input:    `path="/interface"`,
			expected: map[string]string{"path": "/interface"},
		},
		{
			name:     "multiple labels without braces",
			input:    `path="/interface",reason="timeout"`,
			expected: map[string]string{"path": "/interface", "reason": "timeout"},
		},
		{
			name:     "empty input",
			input:    "",
			expected: map[string]string{},
		},
		{
			name:     "single label with quotes",
			input:    `type="warning"`,
			expected: map[string]string{"type": "warning"},
		},
		{
			name:     "label without quotes",
			input:    `method=GET`,
			expected: map[string]string{"method": "GET"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseLabels(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d labels, got %d", len(tt.expected), len(result))
			}

			for key, expectedValue := range tt.expected {
				if actualValue, exists := result[key]; !exists || actualValue != expectedValue {
					t.Errorf("Expected label %s=%s, got %s=%s", key, expectedValue, key, actualValue)
				}
			}
		})
	}
}

func TestScrapeClient(t *testing.T) {
	// Create test device
	deviceID, _ := types.NewDeviceID("test-device")
	deviceName, _ := types.NewDeviceName("test-device")

	device := device.Device{
		ID:   deviceID,
		Name: deviceName,
		Host: "127.0.0.1", // DevSkim: ignore DS162092 - Test host for unit tests
	}

	cfg := config.Config{
		ClientMetricsPort:    "9090",
		ClientMetricsTimeout: 5 * time.Second,
	}

	// Test with unreachable host
	client := &http.Client{Timeout: 100 * time.Millisecond}
	err := scrapeClient(device, client, cfg)

	if err == nil {
		t.Error("Expected error when connecting to unreachable host")
	}
}

func TestBytesCount(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		char     byte
		expected int
	}{
		{"no matches", []byte("hello world"), 'x', 0},
		{"single match", []byte("hello world"), 'o', 2},
		{"multiple matches", []byte("hello,world,test"), ',', 2},
		{"empty input", []byte(""), 'a', 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := bytesCount(tt.input, tt.char)
			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestIsTsnetStartupError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"connection refused", fmt.Errorf("connection refused"), true},
		{"no such host", fmt.Errorf("no such host"), true},
		{"tsnet backend NoState", fmt.Errorf("backend in state NoState"), true},
		{"tsnet no network", fmt.Errorf("tsnet: no Tailscale network"), true},
		{"tsnet not ready", fmt.Errorf("tsnet: not ready"), true},
		{"timeout error", fmt.Errorf("timeout"), false},
		{"other error", fmt.Errorf("some other error"), false},
		{"nil error", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTsnetStartupError(tt.err)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestDeviceMetricsTracker(t *testing.T) {
	tracker := NewDeviceMetricsTracker()

	if tracker == nil {
		t.Fatal("Expected tracker to be created")
	}

	// Test MarkDeviceActive
	deviceID := "test-device"
	tracker.MarkDeviceActive(deviceID)

	// Test CleanupStaleDevices with no stale devices
	staleDevices := tracker.CleanupStaleDevices(time.Hour)
	if len(staleDevices) != 0 {
		t.Errorf("Expected no stale devices, got %d", len(staleDevices))
	}

	// Test CleanupStaleDevices with stale devices
	time.Sleep(10 * time.Millisecond)
	staleDevices = tracker.CleanupStaleDevices(5 * time.Millisecond)
	if len(staleDevices) != 1 {
		t.Errorf("Expected 1 stale device, got %d", len(staleDevices))
	}

	// Test CleanupRemovedDevices
	seenDevices := map[string]struct{}{
		"device1": {},
		"device2": {},
	}
	tracker.CleanupRemovedDevices(seenDevices)
}
