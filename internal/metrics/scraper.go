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
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"tailscale.com/tsnet"

	"github.com/sbaerlocher/tsmetrics/internal/config"
	tsmerrors "github.com/sbaerlocher/tsmetrics/internal/errors"
	"github.com/sbaerlocher/tsmetrics/pkg/device"
)

// HTTPClientProvider provides HTTP clients for metrics scraping.
type HTTPClientProvider interface {
	GetClient() *http.Client
}

type StandardHTTPClientProvider struct {
	timeout       time.Duration
	maxConcurrent int
}

func (p *StandardHTTPClientProvider) GetClient() *http.Client {
	return &http.Client{
		Timeout: p.timeout,
		Transport: &http.Transport{
			MaxIdleConns:        p.maxConcurrent,
			IdleConnTimeout:     p.timeout,
			MaxIdleConnsPerHost: 2,
		},
	}
}

type TsnetHTTPClientProvider struct {
	Server  *tsnet.Server
	Timeout time.Duration
}

func (p *TsnetHTTPClientProvider) GetClient() *http.Client {
	return &http.Client{
		Timeout: p.Timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return p.Server.Dial(ctx, network, addr)
			},
		},
	}
}

var httpClientProvider HTTPClientProvider
var metricLineRE = regexp.MustCompile(`^([a-zA-Z0-9_:]+)(?:\{([^}]*)\})?\s+([-+0-9.eE]+)`)

type peerEndpointState struct {
	available  bool
	retryAfter time.Time
}

type peerScrapeJob struct {
	device device.Device
	key    string
	state  peerEndpointState
}

type peerScrapeResult struct {
	job peerScrapeJob
	err error
}

func SetHTTPClientProvider(provider HTTPClientProvider) {
	httpClientProvider = provider
}

// ScrapeClientMetrics scrapes metrics from the provided devices using the given configuration.
// The provided context is propagated to each per-device HTTP request so that a
// parent shutdown cancels in-flight scrapes promptly.
func ScrapeClientMetrics(ctx context.Context, devices []device.Device, cfg config.Config) error {
	if cfg.PeerRecheckInterval <= 0 {
		cfg.PeerRecheckInterval = config.DefaultPeerRecheckInterval
	}
	collector := &Collector{
		cfg:           cfg,
		peerEndpoints: make(map[string]peerEndpointState),
	}
	return collector.scrapeClientMetricsAt(ctx, devices, time.Now())
}

func (c *Collector) scrapeClientMetricsAt(ctx context.Context, devices []device.Device, now time.Time) error {
	return c.scrapeClientMetricsWithClientAt(ctx, devices, newMetricsHTTPClient(c.cfg), now)
}

func newMetricsHTTPClient(cfg config.Config) *http.Client {
	if httpClientProvider != nil {
		return httpClientProvider.GetClient()
	}
	return &http.Client{
		Timeout: cfg.ClientMetricsTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        cfg.MaxConcurrentScrapes,
			IdleConnTimeout:     cfg.ClientMetricsTimeout,
			MaxIdleConnsPerHost: 2,
		},
	}
}

func (c *Collector) scrapeClientMetricsWithClientAt(
	ctx context.Context,
	devices []device.Device,
	client *http.Client,
	now time.Time,
) error {
	c.peerEndpointsMu.Lock()
	defer c.peerEndpointsMu.Unlock()

	nextEndpoints := make(map[string]peerEndpointState, len(devices))
	jobs := make([]peerScrapeJob, 0, len(devices))

	for _, dev := range devices {
		if scrapeHost(dev) == "" {
			c.cleanupAvailablePeer(dev)
			continue
		}
		if c.cfg.TsnetScrapeTag != "" && !hasTag(dev, c.cfg.TsnetScrapeTag) {
			c.cleanupAvailablePeer(dev)
			continue
		}

		key := peerCacheKey(dev)
		state, found := c.peerEndpoints[key]
		if found {
			nextEndpoints[key] = state
		}
		if found && !state.available && now.Before(state.retryAfter) {
			continue
		}

		jobs = append(jobs, peerScrapeJob{
			device: dev,
			key:    key,
			state:  state,
		})
	}

	sem := make(chan struct{}, c.cfg.MaxConcurrentScrapes)
	results := make(chan peerScrapeResult, len(jobs))
	var wg sync.WaitGroup

schedule:
	for _, job := range jobs {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break schedule
		}

		wg.Add(1)
		go func(job peerScrapeJob) {
			defer wg.Done()
			defer func() { <-sem }()
			results <- peerScrapeResult{
				job: job,
				err: scrapeClient(ctx, job.device, client, c.cfg),
			}
		}(job)
	}

	wg.Wait()
	close(results)

	failures := 0
	for result := range results {
		if result.err == nil {
			nextEndpoints[result.job.key] = peerEndpointState{available: true}
			continue
		}

		if ctxErr := ctx.Err(); ctxErr != nil && stderrors.Is(result.err, ctxErr) {
			continue
		}

		failures++
		nextEndpoints[result.job.key] = peerEndpointState{
			retryAfter: now.Add(c.cfg.PeerRecheckInterval),
		}

		if !result.job.state.available {
			slog.Debug("peer metrics endpoint not available",
				"node", result.job.device.Name.String(),
				"retry_after", now.Add(c.cfg.PeerRecheckInterval),
				"error", result.err)
			continue
		}

		CleanupClientMetrics(result.job.device.ID.String())
		if isConnectionRefused(result.err) {
			slog.Debug("peer metrics endpoint not available",
				"node", result.job.device.Name.String(),
				"retry_after", now.Add(c.cfg.PeerRecheckInterval),
				"error", result.err)
			continue
		}

		slog.Warn("peer metrics endpoint became unavailable",
			"node", result.job.device.Name.String(),
			"retry_after", now.Add(c.cfg.PeerRecheckInterval),
			"error", result.err)
		recordPeerScrapeError(result.job.device, result.err)
	}

	c.peerEndpoints = nextEndpoints
	slog.Debug("peer metrics scrape complete",
		"total_devices", len(devices),
		"scheduled_peers", len(jobs),
		"failed_peers", failures)

	return ctx.Err()
}

func (c *Collector) cleanupAvailablePeer(dev device.Device) {
	if state, found := c.peerEndpoints[peerCacheKey(dev)]; found && state.available {
		CleanupClientMetrics(dev.ID.String())
	}
}

func peerCacheKey(dev device.Device) string {
	if id := dev.ID.String(); id != "" {
		return id
	}
	if name := dev.Name.String(); name != "" {
		return name
	}
	return dev.Host
}

func isConnectionRefused(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "connection refused")
}

func recordPeerScrapeError(dev device.Device, err error) {
	errorType := "unknown"
	retryable := false
	var deviceErr tsmerrors.DeviceError
	if stderrors.As(err, &deviceErr) {
		errorType = deviceErr.ErrorType
		retryable = deviceErr.Retryable
	}

	DeviceErrors.WithLabelValues(
		dev.ID.String(),
		dev.Name.String(),
		errorType,
		strconv.FormatBool(retryable),
	).Inc()
	ScrapeErrors.WithLabelValues(dev.Name.String(), "client_fetch_failed").Inc()
}

func hasTag(d device.Device, tag string) bool {
	for _, t := range d.Tags {
		if t.String() == tag {
			return true
		}
	}
	return false
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
		deviceErr := tsmerrors.DeviceError{
			DeviceID:   dev.ID.String(),
			DeviceName: dev.Name.String(),
			ErrorType:  "network",
			Underlying: err,
			Retryable:  true,
			RetryAfter: 30 * time.Second,
			Timestamp:  time.Now(),
		}
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

	deviceErr := tsmerrors.DeviceError{
		DeviceID:   dev.ID.String(),
		DeviceName: dev.Name.String(),
		ErrorType:  errorType,
		Underlying: fmt.Errorf("unexpected status %d from %s: %s", resp.StatusCode, urlStr, string(body)),
		Retryable:  retryable,
		RetryAfter: 30 * time.Second,
		Timestamp:  time.Now(),
	}
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
		path := labels["path"]
		InboundBytes.WithLabelValues(deviceID, deviceName, path).Set(val)
	case "tailscaled_outbound_bytes_total":
		path := labels["path"]
		OutboundBytes.WithLabelValues(deviceID, deviceName, path).Set(val)
	case "tailscaled_inbound_packets_total":
		path := labels["path"]
		InboundPackets.WithLabelValues(deviceID, deviceName, path).Set(val)
	case "tailscaled_outbound_packets_total":
		path := labels["path"]
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
		"0.0.0.0/8",      // current network (RFC 1122) — never a valid destination
		"127.0.0.0/8",    // IPv4 loopback — prevents hitting the exporter itself
		"::1/128",        // IPv6 loopback
		"169.254.0.0/16", // IPv4 link-local (AWS/GCP/Azure instance metadata)
		"fe80::/10",      // IPv6 link-local
	} {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("scraperBlockedIPNets: invalid CIDR %q: %v", cidr, err))
		}
		scraperBlockedIPNets = append(scraperBlockedIPNets, network)
	}
}

// validateDeviceMetricsURL parses a device metrics URL and rejects it if the
// host resolves to a forbidden literal IP (loopback, link-local, current-net).
// DNS names are accepted; DNS rebinding is out of scope because device hostnames
// originate from the authenticated Tailscale API.
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
	if strings.EqualFold(host, "localhost") {
		return fmt.Errorf("host %q is not a permitted scrape target", host)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// DNS name — trust the Tailscale API as the source of truth.
		return nil
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	for _, network := range scraperBlockedIPNets { // DevSkim: ignore DS162092 - intentionally enumerates loopback/link-local for SSRF validation
		if network.Contains(ip) {
			return fmt.Errorf("host %q is in a forbidden range %s", host, network.String())
		}
	}
	return nil
}
