package metrics

import (
	"context"
	stderrors "errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
		{"reject IPv6 unspecified", "http://[::]:5252/metrics", "forbidden range"},
		{"reject zoned IPv6 link-local", "http://[fe80::1%25eth0]:5252/metrics", "forbidden range"},
		{"reject AWS IPv6 metadata", "http://[fd00:ec2::254]:5252/metrics", "forbidden range"},
		{"reject NAT64 link-local", "http://[64:ff9b::a9fe:a9fe]:5252/metrics", "forbidden range"},
		{"reject IPv4-compatible loopback", "http://[::7f00:1]:5252/metrics", "forbidden range"},
		{"reject localhost with trailing dot", "http://localhost.:5252/metrics", "not a permitted scrape target"},
		{"reject localhost subdomain", "http://foo.localhost:5252/metrics", "not a permitted scrape target"},
		{"allow NAT64 public", "http://[64:ff9b::808:808]:5252/metrics", ""},
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
	if !stderrors.Is(err, errForbiddenTarget) || !strings.Contains(err.Error(), "not followed") {
		t.Fatalf("redirect: want refused redirect, got %v", err)
	}

	_, err = safeDialer.DialContext(context.Background(), "tcp", srv.Listener.Addr().String())
	if err == nil || !strings.Contains(err.Error(), "forbidden range") {
		t.Fatalf("dial to loopback: want forbidden range error, got %v", err)
	}

	// tsnet path: guardedDial must reject a connection whose peer is blocked
	// or unknown, and close it.
	for name, remote := range map[string]net.Addr{
		"loopback peer": &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 80},
		"nil peer":      nil,
	} {
		fc := &fakeConn{remote: remote}
		dial := guardedDial(func(context.Context, string, string) (net.Conn, error) { return fc, nil })
		if _, err := dial(context.Background(), "tcp", "device:5252"); !stderrors.Is(err, errForbiddenTarget) {
			t.Errorf("%s: want errForbiddenTarget, got %v", name, err)
		}
		if !fc.closed {
			t.Errorf("%s: connection not closed", name)
		}
	}
	fc := &fakeConn{remote: &net.TCPAddr{IP: net.IPv4(100, 64, 0, 1), Port: 5252}}
	dial := guardedDial(func(context.Context, string, string) (net.Conn, error) { return fc, nil })
	if _, err := dial(context.Background(), "tcp", "device:5252"); err != nil {
		t.Errorf("tailnet peer: unexpected error %v", err)
	}

	// The production clients must carry both guards.
	c := NewStandardHTTPClientProvider(time.Second, 1).GetClient()
	if c.CheckRedirect == nil || c.Timeout != time.Second {
		t.Fatalf("standard client: CheckRedirect/Timeout not wired: %+v", c)
	}
	if tr, ok := c.Transport.(*http.Transport); !ok || tr.DialContext == nil {
		t.Fatal("standard client: safeDialer not wired")
	}
	p := NewStandardHTTPClientProvider(time.Second, 1)
	if first, second := p.GetClient(), p.GetClient(); first != second {
		t.Fatal("standard client: not reused across calls")
	}
	if (&TsnetHTTPClientProvider{}).GetClient().CheckRedirect == nil {
		t.Fatal("tsnet client: CheckRedirect not wired")
	}
}

type fakeConn struct {
	net.Conn
	remote net.Addr
	closed bool
}

func (c *fakeConn) RemoteAddr() net.Addr { return c.remote }
func (c *fakeConn) Close() error         { c.closed = true; return nil }
