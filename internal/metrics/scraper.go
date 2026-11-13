// Package metrics provides client metrics scraping functionality.
package metrics

import (
	"bufio"
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"tailscale.com/tsnet"

	"github.com/sbaerlocher/tsmetrics/internal/config"
	"github.com/sbaerlocher/tsmetrics/internal/errors"
	"github.com/sbaerlocher/tsmetrics/pkg/device"
)

// HTTPClientProvider provides HTTP clients for metrics scraping.
type HTTPClientProvider interface {
	GetClient() *http.Client
}

type StandardHTTPClientProvider struct {
	timeout       time.Duration
	maxConcurrent int

	once   sync.Once
	client *http.Client
}

// NewStandardHTTPClientProvider returns a provider whose clients honour the
// configured client-metrics timeout and scrape concurrency.
func NewStandardHTTPClientProvider(timeout time.Duration, maxConcurrent int) *StandardHTTPClientProvider {
	return &StandardHTTPClientProvider{timeout: timeout, maxConcurrent: maxConcurrent}
}

// GetClient returns the provider's client, built once so keep-alive
// connections are reused across scrape cycles.
func (p *StandardHTTPClientProvider) GetClient() *http.Client {
	p.once.Do(func() {
		p.client = &http.Client{
			Timeout:       p.timeout,
			CheckRedirect: checkRedirect,
			Transport: &http.Transport{
				DialContext:         safeDialer.DialContext,
				MaxIdleConns:        p.maxConcurrent,
				IdleConnTimeout:     p.timeout,
				MaxIdleConnsPerHost: 2,
			},
		}
	})
	return p.client
}

type TsnetHTTPClientProvider struct {
	Server  *tsnet.Server
	Timeout time.Duration

	once   sync.Once
	client *http.Client
}

// GetClient returns the provider's client, built once so keep-alive
// connections are reused across scrape cycles.
func (p *TsnetHTTPClientProvider) GetClient() *http.Client {
	p.once.Do(func() {
		p.client = &http.Client{
			Timeout:       p.Timeout,
			CheckRedirect: checkRedirect,
			Transport: &http.Transport{
				DialContext: guardedDial(func(ctx context.Context, network, addr string) (net.Conn, error) {
					return p.Server.Dial(ctx, network, addr)
				}),
			},
		}
	})
	return p.client
}

type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// guardedDial wraps dial with a post-connect peer check. tsnet resolves names
// itself and dials non-tailnet IPs over the host network (tsdial.SystemDial),
// so no Control hook is available; the peer is checked before any request
// bytes are written.
func guardedDial(dial dialFunc) dialFunc {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := dial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		if err := checkBlockedRemote(conn.RemoteAddr()); err != nil {
			_ = conn.Close()
			return nil, err
		}
		return conn, nil
	}
}

// checkBlockedRemote applies checkBlockedIP to an established connection's
// peer. A peer that cannot be determined fails closed.
func checkBlockedRemote(remote net.Addr) error {
	if remote == nil {
		return fmt.Errorf("%w: connection has no remote address", errForbiddenTarget)
	}
	host, _, err := net.SplitHostPort(remote.String())
	if err != nil {
		return err
	}
	return checkBlockedIP(host)
}

var httpClientProvider HTTPClientProvider
var metricLineRE = regexp.MustCompile(`^([a-zA-Z0-9_:]+)(?:\{([^}]*)\})?\s+([-+0-9.eE]+)`)

func SetHTTPClientProvider(provider HTTPClientProvider) {
	httpClientProvider = provider
}

// ScrapeClientMetrics scrapes metrics from the provided devices using the given configuration.
// The provided context is propagated to each per-device HTTP request so that a
// parent shutdown cancels in-flight scrapes promptly.
func ScrapeClientMetrics(ctx context.Context, devices []device.Device, cfg config.Config) error {
	if cfg.TsnetScrapeTag != "" {
		slog.Info("scraping devices with tag filter", "requiredTag", cfg.TsnetScrapeTag)
	} else {
		slog.Info("scraping all devices (no tag filter)")
	}

	sem := make(chan struct{}, cfg.MaxConcurrentScrapes)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error
	var scrapedCount int

	var client *http.Client
	if httpClientProvider != nil {
		client = httpClientProvider.GetClient()
	} else {
		// ponytail: both run modes set a provider; this fallback builds a
		// throwaway client per cycle, so release its idle connections.
		client = NewStandardHTTPClientProvider(cfg.ClientMetricsTimeout, cfg.MaxConcurrentScrapes).GetClient()
		defer client.CloseIdleConnections()
	}

	for _, d := range devices {
		if cfg.TsnetScrapeTag != "" && !hasTag(d, cfg.TsnetScrapeTag) {
			slog.Debug("skipping device without required scrape tag", "device", d.Name.String(), "requiredTag", cfg.TsnetScrapeTag, "deviceTags", getDeviceTagsString(d))
			continue
		}

		slog.Debug("scraping device", "device", d.Name.String(), "tags", getDeviceTagsString(d))

		mu.Lock()
		scrapedCount++
		mu.Unlock()

		wg.Add(1)
		sem <- struct{}{}
		go func(dev device.Device) {
			defer wg.Done()
			defer func() { <-sem }()

			if err := scrapeClient(ctx, dev, client, cfg); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("device %s: %w", dev.Name.String(), err))
				mu.Unlock()

				if isTsnetStartupError(err) {
					slog.Debug("device not yet reachable via tsnet (startup)", "device", dev.Name.String(), "error", err)
				} else {
					slog.Error("scrapeClient error", "device", dev.Name.String(), "error", err)
				}
				ScrapeErrors.WithLabelValues(dev.Name.String(), errorTypeClientFetchFailed).Inc()
			}
		}(d)
	}

	wg.Wait()

	slog.Info("scraping completed", "totalDevices", len(devices), "scrapedDevices", scrapedCount, "errors", len(errs))

	if len(errs) > 0 {
		return fmt.Errorf("%d scraping errors: %v", len(errs), errs)
	}
	return nil
}

func hasTag(d device.Device, tag string) bool {
	for _, t := range d.Tags {
		if t.String() == tag {
			return true
		}
	}
	return false
}

func getDeviceTagsString(d device.Device) string {
	if len(d.Tags) == 0 {
		return "none"
	}
	var tags []string
	for _, tag := range d.Tags {
		tags = append(tags, tag.String())
	}
	return strings.Join(tags, ",")
}

func isTsnetStartupError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "backend in state NoState") ||
		strings.Contains(errStr, "tsnet: no Tailscale network") ||
		strings.Contains(errStr, "tsnet: not ready") ||
		strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "no such host")
}

func scrapeClient(ctx context.Context, dev device.Device, client *http.Client, cfg config.Config) error {
	resp, err := fetchDeviceMetrics(ctx, dev, client, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return handleHTTPError(dev, resp, buildMetricsURL(dev, cfg))
	}

	return parseMetricsResponse(dev, io.LimitReader(resp.Body, 10*1024*1024))
}

// scrapeHost returns the host used for client-metrics requests. The FQDN from
// dev.Name (MagicDNS) is preferred because tsnet's resolver has no search
// domain — a short OS-reported dev.Host fails to resolve there and causes
// scrape timeouts. dev.Host is only used as a last resort.
func scrapeHost(dev device.Device) string {
	if name := dev.Name.String(); name != "" {
		return name
	}
	return dev.Host
}

func buildMetricsURL(dev device.Device, cfg config.Config) string {
	host := net.JoinHostPort(scrapeHost(dev), cfg.ClientMetricsPort)
	u := url.URL{Scheme: "http", Host: host, Path: "/metrics"}
	return u.String()
}

func fetchDeviceMetrics(ctx context.Context, dev device.Device, client *http.Client, cfg config.Config) (*http.Response, error) {
	hostForURL := scrapeHost(dev)

	if err := validateHostname(hostForURL); err != nil {
		return nil, fmt.Errorf("invalid hostname %s: %w", hostForURL, err)
	}

	urlStr := buildMetricsURL(dev, cfg)

	// SSRF hardening: the scraper must never reach loopback, link-local, or
	// current-network addresses — even if the Tailscale API returns a
	// manipulated hostname or IP literal. RFC1918 and 100.64.0.0/10 (Tailscale
	// CGNAT) are intentionally allowed since that is where scraped devices
	// live in the homelab/tailnet.
	if err := validateDeviceMetricsURL(urlStr); err != nil {
		return nil, fmt.Errorf("invalid device metrics URL %s: %w", urlStr, err)
	}

	// http.Client.Timeout covers connect + headers + body read, so we do NOT
	// wrap with context.WithTimeout here: the deferred cancel() would fire as
	// soon as this function returns — while the caller is still reading the
	// response body. A prematurely cancelled reqCtx broke body reads as
	// "context canceled".
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if reqErr != nil {
		return nil, fmt.Errorf("failed to create request for %s: %w", urlStr, reqErr)
	}
	resp, err := client.Do(req)
	if err != nil {
		// An SSRF guard rejection is a policy decision, not a flaky network:
		// keep it distinguishable on dashboards and do not retry it.
		errorType, retryable, retryAfter := "network", true, 30*time.Second
		if stderrors.Is(err, errForbiddenTarget) {
			errorType, retryable, retryAfter = "forbidden_target", false, 0
		}
		deviceErr := errors.DeviceError{
			DeviceID:   dev.ID.String(),
			DeviceName: dev.Name.String(),
			ErrorType:  errorType,
			Underlying: err,
			Retryable:  retryable,
			RetryAfter: retryAfter,
			Timestamp:  time.Now(),
		}
		DeviceErrors.WithLabelValues(dev.ID.String(), dev.Name.String(), errorType, strconv.FormatBool(retryable)).Inc()
		return nil, deviceErr
	}
	return resp, nil
}

func handleHTTPError(dev device.Device, resp *http.Response, urlStr string) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	retryable := resp.StatusCode >= 500 || resp.StatusCode == 429
	errorType := "http_client"
	if resp.StatusCode >= 500 {
		errorType = "http_server"
	}

	deviceErr := errors.DeviceError{
		DeviceID:   dev.ID.String(),
		DeviceName: dev.Name.String(),
		ErrorType:  errorType,
		Underlying: fmt.Errorf("unexpected status %d from %s: %s", resp.StatusCode, urlStr, string(body)),
		Retryable:  retryable,
		RetryAfter: 30 * time.Second,
		Timestamp:  time.Now(),
	}
	DeviceErrors.WithLabelValues(dev.ID.String(), dev.Name.String(), errorType, fmt.Sprintf("%t", retryable)).Inc()
	return deviceErr
}

func parseMetricsResponse(dev device.Device, body io.Reader) error {
	r := bufio.NewReader(body)
	for {
		line, err := r.ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			if err := processMetricLine(dev, line); err != nil {
				continue // Skip invalid lines
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func processMetricLine(dev device.Device, line string) error {
	m := metricLineRE.FindStringSubmatch(line)
	if len(m) != 4 {
		return fmt.Errorf("invalid metric line format")
	}

	name := m[1]
	labelsStr := m[2]
	valStr := m[3]

	val, err := strconv.ParseFloat(valStr, 64)
	if err != nil {
		return err
	}

	labels := parseLabels(labelsStr)
	updateDeviceMetric(dev, name, labels, val)
	return nil
}

func updateDeviceMetric(dev device.Device, name string, labels map[string]string, val float64) {
	deviceID := dev.ID.String()
	deviceName := dev.Name.String()

	switch name {
	case "tailscaled_inbound_bytes_total":
		path := labels[labelPath]
		InboundBytes.WithLabelValues(deviceID, deviceName, path).Set(val)
	case "tailscaled_outbound_bytes_total":
		path := labels[labelPath]
		OutboundBytes.WithLabelValues(deviceID, deviceName, path).Set(val)
	case "tailscaled_inbound_packets_total":
		path := labels[labelPath]
		InboundPackets.WithLabelValues(deviceID, deviceName, path).Set(val)
	case "tailscaled_outbound_packets_total":
		path := labels[labelPath]
		OutboundPackets.WithLabelValues(deviceID, deviceName, path).Set(val)
	case "tailscaled_inbound_dropped_packets_total":
		InboundDroppedPackets.WithLabelValues(deviceID, deviceName).Set(val)
	case "tailscaled_outbound_dropped_packets_total":
		reason := labels["reason"]
		OutboundDroppedPackets.WithLabelValues(deviceID, deviceName, reason).Set(val)
	case "tailscaled_health_messages":
		typeLabel := labels["type"]
		HealthMessages.WithLabelValues(deviceID, deviceName, typeLabel).Set(val)
	case "tailscaled_advertised_routes":
		AdvertisedRoutes.WithLabelValues(deviceID, deviceName).Set(val)
	case "tailscaled_approved_routes":
		ApprovedRoutes.WithLabelValues(deviceID, deviceName).Set(val)
	}
}

func parseLabels(s string) map[string]string {
	m := map[string]string{}
	if s == "" {
		return m
	}
	scanner := bufio.NewScanner(strings.NewReader(s))
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		for i := 0; i < len(data); i++ {
			if data[i] == ',' {
				part := data[:i]
				q := bytesCount(part, '"')
				if q%2 == 0 {
					return i + 1, part, nil
				}
			}
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	})

	for scanner.Scan() {
		part := strings.TrimSpace(scanner.Text())
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.Trim(kv[1], `"`)
		m[key] = val
	}
	return m
}

func bytesCount(b []byte, c byte) int {
	cnt := 0
	for i := range b {
		if b[i] == c {
			cnt++
		}
	}
	return cnt
}

// validHostnameRe allows only characters that are valid in DNS hostnames and
// IPv6 literals (brackets and colons). Using an allowlist avoids the risk of
// missing dangerous characters in a blocklist.
var validHostnameRe = regexp.MustCompile(`^[a-zA-Z0-9.\-\[\]:]+$`)

func validateHostname(hostname string) error {
	if hostname == "" {
		return fmt.Errorf("hostname cannot be empty")
	}
	if len(hostname) > 253 {
		return fmt.Errorf("hostname exceeds maximum length of 253 characters")
	}
	if !validHostnameRe.MatchString(hostname) {
		return fmt.Errorf("hostname contains invalid characters (only letters, digits, dot, hyphen, and brackets allowed)")
	}
	return nil
}

// scraperBlockedIPNets contains IP ranges the scraper must never contact, even
// if a compromised Tailscale API response injects such a literal host.
// Notably RFC1918 and 100.64.0.0/10 (Tailscale CGNAT) are NOT blocked: those
// are the exact ranges where legitimate devices live in a homelab/tailnet.
var scraperBlockedIPNets []*net.IPNet

func init() {
	for _, cidr := range []string{
		"0.0.0.0/8",         // current network (RFC 1122) — never a valid destination
		"127.0.0.0/8",       // IPv4 loopback — prevents hitting the exporter itself
		"::1/128",           // IPv6 loopback
		"::/128",            // IPv6 unspecified — dialing [::] reaches local listeners
		"169.254.0.0/16",    // IPv4 link-local (AWS/GCP/Azure instance metadata)
		"fe80::/10",         // IPv6 link-local
		"fd00:ec2::254/128", // AWS IPv6 instance metadata (rest of fc00::/7 stays allowed)
	} {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("scraperBlockedIPNets: invalid CIDR %q: %v", cidr, err))
		}
		scraperBlockedIPNets = append(scraperBlockedIPNets, network)
	}
}

// errForbiddenTarget marks every SSRF guard rejection so callers can classify it.
var errForbiddenTarget = stderrors.New("forbidden scrape target")

// embeddedV4Prefixes are IPv6 ranges that carry an IPv4 address in their last
// 32 bits: NAT64 (RFC 6052) and the deprecated IPv4-compatible form.
var embeddedV4Prefixes = []netip.Prefix{
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("::/96"),
}

// checkRedirect refuses every redirect. /metrics has no reason to redirect,
// and re-validating hops would still let a device pivot to RFC1918 or public
// hosts that are allowed as initial targets.
func checkRedirect(req *http.Request, _ []*http.Request) error {
	return fmt.Errorf("%w: redirects are not followed for client metrics (to %s)", errForbiddenTarget, req.URL.Redacted())
}

// safeDialer rejects connections whose resolved IP is in scraperBlockedIPNets.
// Control runs after DNS resolution, so it also catches DNS names (initial or
// redirected) that resolve into a blocked range.
var safeDialer = &net.Dialer{
	Timeout:   30 * time.Second,
	KeepAlive: 30 * time.Second,
	Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		// Control always receives a resolved literal; anything else fails closed.
		if _, err := netip.ParseAddr(host); err != nil {
			return fmt.Errorf("%w: dial address %q is not an IP", errForbiddenTarget, host)
		}
		return checkBlockedIP(host)
	},
}

// checkBlockedIP returns an error if host is an IP literal in scraperBlockedIPNets.
// IPv6 zones ("fe80::1%eth0") are stripped first; net.ParseIP rejects them,
// which would otherwise let zoned link-local addresses through. IPv4 embedded
// in IPv6 (mapped, NAT64, IPv4-compatible) is checked as the IPv4 address.
func checkBlockedIP(host string) error {
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return nil
	}
	addr = addr.WithZone("").Unmap()
	for _, prefix := range embeddedV4Prefixes {
		if prefix.Contains(addr) {
			b := addr.As16()
			addr = netip.AddrFrom4([4]byte(b[12:16]))
			break
		}
	}
	ip := net.IP(addr.AsSlice())
	for _, network := range scraperBlockedIPNets { // DevSkim: ignore DS162092 - intentionally enumerates loopback/link-local for SSRF validation
		if network.Contains(ip) {
			return fmt.Errorf("%w: host %q is in a forbidden range %s", errForbiddenTarget, host, network.String())
		}
	}
	return nil
}

// validateDeviceMetricsURL parses a device metrics URL and rejects it if the
// host is a forbidden literal IP (loopback, link-local, current-net). DNS names
// pass here; safeDialer checks their resolved IP at connect time.
func validateDeviceMetricsURL(urlStr string) error {
	u, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("malformed URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q not allowed (only http/https)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL has no host")
	}
	if h := strings.ToLower(strings.TrimSuffix(host, ".")); h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return fmt.Errorf("%w: host %q is not a permitted scrape target", errForbiddenTarget, host)
	}
	return checkBlockedIP(host)
}
