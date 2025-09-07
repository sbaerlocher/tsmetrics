package errors

import (
	"errors"
	"testing"
	"time"
)

func TestAPIError(t *testing.T) {
	underlyingErr := errors.New("connection failed")
	apiErr := NewAPIError("/api/devices", 500, underlyingErr)

	if apiErr.Endpoint != "/api/devices" {
		t.Errorf("Expected endpoint '/api/devices', got %s", apiErr.Endpoint)
	}

	if apiErr.StatusCode != 500 {
		t.Errorf("Expected status code 500, got %d", apiErr.StatusCode)
	}

	if !apiErr.IsRetryable() {
		t.Error("Expected 500 error to be retryable")
	}

	if apiErr.Unwrap() != underlyingErr {
		t.Error("Unwrap() should return underlying error")
	}
}

func TestAPIErrorRetryable(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		retryable  bool
	}{
		{"500 Internal Server Error", 500, true},
		{"502 Bad Gateway", 502, true},
		{"503 Service Unavailable", 503, true},
		{"429 Too Many Requests", 429, true},
		{"408 Request Timeout", 408, true},
		{"400 Bad Request", 400, false},
		{"401 Unauthorized", 401, false},
		{"404 Not Found", 404, false},
		{"200 OK", 200, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiErr := NewAPIError("/test", tt.statusCode, errors.New("test error"))
			if apiErr.IsRetryable() != tt.retryable {
				t.Errorf("Expected retryable=%v for status %d, got %v", tt.retryable, tt.statusCode, apiErr.IsRetryable())
			}
		})
	}
}

func TestRetryConfigCalculateDelay(t *testing.T) {
	config := RetryConfig{
		MaxAttempts: 5,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    10 * time.Second,
		Multiplier:  2.0,
	}

	tests := []struct {
		name     string
		attempt  int
		expected time.Duration
	}{
		{"first attempt", 0, 100 * time.Millisecond},
		{"second attempt", 1, 200 * time.Millisecond},
		{"third attempt", 2, 400 * time.Millisecond},
		{"fourth attempt", 3, 800 * time.Millisecond},
		{"large attempt hits max", 10, 10 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delay := config.CalculateDelay(tt.attempt)
			if delay != tt.expected {
				t.Errorf("Expected delay %v for attempt %d, got %v", tt.expected, tt.attempt, delay)
			}
		})
	}
}

func TestDefaultRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()

	if config.MaxAttempts != 3 {
		t.Errorf("Expected MaxAttempts=3, got %d", config.MaxAttempts)
	}

	if config.BaseDelay != 100*time.Millisecond {
		t.Errorf("Expected BaseDelay=100ms, got %v", config.BaseDelay)
	}

	if config.MaxDelay != 10*time.Second {
		t.Errorf("Expected MaxDelay=10s, got %v", config.MaxDelay)
	}

	if config.Multiplier != 2.0 {
		t.Errorf("Expected Multiplier=2.0, got %f", config.Multiplier)
	}
}

func TestDeviceError(t *testing.T) {
	underlyingErr := errors.New("network timeout")
	deviceErr := DeviceError{
		DeviceID:   "device123",
		DeviceName: "test-device",
		ErrorType:  "network",
		Underlying: underlyingErr,
		Retryable:  true,
		RetryAfter: 30 * time.Second,
		Timestamp:  time.Now(),
	}

	expectedMsg := "device test-device (device123) network: network timeout (retryable after 30s)"
	if deviceErr.Error() != expectedMsg {
		t.Errorf("Expected error message %q, got %q", expectedMsg, deviceErr.Error())
	}

	if !deviceErr.IsRetryable() {
		t.Error("Expected device error to be retryable")
	}

	if deviceErr.Unwrap() != underlyingErr {
		t.Error("Unwrap() should return underlying error")
	}
}
