package types

import (
	"testing"
)

func TestDeviceID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"valid ID", "device123", false},
		{"empty ID", "", true},
		{"too long ID", string(make([]byte, 65)), true},
		{"valid max length", string(make([]byte, 64)), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deviceID, err := NewDeviceID(tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewDeviceID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !deviceID.IsValid() {
				t.Errorf("DeviceID.IsValid() = false, want true")
			}
		})
	}
}

func TestDeviceName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid name", "device-01", false},
		{"empty name", "", true},
		{"too long name", string(make([]byte, 254)), true},
		{"invalid characters", "device@host", true},
		{"valid with dots", "device.example.com", false},
		{"valid with underscores", "device_name", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deviceName, err := NewDeviceName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewDeviceName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !deviceName.IsValid() {
				t.Errorf("DeviceName.IsValid() = false, want true")
			}
		})
	}
}

func TestDeviceNameSanitize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"spaces to dashes", "device name", "device-name"},
		{"multiple special chars", "device@#$name", "device-name"},
		{"consecutive dashes", "device---name", "device-name"},
		{"leading/trailing dashes", "-device-name-", "device-name"},
		{"uppercase to lowercase", "DEVICE-NAME", "device-name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deviceName := DeviceName(tt.input)
			sanitized := deviceName.Sanitize()
			if string(sanitized) != tt.expected {
				t.Errorf("DeviceName.Sanitize() = %v, want %v", string(sanitized), tt.expected)
			}
		})
	}
}

func TestMetricName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid metric name", "http_requests_total", false},
		{"starts with letter", "requests_total", false},
		{"starts with underscore", "_requests", false},
		{"starts with number", "1requests", true},
		{"empty name", "", true},
		{"contains special chars", "http-requests", true},
		{"valid with numbers", "requests_2xx", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metricName, err := NewMetricName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewMetricName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !metricName.IsValid() {
				t.Errorf("MetricName.IsValid() = false, want true")
			}
		})
	}
}

func TestTagName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid simple tag", "gateway", false},
		{"valid exporter tag", "exporter", false},
		{"starts with letter", "production", false},
		{"starts with underscore", "_internal", false},
		{"starts with colon", ":invalid", true},
		{"starts with number", "1invalid", true},
		{"empty name", "", true},
		{"valid with dash", "multi-word", false},
		{"valid with underscore", "multi_word", false},
		{"valid complex", "multi-word_123", false},
		{"tag with colon prefix should be rejected", "tag:gateway", true},
		{"tag with colon prefix exporter", "tag:exporter", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tagName, err := NewTagName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewTagName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !tagName.IsValid() {
				t.Errorf("TagName.IsValid() = false, want true")
			}
		})
	}
}

func TestValidateHostname(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		wantErr  bool
	}{
		{"valid domain", "example.com", false},
		{"valid subdomain", "api.example.com", false},
		{"valid IP", "8.8.8.8", false},
		{"private IP", "192.168.1.1", true},
		{"loopback", "127.0.0.1", true}, // DevSkim: ignore DS162092 - Test case for IP validation
		{"empty hostname", "", true},
		{"too long", string(make([]byte, 254)), true},
		{"invalid format", "example..com", true},
		{"starts with dash", "-example.com", true},
		{"ends with dash", "example.com-", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHostname(tt.hostname)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateHostname() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
