package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
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
		{"reject IPv4-mapped IPv6 loopback", "http://[::ffff:127.0.0.1]:5252/metrics", "forbidden range"},
		{"reject localhost string", "http://localhost:5252/metrics", "not a permitted scrape target"},
		{"reject localhost string case-insensitive", "http://LocalHost:5252/metrics", "not a permitted scrape target"},

		// --- allowed literals (tailnet / homelab ranges) ---
		{"allow RFC1918 10/8", "http://10.0.0.1:5252/metrics", ""},
		{"allow RFC1918 192.168/16", "http://192.168.1.1:5252/metrics", ""},
		{"allow Tailscale CGNAT 100.64/10", "http://100.64.0.1:5252/metrics", ""},
		{"allow Tailscale IPv6 ULA", "http://[fd7a:115c:a1e0::1]:5252/metrics", ""},

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

// A device answering with a redirect to a blocked target must not be followed,
// and a connection to a blocked resolved IP must be refused at dial time.
func TestScrapeClientSSRFGuards(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound) // DevSkim: ignore DS162092
	}))
	defer srv.Close()

	// Default transport: httptest listens on loopback, which safeDialer would refuse.
	client := &http.Client{CheckRedirect: checkRedirect}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "forbidden range") {
		t.Fatalf("redirect to link-local: want forbidden range error, got %v", err)
	}

	_, err = safeDialer.DialContext(context.Background(), "tcp", srv.Listener.Addr().String())
	if err == nil || !strings.Contains(err.Error(), "forbidden range") {
		t.Fatalf("dial to loopback: want forbidden range error, got %v", err)
	}
}
