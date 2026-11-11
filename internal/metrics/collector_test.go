package metrics

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sbaerlocher/tsmetrics/internal/api"
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
	devices, err := collector.FetchDevices(context.Background())

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
		{"valid long hostname", strings.Repeat("a", 200), false},
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

func TestScrapeHost(t *testing.T) {
	tests := []struct {
		name     string
		devName  string
		devHost  string
		expected string
	}{
		{"FQDN preferred when both set", "device.tail1234.ts.net", shortHost, "device.tail1234.ts.net"},
		{"falls back to Host when Name empty", "", shortHost, shortHost},
		{"returns empty when both empty", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dev := device.Device{
				Name: types.DeviceName(tt.devName),
				Host: tt.devHost,
			}
			got := scrapeHost(dev)
			if got != tt.expected {
				t.Errorf("scrapeHost() = %q, want %q", got, tt.expected)
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
	err := scrapeClient(context.Background(), device, client, cfg)

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
	tracker.CleanupRemovedDevices(map[string]string{"test-device": "test"})

	tracker.CleanupRemovedDevices(map[string]string{"device1": "one", "device2": "two"})
	if _, ok := tracker.knownDevices["test-device"]; ok {
		t.Error("Expected test-device to be removed")
	}
	if tracker.knownDevices["device1"] != "one" {
		t.Error("Expected device1 to be tracked with its name")
	}
}

// clearDiscoveryEnv makes FetchDevices see no static device list and no credentials.
func clearDiscoveryEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"TARGET_DEVICES", "TEST_DEVICES", "TAILNET_NAME",
		"OAUTH_CLIENT_ID", "OAUTH_CLIENT_SECRET", "OAUTH_TOKEN",
	} {
		t.Setenv(key, "")
	}
}

// seedDeviceSeries writes one client, one device-error and one scrape-error series
// for the device and removes them again when the test ends.
func seedDeviceSeries(t *testing.T, deviceID, deviceName string) {
	t.Helper()
	InboundBytes.WithLabelValues(deviceID, deviceName, "direct").Set(1)
	DeviceErrors.WithLabelValues(deviceID, deviceName, "timeout", "true").Inc()
	ScrapeErrors.WithLabelValues(deviceName, "client_fetch_failed").Inc()
	t.Cleanup(func() { CleanupDeviceMetrics(deviceID, deviceName) })
}

// deviceSeries returns the metric families that still hold a series for the device,
// matched by device_id or, for scrape errors, by target=<device name>.
func deviceSeries(t *testing.T, deviceID, deviceName string) map[string]bool {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	found := make(map[string]bool)
	for _, family := range families {
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if (label.GetName() == labelDeviceID && label.GetValue() == deviceID) ||
					(label.GetName() == labelTarget && label.GetValue() == deviceName) {
					found[family.GetName()] = true
				}
			}
		}
	}
	return found
}

var seededFamilies = []string{
	"tailscaled_inbound_bytes_total",
	"tsmetrics_device_errors_total",
	"tsmetrics_scrape_errors_total",
}

func TestUpdateMetricsCleansUpVanishedDevice(t *testing.T) {
	clearDiscoveryEnv(t)

	const deviceID, deviceName = "vanished-device", "vanished-name"
	c := NewCollector(config.Config{ClientMetricsTimeout: time.Second, MaxConcurrentScrapes: 1})
	c.tracker.knownDevices[deviceID] = deviceName
	seedDeviceSeries(t, deviceID, deviceName)

	before := deviceSeries(t, deviceID, deviceName)
	for _, name := range seededFamilies {
		if !before[name] {
			t.Fatalf("expected %s series before cleanup", name)
		}
	}

	if err := c.UpdateMetrics(context.Background(), "test"); err != nil {
		t.Fatalf("update metrics: %v", err)
	}
	for name := range deviceSeries(t, deviceID, deviceName) {
		t.Errorf("series %s still exists for %s after cleanup", name, deviceID)
	}
}

func TestUpdateMetricsKeepsSeriesOnAPIFailure(t *testing.T) {
	clearDiscoveryEnv(t)

	const deviceID, deviceName = "kept-device", "kept-name"
	c := NewCollector(config.Config{ClientMetricsTimeout: time.Second, MaxConcurrentScrapes: 1})
	c.apiClient = api.NewClientWithBaseURL("token", "http://127.0.0.1:1/tailnet/test")
	c.tracker.knownDevices[deviceID] = deviceName
	seedDeviceSeries(t, deviceID, deviceName)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.UpdateMetrics(ctx, "test"); err == nil {
		t.Fatal("expected error from failed API discovery")
	}

	after := deviceSeries(t, deviceID, deviceName)
	for _, name := range seededFamilies {
		if !after[name] {
			t.Errorf("series %s was deleted although discovery failed", name)
		}
	}
}
