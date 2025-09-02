package metrics

import (
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
		{"too long", string(make([]byte, 254)), true},
		{"invalid characters", "example..com", true},
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
			name:     "single label",
			input:    `{path="/interface"}`,
			expected: map[string]string{"path": "/interface"},
		},
		{
			name:     "multiple labels",
			input:    `{path="/interface",reason="timeout"}`,
			expected: map[string]string{"path": "/interface", "reason": "timeout"},
		},
		{
			name:     "empty labels",
			input:    `{}`,
			expected: map[string]string{},
		},
		{
			name:     "no braces",
			input:    `path="/interface"`,
			expected: map[string]string{},
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
