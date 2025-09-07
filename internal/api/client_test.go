package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sbaerlocher/tsmetrics/pkg/device"
)

type mockTransport struct {
	responses map[string]*http.Response
	requests  []*http.Request
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m.requests = append(m.requests, req)

	url := req.URL.String()
	if resp, exists := m.responses[url]; exists {
		return resp, nil
	}

	return &http.Response{
		StatusCode: 404,
		Body:       io.NopCloser(bytes.NewBufferString("Not Found")),
	}, nil
}

func createMockResponse(statusCode int, body interface{}) *http.Response {
	var bodyReader io.ReadCloser

	if body != nil {
		bodyBytes, _ := json.Marshal(body)
		bodyReader = io.NopCloser(bytes.NewBuffer(bodyBytes))
	} else {
		bodyReader = io.NopCloser(bytes.NewBufferString(""))
	}

	return &http.Response{
		StatusCode: statusCode,
		Body:       bodyReader,
		Header:     make(http.Header),
	}
}

func TestNewClient(t *testing.T) {
	client := NewClient("test-client", "test-secret", "test-tailnet")

	if client == nil {
		t.Fatal("Expected client to be created")
	}

	if client.baseURL != "https://api.tailscale.com/api/v2/tailnet/test-tailnet" {
		t.Errorf("Expected baseURL to contain tailnet, got %s", client.baseURL)
	}
}

func TestNewClientWithToken(t *testing.T) {
	client := NewClientWithToken("test-token", "test-tailnet")

	if client == nil {
		t.Fatal("Expected client to be created")
	}

	if client.baseURL != "https://api.tailscale.com/api/v2/tailnet/test-tailnet" {
		t.Errorf("Expected baseURL to contain tailnet, got %s", client.baseURL)
	}
}

func TestClientFetchDevices(t *testing.T) {
	mockDevices := []device.Device{
		{
			ID:         "device1",
			Name:       "test-device-1",
			Host:       "example.com",
			Online:     true,
			Authorized: true,
		},
		{
			ID:         "device2",
			Name:       "test-device-2",
			Host:       "example2.com",
			Online:     false,
			Authorized: true,
		},
	}

	responseBody := map[string]interface{}{
		"devices": mockDevices,
	}

	mockTransport := &mockTransport{
		responses: make(map[string]*http.Response),
		requests:  make([]*http.Request, 0),
	}

	mockTransport.responses["https://api.tailscale.com/api/v2/tailnet/test-tailnet/devices?fields=all"] =
		createMockResponse(200, responseBody)

	// Add mock responses for route API calls
	routesResponse1 := map[string][]string{
		"advertisedRoutes": {},
		"enabledRoutes":    {},
	}
	routesResponse2 := map[string][]string{
		"advertisedRoutes": {"192.168.1.0/24"},
		"enabledRoutes":    {"192.168.1.0/24"},
	}
	mockTransport.responses["https://api.tailscale.com/api/v2/device/device1/routes?fields=all"] =
		createMockResponse(200, routesResponse1)
	mockTransport.responses["https://api.tailscale.com/api/v2/device/device2/routes?fields=all"] =
		createMockResponse(200, routesResponse2)

	client := &Client{
		httpClient: &http.Client{
			Transport: mockTransport,
			Timeout:   30 * time.Second,
		},
		baseURL: "https://api.tailscale.com/api/v2/tailnet/test-tailnet",
	}

	devices, err := client.FetchDevices()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(devices) != 2 {
		t.Errorf("Expected 2 devices, got %d", len(devices))
	}

	if devices[0].ID != "device1" {
		t.Errorf("Expected first device ID 'device1', got %s", devices[0].ID)
	}

	if devices[1].Name != "test-device-2" {
		t.Errorf("Expected second device name 'test-device-2', got %s", devices[1].Name)
	}

	// Expect 3 requests: 1 for devices + 2 for routes
	if len(mockTransport.requests) != 3 {
		t.Errorf("Expected 3 requests, got %d", len(mockTransport.requests))
	}

	req := mockTransport.requests[0]
	if req.Method != "GET" {
		t.Errorf("Expected GET request, got %s", req.Method)
	}
}

func TestClientFetchDevicesError(t *testing.T) {
	mockTransport := &mockTransport{
		responses: make(map[string]*http.Response),
		requests:  make([]*http.Request, 0),
	}

	mockTransport.responses["https://api.tailscale.com/api/v2/tailnet/test-tailnet/devices?fields=all"] =
		createMockResponse(500, "Internal Server Error")

	client := &Client{
		httpClient: &http.Client{
			Transport: mockTransport,
			Timeout:   30 * time.Second,
		},
		baseURL: "https://api.tailscale.com/api/v2/tailnet/test-tailnet",
	}

	devices, err := client.FetchDevices()
	if err == nil {
		t.Fatal("Expected error for 500 status code")
	}

	if devices != nil {
		t.Error("Expected devices to be nil on error")
	}
}

func TestClientWithRealServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/devices" {
			response := map[string]interface{}{
				"devices": []device.Device{
					{
						ID:   "test-device",
						Name: "test",
						Host: "test.example.com",
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    server.URL,
	}

	devices, err := client.FetchDevices()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(devices) != 1 {
		t.Errorf("Expected 1 device, got %d", len(devices))
	}

	if devices[0].ID != "test-device" {
		t.Errorf("Expected device ID 'test-device', got %s", devices[0].ID)
	}
}

func TestClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(200)
	}))
	defer server.Close()

	client := &Client{
		httpClient: &http.Client{Timeout: 100 * time.Millisecond},
		baseURL:    server.URL,
	}

	_, err := client.FetchDevices()
	if err == nil {
		t.Fatal("Expected timeout error")
	}
}

func TestParseTags(t *testing.T) {
	client := NewClient("test-client", "test-secret", "test-tailnet")

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "tags with prefix",
			input:    []string{"tag:exporter", "tag:gateway"},
			expected: []string{"exporter", "gateway"},
		},
		{
			name:     "tags without prefix",
			input:    []string{"exporter", "gateway"},
			expected: []string{"exporter", "gateway"},
		},
		{
			name:     "mixed tags",
			input:    []string{"tag:exporter", "production", "tag:gateway"},
			expected: []string{"exporter", "production", "gateway"},
		},
		{
			name:     "empty tags",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "invalid tags filtered out",
			input:    []string{"tag:valid", "tag:", "tag:123invalid"},
			expected: []string{"valid"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.parseTags(tt.input, "test-device")

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d tags, got %d", len(tt.expected), len(result))
				return
			}

			for i, expected := range tt.expected {
				if result[i].String() != expected {
					t.Errorf("Expected tag %s, got %s", expected, result[i].String())
				}
			}
		})
	}
}

func TestClientExitNodeDetection(t *testing.T) {
	mockTransport := &mockTransport{
		responses: make(map[string]*http.Response),
		requests:  make([]*http.Request, 0),
	}

	// Mock devices response
	devices := []map[string]interface{}{
		{
			"id":         "exit-node-device",
			"name":       "exit-node.example.com",
			"hostname":   "exit-node",
			"addresses":  []string{"100.64.0.1"},
			"tags":       []string{"tag:gateway"},
			"authorized": true,
			"lastSeen":   time.Now().Add(-5 * time.Minute).Format(time.RFC3339),
			"user":       "test@example.com",
		},
		{
			"id":         "subnet-router-device",
			"name":       "subnet-router.example.com",
			"hostname":   "subnet-router",
			"addresses":  []string{"100.64.0.2"},
			"tags":       []string{"tag:gateway"},
			"authorized": true,
			"lastSeen":   time.Now().Add(-3 * time.Minute).Format(time.RFC3339),
			"user":       "test@example.com",
		},
		{
			"id":         "regular-device",
			"name":       "regular.example.com",
			"hostname":   "regular",
			"addresses":  []string{"100.64.0.3"},
			"tags":       []string{},
			"authorized": true,
			"lastSeen":   time.Now().Add(-1 * time.Minute).Format(time.RFC3339),
			"user":       "test@example.com",
		},
	}

	responseBody := map[string]interface{}{
		"devices": devices,
	}

	mockTransport.responses["https://api.tailscale.com/api/v2/tailnet/test-tailnet/devices?fields=all"] =
		createMockResponse(200, responseBody)

	// Mock route responses for different device types
	exitNodeRoutes := map[string][]string{
		"advertisedRoutes": {"0.0.0.0/0", "::/0"},
		"enabledRoutes":    {"0.0.0.0/0", "::/0"},
	}
	subnetRouterRoutes := map[string][]string{
		"advertisedRoutes": {"0.0.0.0/0", "::/0", "192.168.1.0/24"},
		"enabledRoutes":    {"0.0.0.0/0", "::/0", "192.168.1.0/24"},
	}
	regularDeviceRoutes := map[string][]string{
		"advertisedRoutes": {},
		"enabledRoutes":    {},
	}

	mockTransport.responses["https://api.tailscale.com/api/v2/device/exit-node-device/routes?fields=all"] =
		createMockResponse(200, exitNodeRoutes)
	mockTransport.responses["https://api.tailscale.com/api/v2/device/subnet-router-device/routes?fields=all"] =
		createMockResponse(200, subnetRouterRoutes)
	mockTransport.responses["https://api.tailscale.com/api/v2/device/regular-device/routes?fields=all"] =
		createMockResponse(200, regularDeviceRoutes)

	client := &Client{
		httpClient: &http.Client{
			Transport: mockTransport,
			Timeout:   30 * time.Second,
		},
		baseURL: "https://api.tailscale.com/api/v2/tailnet/test-tailnet",
	}

	devices_result, err := client.FetchDevices()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(devices_result) != 3 {
		t.Errorf("Expected 3 devices, got %d", len(devices_result))
	}

	// Test exit node device
	exitNode := devices_result[0]
	if !exitNode.IsExitNode {
		t.Error("Expected exit-node-device to be an exit node")
	}
	if !exitNode.ExitNodeOption {
		t.Error("Expected exit-node-device to have exit node option")
	}

	// Test subnet router + exit node device
	subnetRouter := devices_result[1]
	if !subnetRouter.IsExitNode {
		t.Error("Expected subnet-router-device to be an exit node")
	}
	if !subnetRouter.ExitNodeOption {
		t.Error("Expected subnet-router-device to have exit node option")
	}

	// Test regular device
	regular := devices_result[2]
	if regular.IsExitNode {
		t.Error("Expected regular-device NOT to be an exit node")
	}
	if regular.ExitNodeOption {
		t.Error("Expected regular-device NOT to have exit node option")
	}
}
