package metrics

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/sbaerlocher/tsmetrics/internal/config"
	"github.com/sbaerlocher/tsmetrics/internal/types"
	"github.com/sbaerlocher/tsmetrics/pkg/device"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

const testDeviceIDLabel = "device_id"

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func testPeer(id, name string) device.Device {
	return device.Device{
		ID:   types.DeviceID(id),
		Name: types.DeviceName(name),
	}
}

func testPeerCollector(recheckInterval time.Duration) *Collector {
	return &Collector{
		cfg: config.Config{
			ClientMetricsPort:    "5252",
			ClientMetricsTimeout: time.Second,
			PeerRecheckInterval:  recheckInterval,
			MaxConcurrentScrapes: 1,
		},
		peerEndpoints: make(map[string]peerEndpointState),
	}
}

func testResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func hasInboundMetric(deviceID string) bool {
	registry := prometheus.NewRegistry()
	registry.MustRegister(InboundBytes)
	metricFamilies, err := registry.Gather()
	if err != nil {
		return false
	}
	for _, family := range metricFamilies {
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == testDeviceIDLabel && label.GetValue() == deviceID {
					return true
				}
			}
		}
	}
	return false
}

func counterValue(t *testing.T, counter prometheus.Counter) float64 {
	t.Helper()
	metric := &dto.Metric{}
	if err := counter.Write(metric); err != nil {
		t.Fatalf("write Prometheus counter: %v", err)
	}
	return metric.GetCounter().GetValue()
}

func captureDebugLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	oldLogger := slog.Default()
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() {
		slog.SetDefault(oldLogger)
	})
	return &logs
}

func TestScrapePeersRechecksUnavailableEndpoints(t *testing.T) {
	const (
		deviceID   = "inventory-recheck-id"
		deviceName = "inventory-recheck.tail.ts.net"
	)
	dev := testPeer(deviceID, deviceName)
	collector := testPeerCollector(15 * time.Minute)
	logs := captureDebugLogs(t)
	defer CleanupClientMetrics(deviceID)
	defer ScrapeErrors.DeleteLabelValues(deviceName, "client_fetch_failed")

	requestCount := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requestCount++
		if requestCount == 1 {
			return testResponse(http.StatusNotFound, "not found"), nil
		}
		return testResponse(http.StatusOK, "tailscaled_inbound_bytes_total{path=\"direct\"} 42\n"), nil
	})}

	counter := ScrapeErrors.WithLabelValues(deviceName, "client_fetch_failed")
	counterBefore := counterValue(t, counter)
	start := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)

	if err := collector.scrapeClientMetricsWithClientAt(context.Background(), []device.Device{dev}, client, start); err != nil {
		t.Fatalf("first peer scrape returned an error: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("first peer scrape sent %d requests, want 1", requestCount)
	}
	state := collector.peerEndpoints[deviceID]
	if state.available || !state.retryAfter.Equal(start.Add(15*time.Minute)) {
		t.Fatalf("first failure stored state %+v", state)
	}
	if got := counterValue(t, counter); got != counterBefore {
		t.Fatalf("first discovery failure changed the scrape error counter from %v to %v", counterBefore, got)
	}

	if err := collector.scrapeClientMetricsWithClientAt(context.Background(), []device.Device{dev}, client, start.Add(14*time.Minute)); err != nil {
		t.Fatalf("early peer recheck returned an error: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("peer was rechecked before the interval: got %d requests, want 1", requestCount)
	}

	if err := collector.scrapeClientMetricsWithClientAt(context.Background(), []device.Device{dev}, client, start.Add(15*time.Minute)); err != nil {
		t.Fatalf("peer recheck returned an error: %v", err)
	}
	if requestCount != 2 || !collector.peerEndpoints[deviceID].available {
		t.Fatalf("successful recheck did not make the peer available: requests=%d state=%+v", requestCount, collector.peerEndpoints[deviceID])
	}

	if err := collector.scrapeClientMetricsWithClientAt(context.Background(), []device.Device{dev}, client, start.Add(16*time.Minute)); err != nil {
		t.Fatalf("regular peer scrape returned an error: %v", err)
	}
	if requestCount != 3 {
		t.Fatalf("available peer request count = %d, want 3", requestCount)
	}
	if !hasInboundMetric(deviceID) {
		t.Fatal("available peer did not retain its client metric")
	}

	collector.cfg.TsnetScrapeTag = "exporter"
	if err := collector.scrapeClientMetricsWithClientAt(context.Background(), []device.Device{dev}, client, start.Add(17*time.Minute)); err != nil {
		t.Fatalf("tag-filtered inventory scrape returned an error: %v", err)
	}
	if requestCount != 3 {
		t.Fatalf("tag-filtered peer sent another request: got %d requests, want 3", requestCount)
	}
	if hasInboundMetric(deviceID) {
		t.Fatal("tag-filtered peer retained its client metric")
	}
	if len(collector.peerEndpoints) != 0 {
		t.Fatalf("tag-filtered peer remained in the endpoint inventory: %+v", collector.peerEndpoints)
	}

	collector.cfg.TsnetScrapeTag = ""
	collector.peerEndpoints[deviceID] = peerEndpointState{available: true}
	if err := collector.scrapeClientMetricsWithClientAt(context.Background(), nil, client, start.Add(18*time.Minute)); err != nil {
		t.Fatalf("empty inventory scrape returned an error: %v", err)
	}
	if len(collector.peerEndpoints) != 0 {
		t.Fatalf("removed peer remained in the endpoint inventory: %+v", collector.peerEndpoints)
	}
	if strings.Contains(logs.String(), "level=WARN") || strings.Contains(logs.String(), "level=ERROR") {
		t.Fatalf("initial peer discovery failure produced a warning or error log:\n%s", logs.String())
	}
}

func TestScrapePeersSuppressesRepeatedFailuresAfterEndpointBecomesUnavailable(t *testing.T) {
	const (
		deviceID   = "inventory-failure-id"
		deviceName = "inventory-failure.tail.ts.net"
	)
	dev := testPeer(deviceID, deviceName)
	collector := testPeerCollector(15 * time.Minute)
	logs := captureDebugLogs(t)
	defer CleanupClientMetrics(deviceID)
	defer ScrapeErrors.DeleteLabelValues(deviceName, "client_fetch_failed")
	defer DeviceErrors.DeletePartialMatch(prometheus.Labels{testDeviceIDLabel: deviceID})

	requestCount := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requestCount++
		if requestCount == 1 {
			return testResponse(http.StatusOK, "tailscaled_inbound_bytes_total{path=\"direct\"} 42\n"), nil
		}
		return testResponse(http.StatusInternalServerError, "failed"), nil
	})}

	counter := ScrapeErrors.WithLabelValues(deviceName, "client_fetch_failed")
	counterBefore := counterValue(t, counter)
	start := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)

	if err := collector.scrapeClientMetricsWithClientAt(context.Background(), []device.Device{dev}, client, start); err != nil {
		t.Fatalf("first peer scrape returned an error: %v", err)
	}
	if !hasInboundMetric(deviceID) {
		t.Fatal("successful peer scrape did not store the client metric")
	}

	if err := collector.scrapeClientMetricsWithClientAt(context.Background(), []device.Device{dev}, client, start.Add(time.Minute)); err != nil {
		t.Fatalf("failed peer scrape returned an error: %v", err)
	}
	if got := counterValue(t, counter); got != counterBefore+1 {
		t.Fatalf("known peer failure changed the scrape error counter to %v, want %v", got, counterBefore+1)
	}
	if hasInboundMetric(deviceID) {
		t.Fatal("known peer failure did not remove its cached client metric")
	}

	if err := collector.scrapeClientMetricsWithClientAt(context.Background(), []device.Device{dev}, client, start.Add(2*time.Minute)); err != nil {
		t.Fatalf("suppressed peer scrape returned an error: %v", err)
	}
	if requestCount != 2 {
		t.Fatalf("unavailable peer was retried immediately: got %d requests, want 2", requestCount)
	}
	if got := counterValue(t, counter); got != counterBefore+1 {
		t.Fatalf("suppressed peer changed the scrape error counter to %v, want %v", got, counterBefore+1)
	}

	if err := collector.scrapeClientMetricsWithClientAt(context.Background(), []device.Device{dev}, client, start.Add(16*time.Minute)); err != nil {
		t.Fatalf("peer recheck returned an error: %v", err)
	}
	if requestCount != 3 {
		t.Fatalf("peer was not rechecked after the interval: got %d requests, want 3", requestCount)
	}
	if got := counterValue(t, counter); got != counterBefore+1 {
		t.Fatalf("failed recheck changed the scrape error counter to %v, want %v", got, counterBefore+1)
	}
	if got := strings.Count(logs.String(), "level=WARN"); got != 1 {
		t.Fatalf("known peer failures produced %d warning logs, want 1:\n%s", got, logs.String())
	}
}

func TestValidateDeviceMetricsURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr string // substring; "" means no error
	}{
		// --- blocked literals ---
		{"reject IPv4 loopback", "http://127.0.0.1:5252/metrics", "forbidden range"},
		{"reject current-network zero", "http://0.0.0.0:5252/metrics", "forbidden range"},
		{"reject IPv4 link-local (cloud metadata)", "http://169.254.169.254:5252/metrics", "forbidden range"},
		{"reject IPv6 loopback", "http://[::1]:5252/metrics", "forbidden range"},
		{"reject IPv6 link-local", "http://[fe80::1]:5252/metrics", "forbidden range"},
		{"reject localhost string", "http://localhost:5252/metrics", "not a permitted scrape target"},
		{"reject localhost string case-insensitive", "http://LocalHost:5252/metrics", "not a permitted scrape target"},

		// --- allowed literals (tailnet / homelab ranges) ---
		{"allow RFC1918 10/8", "http://10.0.0.1:5252/metrics", ""},
		{"allow RFC1918 192.168/16", "http://192.168.1.1:5252/metrics", ""},
		{"allow Tailscale CGNAT 100.64/10", "http://100.64.0.1:5252/metrics", ""},

		// --- DNS names ---
		{"allow MagicDNS FQDN", "http://device.tail1234.ts.net:5252/metrics", ""},
		{"allow short hostname", "http://device:5252/metrics", ""},

		// --- scheme enforcement ---
		{"reject ftp scheme", "ftp://device.tail1234.ts.net/metrics", "scheme"},
		{"reject file scheme", "file:///etc/passwd", "scheme"},

		// --- malformed / edge cases ---
		{"reject URL with no host", "http:///metrics", "no host"},
		{"reject malformed URL", "http://%zz/metrics", "malformed URL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDeviceMetricsURL(tt.url)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("validateDeviceMetricsURL(%q) unexpected error: %v", tt.url, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateDeviceMetricsURL(%q) expected error containing %q, got nil", tt.url, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("validateDeviceMetricsURL(%q) error = %v; want substring %q", tt.url, err, tt.wantErr)
			}
		})
	}
}
