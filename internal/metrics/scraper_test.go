package metrics

import (
	"strings"
	"testing"
)

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
