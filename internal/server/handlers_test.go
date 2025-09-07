package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnhancedHealthHandler(t *testing.T) {
	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(EnhancedHealthHandler)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, status)
	}

	if contentType := rr.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}

	// Check if response contains expected fields
	body := rr.Body.String()
	expectedFields := []string{"status", "version", "build_time", "timestamp", "memory_mb", "goroutines"}

	for _, field := range expectedFields {
		if !contains(body, field) {
			t.Errorf("Expected response to contain field %s", field)
		}
	}
}

func TestSetVersion(t *testing.T) {
	testVersion := "1.2.3"
	testBuildTime := "2025-09-03T00:00:00Z"

	SetVersion(testVersion, testBuildTime)

	if version != testVersion {
		t.Errorf("Expected version %s, got %s", testVersion, version)
	}

	if buildTime != testBuildTime {
		t.Errorf("Expected buildTime %s, got %s", testBuildTime, buildTime)
	}
}

func TestUtilityFunctions(t *testing.T) {
	// Test bToMb
	bytes := uint64(1024 * 1024 * 10) // 10 MB
	mb := bToMb(bytes)
	if mb != 10 {
		t.Errorf("Expected 10 MB, got %d", mb)
	}

	// Test getUptimeSeconds - should be at least 0
	uptime := getUptimeSeconds()
	if uptime < 0 {
		t.Error("Expected non-negative uptime")
	}

	// Test getLastScrapeTime
	lastScrape := getLastScrapeTime()
	if lastScrape <= 0 {
		t.Error("Expected positive last scrape time")
	}

	// Test getOnlineDeviceCount
	deviceCount := getOnlineDeviceCount()
	if deviceCount < 0 {
		t.Error("Expected non-negative device count")
	}
}

func contains(str, substr string) bool {
	return len(str) >= len(substr) &&
		(str == substr ||
			(len(str) > len(substr) &&
				(str[:len(substr)] == substr ||
					str[len(str)-len(substr):] == substr ||
					findSubstring(str, substr))))
}

func findSubstring(str, substr string) bool {
	for i := 0; i <= len(str)-len(substr); i++ {
		if str[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
