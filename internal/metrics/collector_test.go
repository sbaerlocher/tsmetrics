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
	t.Cleanup(func() {
		CleanupDeviceMetrics(deviceID)
		ScrapeErrors.DeleteLabelValues(deviceName, "client_fetch_failed")
	})
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

const (
	labelVersion     = "version"
	testVersionOld   = "1.0.0"
	testVersionNew   = "1.2.0"
	testRouteKept    = "10.0.0.0/24"
	testRouteDropped = "10.1.0.0/24"
	testDERPRegion   = "zrh"
)

// hasSeries reports whether the default registry exposes a series of metric
// whose labels include every pair in labels.
func hasSeries(t *testing.T, metric string, labels map[string]string) bool {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range families {
		if mf.GetName() != metric {
			continue
		}
		for _, m := range mf.GetMetric() {
			got := map[string]string{}
			for _, lp := range m.GetLabel() {
				got[lp.GetName()] = lp.GetValue()
			}
			match := true
			for k, v := range labels {
				if got[k] != v {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	return false
}

// testDevice builds a device and removes all of its series when the test ends,
// so tests sharing the global registry stay independent.
func testDevice(t *testing.T, id, name string) device.Device {
	t.Helper()
	deviceID, err := types.NewDeviceID(id)
	if err != nil {
		t.Fatal(err)
	}
	deviceName, err := types.NewDeviceName(name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		CleanupDeviceMetrics(id)
		ScrapeErrors.DeleteLabelValues(name, "client_fetch_failed")
	})
	return device.Device{ID: deviceID, Name: deviceName}
}

func TestUpdateAPIMetricsSetsSeries(t *testing.T) {
	c := NewCollector(config.Config{})
	d := testDevice(t, "label-dev-1", "label-dev-1")
	d.OS = "linux"
	d.ClientVersion = testVersionOld

	seen, _ := c.updateAPIMetrics([]device.Device{d})

	if _, ok := seen["label-dev-1"]; !ok {
		t.Error("expected device in seen set")
	}
	if !hasSeries(t, "tailscale_device_info", map[string]string{labelDeviceID: "label-dev-1", "os": "linux", labelVersion: testVersionOld}) {
		t.Error("expected tailscale_device_info series")
	}
}

func TestUpdateAPIMetricsVersionChange(t *testing.T) {
	c := NewCollector(config.Config{})
	d := testDevice(t, "label-dev-version", "label-dev-version")
	d.ClientVersion = testVersionOld
	c.updateAPIMetrics([]device.Device{d})

	d.ClientVersion = testVersionNew
	c.updateAPIMetrics([]device.Device{d})

	if hasSeries(t, "tailscale_device_info", map[string]string{labelDeviceID: "label-dev-version", labelVersion: testVersionOld}) {
		t.Error("old version series still present")
	}
	if !hasSeries(t, "tailscale_device_info", map[string]string{labelDeviceID: "label-dev-version", labelVersion: testVersionNew}) {
		t.Error("new version series missing")
	}
}

func TestUpdateAPIMetricsRouteRemoved(t *testing.T) {
	c := NewCollector(config.Config{})
	d := testDevice(t, "label-dev-route", "label-dev-route")
	d.AdvertisedRoutes = []string{testRouteKept, testRouteDropped}
	c.updateAPIMetrics([]device.Device{d})

	d.AdvertisedRoutes = []string{testRouteKept}
	c.updateAPIMetrics([]device.Device{d})

	if hasSeries(t, "tailscale_device_routes_advertised", map[string]string{labelDeviceID: "label-dev-route", "route": testRouteDropped}) {
		t.Error("removed route series still present")
	}
	if !hasSeries(t, "tailscale_device_routes_advertised", map[string]string{labelDeviceID: "label-dev-route", "route": testRouteKept}) {
		t.Error("remaining route series missing")
	}
}

func TestUpdateAPIMetricsUserCleared(t *testing.T) {
	c := NewCollector(config.Config{})
	d := testDevice(t, "label-dev-user", "label-dev-user")
	d.User = "a@example.com"
	c.updateAPIMetrics([]device.Device{d})

	d.User = ""
	c.updateAPIMetrics([]device.Device{d})

	if hasSeries(t, "tailscale_device_user", map[string]string{labelDeviceID: "label-dev-user"}) {
		t.Error("user series still present after user was cleared")
	}
}

func TestUpdateAPIMetricsConnectivityDropped(t *testing.T) {
	c := NewCollector(config.Config{})
	d := testDevice(t, "label-dev-conn", "label-dev-conn")
	d.ClientConnectivity = &device.ClientConnectivity{
		Latency: map[string]device.LatencyInfo{testDERPRegion: {LatencyMs: 5, Preferred: true}},
	}
	c.updateAPIMetrics([]device.Device{d})

	d.ClientConnectivity = nil
	c.updateAPIMetrics([]device.Device{d})

	labels := map[string]string{labelDeviceID: "label-dev-conn"}
	if hasSeries(t, "tailscale_device_latency_ms", labels) {
		t.Error("latency series still present after connectivity dropped")
	}
	if hasSeries(t, "tailscale_device_client_supports", labels) {
		t.Error("client supports series still present after connectivity dropped")
	}
}

func TestUpdateAPIMetricsRename(t *testing.T) {
	const renameID, renameOld, renameNew = "label-dev-rename", "label-old", "label-new"
	c := NewCollector(config.Config{})
	d := testDevice(t, renameID, renameOld)
	c.updateAPIMetrics([]device.Device{d})
	InboundBytes.WithLabelValues(renameID, renameOld, "direct").Set(1)
	DeviceErrors.WithLabelValues(renameID, renameOld, "timeout", "true").Inc()

	newName, _ := types.NewDeviceName(renameNew)
	d.Name = newName
	_, renamed := c.updateAPIMetrics([]device.Device{d})
	if !hasSeries(t, "tailscaled_inbound_bytes_total", map[string]string{labelDeviceID: renameID, labelDeviceName: renameOld}) {
		t.Error("old-name client series dropped before the client scrape wrote new ones")
	}
	InboundBytes.WithLabelValues(renameID, renameNew, "direct").Set(2)
	dropRenamedDeviceSeries(renamed, map[string]string{renameID: renameNew})

	old := map[string]string{labelDeviceID: renameID, labelDeviceName: renameOld}
	for _, metric := range []string{"tailscale_device_info", "tailscaled_inbound_bytes_total", "tsmetrics_device_errors_total"} {
		if hasSeries(t, metric, old) {
			t.Errorf("%s still has series with old device name", metric)
		}
	}
	newLabels := map[string]string{labelDeviceID: renameID, labelDeviceName: renameNew}
	if !hasSeries(t, "tailscale_device_info", newLabels) {
		t.Error("tailscale_device_info missing for new device name")
	}
	if !hasSeries(t, "tailscaled_inbound_bytes_total", newLabels) {
		t.Error("new-name client series removed")
	}
}

func TestUpdateAPIMetricsUnchangedKeepsSeries(t *testing.T) {
	c := NewCollector(config.Config{})
	d := testDevice(t, "label-dev-same", "label-dev-same")
	d.ClientVersion = testVersionOld
	c.updateAPIMetrics([]device.Device{d})
	InboundBytes.WithLabelValues("label-dev-same", "label-dev-same", "direct").Set(1)
	DeviceErrors.WithLabelValues("label-dev-same", "label-dev-same", "timeout", "true").Inc()

	c.updateAPIMetrics([]device.Device{d})

	labels := map[string]string{labelDeviceID: "label-dev-same"}
	if !hasSeries(t, "tailscaled_inbound_bytes_total", labels) {
		t.Error("client series removed for unchanged device")
	}
	if !hasSeries(t, "tsmetrics_device_errors_total", labels) {
		t.Error("device error series removed for unchanged device")
	}
}

func TestDeviceLabelFingerprintIgnoresOrderAndValues(t *testing.T) {
	id, _ := types.NewDeviceID("fp-dev")
	name, _ := types.NewDeviceName("fp-dev")
	base := func() device.Device {
		return device.Device{
			ID:               id,
			Name:             name,
			ClientVersion:    testVersionOld,
			AdvertisedRoutes: []string{testRouteKept, testRouteDropped},
			LastSeen:         time.Unix(1000, 0),
			ClientConnectivity: &device.ClientConnectivity{
				Latency: map[string]device.LatencyInfo{testDERPRegion: {LatencyMs: 5, Preferred: true}},
			},
		}
	}

	a := base()
	b := base()
	b.AdvertisedRoutes = []string{testRouteDropped, testRouteKept}
	b.LastSeen = time.Unix(2000, 0)
	b.ClientConnectivity = &device.ClientConnectivity{
		Latency: map[string]device.LatencyInfo{testDERPRegion: {LatencyMs: 50, Preferred: true}},
	}
	if deviceLabelFingerprint(a) != deviceLabelFingerprint(b) {
		t.Error("fingerprint changed for route order, latency value or last seen time")
	}

	c := base()
	c.ClientVersion = testVersionNew
	if deviceLabelFingerprint(a) == deviceLabelFingerprint(c) {
		t.Error("fingerprint unchanged for version change")
	}
}

func TestDeleteDeviceScrapeErrors(t *testing.T) {
	const fetchFailed = "client_fetch_failed"
	scrapeErr := func(target, errorType string) map[string]string {
		return map[string]string{labelTarget: target, labelErrorType: errorType}
	}
	seed := func(t *testing.T, target, errorType string) {
		t.Helper()
		ScrapeErrors.WithLabelValues(target, errorType).Inc()
		t.Cleanup(func() { ScrapeErrors.DeleteLabelValues(target, errorType) })
	}

	t.Run("removed", func(t *testing.T) {
		tracker := NewDeviceMetricsTracker()
		tracker.knownDevices["sde-a"] = "sde-name-a"
		seed(t, "sde-name-a", fetchFailed)
		tracker.CleanupRemovedDevices(map[string]string{})
		if hasSeries(t, "tsmetrics_scrape_errors_total", scrapeErr("sde-name-a", fetchFailed)) {
			t.Error("scrape error series of removed device still present")
		}
	})

	t.Run("reused name", func(t *testing.T) {
		tracker := NewDeviceMetricsTracker()
		tracker.knownDevices["sde-old"] = "sde-dup"
		seed(t, "sde-dup", fetchFailed)
		tracker.CleanupRemovedDevices(map[string]string{"sde-new": "sde-dup"})
		if !hasSeries(t, "tsmetrics_scrape_errors_total", scrapeErr("sde-dup", fetchFailed)) {
			t.Error("scrape error series deleted although a new device carries the name")
		}
	})

	t.Run("aggregate untouched", func(t *testing.T) {
		tracker := NewDeviceMetricsTracker()
		tracker.knownDevices["sde-t"] = "tailscale"
		seed(t, "tailscale", fetchFailed)
		seed(t, "tailscale", "fetch_failed")
		seed(t, "tailscale", "client_scrape_failed")
		tracker.CleanupRemovedDevices(map[string]string{})
		if hasSeries(t, "tsmetrics_scrape_errors_total", scrapeErr("tailscale", fetchFailed)) {
			t.Error("client_fetch_failed series of removed device still present")
		}
		for _, errorType := range []string{"fetch_failed", "client_scrape_failed"} {
			if !hasSeries(t, "tsmetrics_scrape_errors_total", scrapeErr("tailscale", errorType)) {
				t.Errorf("collector series %s deleted with the device", errorType)
			}
		}
	})

	t.Run("empty name", func(t *testing.T) {
		tracker := NewDeviceMetricsTracker()
		tracker.knownDevices["sde-x"] = ""
		tracker.CleanupRemovedDevices(map[string]string{})
		if _, ok := tracker.knownDevices["sde-x"]; ok {
			t.Error("sde-x still tracked")
		}
	})

	t.Run("renamed", func(t *testing.T) {
		seed(t, "sde-old-name", fetchFailed)
		dropRenamedDeviceSeries(map[string]string{"sde-r": "sde-old-name"}, map[string]string{"sde-r": "sde-new-name"})
		if hasSeries(t, "tsmetrics_scrape_errors_total", scrapeErr("sde-old-name", fetchFailed)) {
			t.Error("scrape error series under old name still present after rename")
		}
	})

	t.Run("rename onto taken name", func(t *testing.T) {
		seed(t, "sde-taken", fetchFailed)
		dropRenamedDeviceSeries(
			map[string]string{"sde-r2": "sde-taken"},
			map[string]string{"sde-r2": "sde-other", "sde-z": "sde-taken"},
		)
		if !hasSeries(t, "tsmetrics_scrape_errors_total", scrapeErr("sde-taken", fetchFailed)) {
			t.Error("scrape error series deleted although another device carries the name")
		}
	})
}
