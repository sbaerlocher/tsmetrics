package security

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInputValidator_ValidateString(t *testing.T) {
	validator := NewInputValidator()

	tests := []struct {
		name        string
		input       string
		fieldName   string
		expectError bool
	}{
		{
			name:        "valid string",
			input:       "test-device-123",
			fieldName:   "device_name",
			expectError: false,
		},
		{
			name:        "empty string",
			input:       "",
			fieldName:   "device_name",
			expectError: true,
		},
		{
			name:        "too long string",
			input:       strings.Repeat("a", 1001),
			fieldName:   "device_name",
			expectError: true,
		},
		{
			name:        "control characters",
			input:       "test\x00device",
			fieldName:   "device_name",
			expectError: true,
		},
		{
			name:        "allowed tabs and newlines",
			input:       "test\tdevice\n",
			fieldName:   "device_name",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateString(tt.input, tt.fieldName)
			if (err != nil) != tt.expectError {
				t.Errorf("ValidateString() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestInputValidator_ValidateURL(t *testing.T) {
	validator := NewInputValidator()

	tests := []struct {
		name        string
		input       string
		expectError bool
	}{
		{
			name:        "valid HTTPS URL",
			input:       "https://api.tailscale.com/v2/device",
			expectError: false,
		},
		{
			name:        "valid HTTPS URL for testing",
			input:       "https://example.com", // Test URL
			expectError: false,
		},
		{
			name:        "invalid scheme",
			input:       "ftp://example.com",
			expectError: true,
		},
		{
			name:        "localhost URL",
			input:       "http://localhost:8080", // DevSkim: ignore DS162092 - Test case for localhost validation
			expectError: true,
		},
		{
			name:        "malformed URL",
			input:       "not-a-url",
			expectError: true,
		},
		{
			name:        "empty URL",
			input:       "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateURL(tt.input, "test_url")
			if (err != nil) != tt.expectError {
				t.Errorf("ValidateURL() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestInputValidator_ValidateToken(t *testing.T) {
	validator := NewInputValidator()

	tests := []struct {
		name        string
		token       string
		expectError bool
	}{
		{
			name:        "valid token",
			token:       "tskey-1234567890abcdef",
			expectError: false,
		},
		{
			name:        "empty token",
			token:       "",
			expectError: true,
		},
		{
			name:        "too short token",
			token:       "short",
			expectError: true,
		},
		{
			name:        "too long token",
			token:       strings.Repeat("a", 501),
			expectError: true,
		},
		{
			name:        "invalid characters",
			token:       "token-with-@-symbol",
			expectError: true,
		},
		{
			name:        "valid token with underscores and dots",
			token:       "tskey_client.12345.abcdef",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateToken(tt.token)
			if (err != nil) != tt.expectError {
				t.Errorf("ValidateToken() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestInputValidator_ValidateDeviceID(t *testing.T) {
	validator := NewInputValidator()

	tests := []struct {
		name        string
		deviceID    string
		expectError bool
	}{
		{
			name:        "valid device ID",
			deviceID:    "device-123",
			expectError: false,
		},
		{
			name:        "valid device ID with underscores",
			deviceID:    "device_123",
			expectError: false,
		},
		{
			name:        "too long device ID",
			deviceID:    strings.Repeat("a", 51),
			expectError: true,
		},
		{
			name:        "invalid characters",
			deviceID:    "device@123",
			expectError: true,
		},
		{
			name:        "empty device ID",
			deviceID:    "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateDeviceID(tt.deviceID)
			if (err != nil) != tt.expectError {
				t.Errorf("ValidateDeviceID() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestRateLimiter(t *testing.T) {
	// Create a rate limiter that allows 2 requests per second with burst of 1
	limiter := NewRateLimiter(2.0, 1)

	clientID := "test-client"

	// First request should be allowed
	if !limiter.Allow(clientID) {
		t.Error("First request should be allowed")
	}

	// Second request should be denied (exceeds burst)
	if limiter.Allow(clientID) {
		t.Error("Second immediate request should be denied")
	}

	// Wait for rate limiter to recover
	time.Sleep(time.Second)

	// Request should be allowed again
	if !limiter.Allow(clientID) {
		t.Error("Request after waiting should be allowed")
	}
}

func TestSecurityHeadersMiddleware(t *testing.T) {
	handler := SecurityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name            string
		path            string
		expectedHeaders map[string]string
	}{
		{
			name: "security headers for regular endpoint",
			path: "/test",
			expectedHeaders: map[string]string{
				"X-Content-Type-Options": "nosniff",
				"X-Frame-Options":        "DENY",
				"X-XSS-Protection":       "1; mode=block",
			},
		},
		{
			name: "cache headers for API endpoint",
			path: "/api/devices",
			expectedHeaders: map[string]string{
				"Cache-Control": "no-store, no-cache, must-revalidate, private",
				"Pragma":        "no-cache",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			for header, expected := range tt.expectedHeaders {
				actual := w.Header().Get(header)
				if actual != expected {
					t.Errorf("Header %s = %s, expected %s", header, actual, expected)
				}
			}
		})
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	limiter := NewRateLimiter(1.0, 1) // 1 request per second, burst of 1

	handler := RateLimitMiddleware(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request should succeed
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "192.168.1.1:12345"
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Errorf("First request should succeed, got status %d", w1.Code)
	}

	// Second immediate request should be rate limited
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.1:12346" // Same IP, different port
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("Second request should be rate limited, got status %d", w2.Code)
	}
}

func TestAuthValidator(t *testing.T) {
	validator := NewAuthValidator()

	token := "test-token-123"
	validator.AddValidToken(token)

	// Valid token should be accepted
	if !validator.ValidateToken(token) {
		t.Error("Valid token should be accepted")
	}

	// Invalid token should be rejected
	if validator.ValidateToken("invalid-token") {
		t.Error("Invalid token should be rejected")
	}

	// Test secure validation
	if !validator.SecureValidateToken(token) {
		t.Error("Valid token should be accepted by secure validation")
	}

	// Remove token
	validator.RemoveToken(token)
	if validator.ValidateToken(token) {
		t.Error("Removed token should be rejected")
	}
}

func TestAuthenticationMiddleware(t *testing.T) {
	validator := NewAuthValidator()
	token := "valid-token-123"
	validator.AddValidToken(token)

	handler := AuthenticationMiddleware(validator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name           string
		path           string
		authHeader     string
		expectedStatus int
	}{
		{
			name:           "health check bypass",
			path:           "/health",
			authHeader:     "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid token",
			path:           "/api/devices",
			authHeader:     "Bearer " + token,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing auth header",
			path:           "/api/devices",
			authHeader:     "",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "invalid token format",
			path:           "/api/devices",
			authHeader:     "Token " + token,
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "invalid token",
			path:           "/api/devices",
			authHeader:     "Bearer invalid-token",
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

func TestRequestSizeLimitMiddleware(t *testing.T) {
	maxSize := int64(100) // 100 bytes limit
	handler := RequestSizeLimitMiddleware(maxSize)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to read the body
		buf := make([]byte, 200)
		_, err := r.Body.Read(buf)
		if err != nil && err.Error() != "EOF" {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name           string
		bodySize       int
		expectedStatus int
	}{
		{
			name:           "small request",
			bodySize:       50,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "large request",
			bodySize:       150,
			expectedStatus: http.StatusRequestEntityTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := strings.NewReader(strings.Repeat("a", tt.bodySize))
			req := httptest.NewRequest("POST", "/test", body)
			req.ContentLength = int64(tt.bodySize)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", w.Code, tt.expectedStatus)
			}
		})
	}
}

func TestGetClientID(t *testing.T) {
	tests := []struct {
		name          string
		remoteAddr    string
		xForwardedFor string
		xRealIP       string
		expectedIP    string
	}{
		{
			name:       "remote addr only",
			remoteAddr: "192.168.1.1:12345",
			expectedIP: "192.168.1.1",
		},
		{
			name:          "x-forwarded-for header",
			remoteAddr:    "10.0.0.1:12345",
			xForwardedFor: "192.168.1.1, 10.0.0.2",
			expectedIP:    "192.168.1.1",
		},
		{
			name:       "x-real-ip header",
			remoteAddr: "10.0.0.1:12345",
			xRealIP:    "192.168.1.1",
			expectedIP: "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}
			if tt.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.xRealIP)
			}

			clientID := getClientID(req)
			if clientID != tt.expectedIP {
				t.Errorf("Expected client ID %s, got %s", tt.expectedIP, clientID)
			}
		})
	}
}

func TestGenerateSecurityAuditReport(t *testing.T) {
	report := GenerateSecurityAuditReport()

	if report.Timestamp.IsZero() {
		t.Error("Audit report should have a timestamp")
	}

	if report.RiskLevel != "LOW" {
		t.Errorf("Expected risk level LOW, got %s", report.RiskLevel)
	}

	expectedFeatures := []string{
		"input_validation",
		"rate_limiting",
		"security_headers",
		"authentication",
		"request_timeout",
		"request_size_limiting",
	}

	for _, feature := range expectedFeatures {
		if !report.SecurityFeatures[feature] {
			t.Errorf("Security feature %s should be enabled", feature)
		}
	}

	if len(report.Recommendations) == 0 {
		t.Error("Audit report should contain recommendations")
	}
}

func TestRateLimiterCleanup(t *testing.T) {
	limiter := NewRateLimiter(10.0, 5) // High rate for testing

	// Create some limiters
	limiter.Allow("client1")
	limiter.Allow("client2")

	// Check initial state
	limiter.mutex.RLock()
	initialCount := len(limiter.limiters)
	limiter.mutex.RUnlock()

	if initialCount != 2 {
		t.Errorf("Expected 2 limiters, got %d", initialCount)
	}

	// Wait for limiters to reset to full capacity, then cleanup
	time.Sleep(100 * time.Millisecond)
	limiter.Cleanup()

	limiter.mutex.RLock()
	finalCount := len(limiter.limiters)
	limiter.mutex.RUnlock()

	// Limiters at full capacity should be cleaned up
	if finalCount >= initialCount {
		t.Errorf("Cleanup should have removed some limiters, initial: %d, final: %d", initialCount, finalCount)
	}
}
