package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"tailscale.com/tsnet"
)

var metricsTracker = NewDeviceMetricsTracker()

// Track if API credentials warning was already shown
var (
	apiCredentialsWarningShown bool
	apiCredentialsWarningMutex sync.Mutex
	testDevicesWarningShown    bool
	testDevicesWarningMutex    sync.Mutex
)

func fetchDevices() ([]Device, error) {
	// Parse TARGET_DEVICES env: comma-separated list of device names or hostnames to monitor
	targetDevices := []string{}
	if v := os.Getenv("TARGET_DEVICES"); v != "" {
		for _, part := range strings.Split(v, ",") {
			p := strings.TrimSpace(part)
			if p != "" {
				targetDevices = append(targetDevices, p)
			}
		}
		slog.Debug("TARGET_DEVICES specified", "devices", targetDevices)
	}

	// Backward compatibility: also check TEST_DEVICES (deprecated)
	if len(targetDevices) == 0 {
		if v := os.Getenv("TEST_DEVICES"); v != "" {
			for _, part := range strings.Split(v, ",") {
				p := strings.TrimSpace(part)
				if p != "" {
					targetDevices = append(targetDevices, p)
				}
			}
			// Show deprecation warning only once
			testDevicesWarningMutex.Lock()
			if !testDevicesWarningShown {
				slog.Warn("TEST_DEVICES is deprecated, use TARGET_DEVICES instead", "devices", targetDevices)
				testDevicesWarningShown = true
			}
			testDevicesWarningMutex.Unlock()
		}
	}

	// Use APIClient for consistent OAuth2 and HTTP handling
	clientID := os.Getenv("OAUTH_CLIENT_ID")
	clientSecret := os.Getenv("OAUTH_CLIENT_SECRET")
	tailnet := os.Getenv("TAILNET_NAME")

	if tailnet != "" && (clientID != "" || os.Getenv("OAUTH_TOKEN") != "") {
		slog.Info("attempting Tailscale API discovery")

		var apiClient *APIClient
		if clientID != "" && clientSecret != "" {
			apiClient = NewAPIClient(clientID, clientSecret, tailnet)
		} else {
			// Fallback to direct token for backwards compatibility
			apiClient = NewAPIClientWithToken(os.Getenv("OAUTH_TOKEN"), tailnet)
		}

		devices, err := apiClient.fetchDevicesFromAPI()
		if err != nil {
			slog.Error("Tailscale API failed", "error", err)
		} else {
			// Filter by TARGET_DEVICES if specified
			if len(targetDevices) > 0 {
				filtered := make([]Device, 0)
				for _, d := range devices {
					for _, td := range targetDevices {
						if strings.EqualFold(d.Name, td) || strings.EqualFold(d.ID, td) || strings.EqualFold(d.Host, td) {
							filtered = append(filtered, d)
							break
						}
					}
				}
				devices = filtered
			}
			slog.Info("Tailscale API discovery successful", "device_count", len(devices))
			return devices, nil
		}
	} else {
		// Show warning only once at startup
		apiCredentialsWarningMutex.Lock()
		if !apiCredentialsWarningShown {
			slog.Warn("Tailscale API credentials not provided", "required", "TAILNET_NAME/OAUTH_CLIENT_ID+SECRET or OAUTH_TOKEN")
			apiCredentialsWarningShown = true
		}
		apiCredentialsWarningMutex.Unlock()
	}

	// fallback: use TARGET_DEVICES as static device list if API discovery failed
	if len(targetDevices) > 0 {
		out := make([]Device, 0, len(targetDevices))
		for _, name := range targetDevices {
			out = append(out, Device{
				ID:     name,
				Name:   name,
				Host:   name,
				Tags:   []string{"exporter"},
				Online: true,
			})
		}
		slog.Info("using TARGET_DEVICES as static device list", "devices", targetDevices)
		return out, nil
	}

	// no devices available
	slog.Error("no devices to discover", "hint", "set TARGET_DEVICES or provide Tailscale API credentials")
	return []Device{}, nil
}

func updateMetrics(target string, cfg Config) error {
	start := time.Now()
	defer func() {
		scrapeDuration.WithLabelValues(target).Observe(time.Since(start).Seconds())
	}()

	devices, err := fetchDevices()
	if err != nil {
		scrapeErrors.WithLabelValues(target, "fetch_failed").Inc()
		return err
	}

	deviceCount.Set(float64(len(devices)))
	seen := map[string]struct{}{}
	for _, d := range devices {
		onlineStr := "false"
		if d.Online {
			onlineStr = "true"
		}
		deviceInfo.WithLabelValues(d.ID, d.Name, onlineStr, d.OS, d.ClientVersion).Set(1)

		// Update API-based metrics
		authValue := 0.0
		if d.Authorized {
			authValue = 1.0
		}
		deviceAuthorized.WithLabelValues(d.ID, d.Name).Set(authValue)

		// Last seen timestamp
		if !d.LastSeen.IsZero() {
			deviceLastSeen.WithLabelValues(d.ID, d.Name).Set(float64(d.LastSeen.Unix()))
		}

		// User assignment
		if d.User != "" {
			deviceUser.WithLabelValues(d.ID, d.Name, d.User).Set(1)
		}

		// Machine key expiry
		expiryValue := 0.0
		if !d.KeyExpiryDisabled && !d.Expires.IsZero() {
			expiryValue = float64(d.Expires.Unix())
		}
		deviceMachineKeyExpiry.WithLabelValues(d.ID, d.Name).Set(expiryValue)

		// Exit node status
		exitNodeValue := 0.0
		if d.IsExitNode {
			exitNodeValue = 1.0
		}
		deviceExitNode.WithLabelValues(d.ID, d.Name).Set(exitNodeValue)

		// Subnet router status (has advertised routes)
		subnetRouterValue := 0.0
		if len(d.AdvertisedRoutes) > 0 {
			subnetRouterValue = 1.0
		}
		deviceSubnetRouter.WithLabelValues(d.ID, d.Name).Set(subnetRouterValue)

		// Advertised routes
		for _, route := range d.AdvertisedRoutes {
			deviceRoutesAdvertised.WithLabelValues(d.ID, d.Name, route).Set(1)
		}

		// Enabled routes
		for _, route := range d.EnabledRoutes {
			deviceRoutesEnabled.WithLabelValues(d.ID, d.Name, route).Set(1)
		}

		seen[d.ID] = struct{}{}
	}

	// Scrape client metrics concurrently for online devices
	if err := scrapeClientMetrics(devices, cfg); err != nil {
		slog.Error("scrapeClientMetrics error", "error", err)
		scrapeErrors.WithLabelValues(target, "client_scrape_failed").Inc()
	}

	_ = seen
	return nil
}

func startBackgroundScraper(cfg Config, ctx context.Context) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		if err := updateMetrics("tailscale", cfg); err != nil {
			slog.Error("initial update failed", "error", err)
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := updateMetrics("tailscale", cfg); err != nil {
					slog.Error("updateMetrics error", "error", err)
				}
			}
		}
	}()
}

// HTTPClientProvider provides HTTP clients for scraping
type HTTPClientProvider interface {
	GetClient() *http.Client
}

// StandardHTTPClientProvider uses standard HTTP client
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

// TsnetHTTPClientProvider uses tsnet HTTP client
type TsnetHTTPClientProvider struct {
	server  *tsnet.Server
	timeout time.Duration
}

func (p *TsnetHTTPClientProvider) GetClient() *http.Client {
	return &http.Client{
		Timeout: p.timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return p.server.Dial(ctx, network, addr)
			},
		},
	}
}

// Global HTTP client provider
var httpClientProvider HTTPClientProvider

// scrapeClientMetrics concurrently scrapes device client metrics with comprehensive error handling
func scrapeClientMetrics(devices []Device, cfg Config) error {
	sem := make(chan struct{}, cfg.MaxConcurrentScrapes)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errors []error

	// Use the configured HTTP client provider (either standard or tsnet)
	var client *http.Client
	if httpClientProvider != nil {
		client = httpClientProvider.GetClient()
	} else {
		// Fallback to standard client if no provider set
		client = &http.Client{
			Timeout: cfg.ClientMetricsTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        cfg.MaxConcurrentScrapes,
				IdleConnTimeout:     cfg.ClientMetricsTimeout,
				MaxIdleConnsPerHost: 2,
			},
		}
	}

	for _, d := range devices {
		// only scrape online devices that carry the required "exporter" tag
		if !d.Online || !hasTag(d, "exporter") {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(dev Device) {
			defer wg.Done()
			defer func() { <-sem }()

			if err := scrapeClient(dev, client, cfg); err != nil {
				mu.Lock()
				errors = append(errors, fmt.Errorf("device %s: %w", dev.Name, err))
				mu.Unlock()
				slog.Error("scrapeClient error", "device", dev.Name, "error", err)
				scrapeErrors.WithLabelValues(dev.Name, "client_fetch_failed").Inc()
			}
		}(d)
	}

	wg.Wait()

	// Cleanup stale devices (not seen for 5 minutes)
	staleDevices := metricsTracker.CleanupStaleDevices(5 * time.Minute)
	if len(staleDevices) > 0 {
		slog.Info("cleaned up stale devices", "count", len(staleDevices), "devices", staleDevices)
		for _, deviceID := range staleDevices {
			cleanupDeviceMetrics(deviceID)
		}
	}

	// Return aggregated errors
	if len(errors) > 0 {
		return fmt.Errorf("%d scraping errors: %v", len(errors), errors)
	}
	return nil
}

func hasTag(d Device, tag string) bool {
	for _, t := range d.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

var metricLineRE = regexp.MustCompile(`^([a-zA-Z0-9_:]+)(?:\{([^}]*)\})?\s+([-+0-9.eE]+)`)

func parseLabels(s string) map[string]string {
	m := map[string]string{}
	if s == "" {
		return m
	}
	// split by comma but handle quoted values
	scanner := bufio.NewScanner(strings.NewReader(s))
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		for i := 0; i < len(data); i++ {
			if data[i] == ',' {
				// check quotes count to ensure comma is not inside quote
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

func scrapeClient(dev Device, client *http.Client, cfg Config) error {
	hostForURL := dev.Host
	if hostForURL == "" {
		hostForURL = dev.Name
	}

	// Input validation to prevent HTTP header injection
	if err := validateHostname(hostForURL); err != nil {
		return fmt.Errorf("invalid hostname %s: %w", hostForURL, err)
	}

	host := net.JoinHostPort(hostForURL, cfg.ClientMetricsPort)
	u := url.URL{Scheme: "http", Host: host, Path: "/metrics"}
	urlStr := u.String()

	// Mark device as active for cleanup tracking
	metricsTracker.MarkDeviceActive(dev.ID)

	resp, err := client.Get(urlStr)
	if err != nil {
		// Create structured error with retry information
		deviceErr := DeviceError{
			DeviceID:   dev.ID,
			DeviceName: dev.Name,
			ErrorType:  "network",
			Underlying: err,
			Retryable:  true,
			RetryAfter: 30 * time.Second,
			Timestamp:  time.Now(),
		}

		// Update metrics
		deviceErrors.WithLabelValues(dev.ID, dev.Name, "network", "true").Inc()

		return deviceErr
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))

		// Determine if HTTP error is retryable
		retryable := resp.StatusCode >= 500 || resp.StatusCode == 429
		errorType := "http_client"
		if resp.StatusCode >= 500 {
			errorType = "http_server"
		}

		deviceErr := DeviceError{
			DeviceID:   dev.ID,
			DeviceName: dev.Name,
			ErrorType:  errorType,
			Underlying: fmt.Errorf("unexpected status %d from %s: %s", resp.StatusCode, urlStr, string(body)),
			Retryable:  retryable,
			RetryAfter: 30 * time.Second,
			Timestamp:  time.Now(),
		}

		// Update metrics
		deviceErrors.WithLabelValues(dev.ID, dev.Name, errorType, fmt.Sprintf("%t", retryable)).Inc()

		return deviceErr
	}

	r := bufio.NewReader(resp.Body)
	for {
		line, err := r.ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			m := metricLineRE.FindStringSubmatch(line)
			if len(m) == 4 {
				name := m[1]
				labelsStr := m[2]
				valStr := m[3]
				val, err := strconv.ParseFloat(valStr, 64)
				if err != nil {
					continue
				}
				labels := parseLabels(labelsStr)
				switch name {
				case "tailscaled_inbound_bytes_total":
					path := labels["path"]
					d_inbound_bytes.WithLabelValues(dev.ID, dev.Name, path).Set(val)
				case "tailscaled_outbound_bytes_total":
					path := labels["path"]
					d_outbound_bytes.WithLabelValues(dev.ID, dev.Name, path).Set(val)
				case "tailscaled_inbound_packets_total":
					path := labels["path"]
					d_inbound_packets.WithLabelValues(dev.ID, dev.Name, path).Set(val)
				case "tailscaled_outbound_packets_total":
					path := labels["path"]
					d_outbound_packets.WithLabelValues(dev.ID, dev.Name, path).Set(val)
				case "tailscaled_inbound_dropped_packets_total":
					d_inbound_dropped_packets.WithLabelValues(dev.ID, dev.Name).Set(val)
				case "tailscaled_outbound_dropped_packets_total":
					reason := labels["reason"]
					d_outbound_dropped_packets.WithLabelValues(dev.ID, dev.Name, reason).Set(val)
				case "tailscaled_health_messages":
					typeLabel := labels["type"]
					d_health_messages.WithLabelValues(dev.ID, dev.Name, typeLabel).Set(val)
				case "tailscaled_advertised_routes":
					d_advertised_routes.WithLabelValues(dev.ID, dev.Name).Set(val)
				case "tailscaled_approved_routes":
					d_approved_routes.WithLabelValues(dev.ID, dev.Name).Set(val)
				}
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

type DeviceMetricsTracker struct {
	mu           sync.RWMutex
	knownDevices map[string]bool
	lastSeen     map[string]time.Time
}

func NewDeviceMetricsTracker() *DeviceMetricsTracker {
	return &DeviceMetricsTracker{
		knownDevices: make(map[string]bool),
		lastSeen:     make(map[string]time.Time),
	}
}

func (t *DeviceMetricsTracker) MarkDeviceActive(deviceID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.knownDevices[deviceID] = true
	t.lastSeen[deviceID] = time.Now()
}

func (t *DeviceMetricsTracker) CleanupStaleDevices(maxAge time.Duration) []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	var staleDevices []string
	now := time.Now()

	for deviceID, lastSeen := range t.lastSeen {
		if now.Sub(lastSeen) > maxAge {
			staleDevices = append(staleDevices, deviceID)
			delete(t.knownDevices, deviceID)
			delete(t.lastSeen, deviceID)
		}
	}

	return staleDevices
}

func validateHostname(host string) error {
	// Prevent HTTP header injection and other attacks
	if host == "" {
		return fmt.Errorf("hostname cannot be empty")
	}
	if strings.Contains(host, "\r") || strings.Contains(host, "\n") {
		return fmt.Errorf("invalid hostname contains newline characters: %s", host)
	}
	if strings.Contains(host, " ") || strings.Contains(host, "\t") {
		return fmt.Errorf("invalid hostname contains whitespace: %s", host)
	}
	if len(host) > 253 {
		return fmt.Errorf("hostname too long: %d characters", len(host))
	}
	return nil
}
