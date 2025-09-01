package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var metricsTracker = NewDeviceMetricsTracker()

func fetchDevices() ([]Device, error) {
	// Parse TEST_DEVICES env: comma-separated list of device names or ids to limit discovery (testing)
	testDevices := []string{}
	if v := os.Getenv("TEST_DEVICES"); v != "" {
		for _, part := range strings.Split(v, ",") {
			p := strings.TrimSpace(part)
			if p != "" {
				testDevices = append(testDevices, p)
			}
		}
		log.Printf("TEST_DEVICES specified: %v", testDevices)
	}

	// Use APIClient for consistent OAuth2 and HTTP handling
	clientID := os.Getenv("OAUTH_CLIENT_ID")
	clientSecret := os.Getenv("OAUTH_CLIENT_SECRET")
	tailnet := os.Getenv("TAILNET_NAME")

	if tailnet != "" && (clientID != "" || os.Getenv("OAUTH_TOKEN") != "") {
		log.Printf("attempting Tailscale API discovery...")

		var apiClient *APIClient
		if clientID != "" && clientSecret != "" {
			apiClient = NewAPIClient(clientID, clientSecret, tailnet)
		} else {
			// Fallback to direct token for backwards compatibility
			apiClient = NewAPIClientWithToken(os.Getenv("OAUTH_TOKEN"), tailnet)
		}

		devices, err := apiClient.fetchDevicesFromAPI()
		if err != nil {
			log.Printf("Tailscale API failed: %v", err)
		} else {
			// Filter by TEST_DEVICES if specified
			if len(testDevices) > 0 {
				filtered := make([]Device, 0)
				for _, d := range devices {
					for _, td := range testDevices {
						if strings.EqualFold(d.Name, td) || strings.EqualFold(d.ID, td) || strings.EqualFold(d.Host, td) {
							filtered = append(filtered, d)
							break
						}
					}
				}
				devices = filtered
			}
			log.Printf("Tailscale API discovery successful: %d devices found", len(devices))
			return devices, nil
		}
	} else {
		log.Printf("Tailscale API credentials not provided (TAILNET_NAME/OAUTH_CLIENT_ID+SECRET or OAUTH_TOKEN)")
	}

	// fallback: use TEST_DEVICES as mock devices if API discovery failed
	if len(testDevices) > 0 {
		out := make([]Device, 0, len(testDevices))
		for _, name := range testDevices {
			out = append(out, Device{
				ID:     name,
				Name:   name,
				Host:   name,
				Tags:   []string{"exporter"},
				Online: true,
			})
		}
		log.Printf("using TEST_DEVICES as fallback mock: %v", testDevices)
		return out, nil
	}

	// no devices available
	log.Printf("no devices to discover - set TEST_DEVICES or provide Tailscale API credentials")
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
		log.Printf("scrapeClientMetrics error: %v", err)
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
			log.Printf("initial update failed: %v", err)
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := updateMetrics("tailscale", cfg); err != nil {
					log.Printf("updateMetrics error: %v", err)
				}
			}
		}
	}()
}

// scrapeClientMetrics concurrently scrapes device client metrics with comprehensive error handling
func scrapeClientMetrics(devices []Device, cfg Config) error {
	sem := make(chan struct{}, cfg.MaxConcurrentScrapes)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errors []error

	client := &http.Client{
		Timeout: cfg.ClientMetricsTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        cfg.MaxConcurrentScrapes,
			IdleConnTimeout:     cfg.ClientMetricsTimeout,
			MaxIdleConnsPerHost: 2,
		},
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

			if err := scrapeClient(dev, client); err != nil {
				mu.Lock()
				errors = append(errors, fmt.Errorf("device %s: %w", dev.Name, err))
				mu.Unlock()
				log.Printf("scrapeClient %s error: %v", dev.Name, err)
				scrapeErrors.WithLabelValues(dev.Name, "client_fetch_failed").Inc()
			}
		}(d)
	}

	wg.Wait()

	// Cleanup stale devices (not seen for 5 minutes)
	staleDevices := metricsTracker.CleanupStaleDevices(5 * time.Minute)
	if len(staleDevices) > 0 {
		log.Printf("cleaned up %d stale devices: %v", len(staleDevices), staleDevices)
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

func scrapeClient(dev Device, client *http.Client) error {
	hostForURL := dev.Host
	if hostForURL == "" {
		hostForURL = dev.Name
	}

	// Input validation to prevent HTTP header injection
	if err := validateHostname(hostForURL); err != nil {
		return fmt.Errorf("invalid hostname %s: %w", hostForURL, err)
	}

	host := net.JoinHostPort(hostForURL, "5252")
	u := url.URL{Scheme: "http", Host: host, Path: "/metrics"}
	urlStr := u.String()

	// Mark device as active for cleanup tracking
	metricsTracker.MarkDeviceActive(dev.ID)

	resp, err := client.Get(urlStr)
	if err != nil {
		return fmt.Errorf("failed to fetch metrics from %s: %w", urlStr, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("unexpected status %d from %s: %s", resp.StatusCode, urlStr, string(body))
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
