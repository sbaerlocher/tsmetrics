package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/oauth2/clientcredentials"
	"tailscale.com/tsnet"
)

var (
	version   = "dev"
	buildTime = "unknown"
)

var (
	scrapeDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "tsmetrics_scrape_duration_seconds",
			Help: "Time spent scraping target",
		},
		[]string{"target"},
	)

	scrapeErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tsmetrics_scrape_errors_total",
			Help: "Scrape errors by target",
		},
		[]string{"target", "error_type"},
	)

	deviceCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "tailscale_device_count",
			Help: "Number of devices discovered",
		},
	)

	deviceInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_info",
			Help: "Static info about devices (value=1)",
		},
		[]string{"device_id", "device_name", "online"},
	)

	// Client metrics exposed by devices (using Gauge for absolute values from remote counters)
	// We use Gauge because we're reflecting the current state of remote counters, not incrementing locally
	d_inbound_bytes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_inbound_bytes_total", Help: "Current inbound bytes from device (reflects remote counter)"},
		[]string{"device_id", "device_name", "path"},
	)
	d_outbound_bytes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_outbound_bytes_total", Help: "Current outbound bytes from device (reflects remote counter)"},
		[]string{"device_id", "device_name", "path"},
	)
	d_inbound_packets = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_inbound_packets_total", Help: "Current inbound packets from device (reflects remote counter)"},
		[]string{"device_id", "device_name", "path"},
	)
	d_outbound_packets = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_outbound_packets_total", Help: "Current outbound packets from device (reflects remote counter)"},
		[]string{"device_id", "device_name", "path"},
	)
	d_inbound_dropped_packets = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_inbound_dropped_packets_total", Help: "Current dropped inbound packets from device (reflects remote counter)"},
		[]string{"device_id", "device_name"},
	)
	d_outbound_dropped_packets = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_outbound_dropped_packets_total", Help: "Current dropped outbound packets from device (reflects remote counter)"},
		[]string{"device_id", "device_name", "reason"},
	)
	d_health_messages = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_health_messages", Help: "Health message count from device"},
		[]string{"device_id", "device_name", "type"},
	)
	d_advertised_routes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_advertised_routes", Help: "Number of advertised network routes by device"},
		[]string{"device_id", "device_name"},
	)
	d_approved_routes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_approved_routes", Help: "Number of approved network routes by device"},
		[]string{"device_id", "device_name"},
	)
)

type Device struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Host   string   `json:"host"`
	Tags   []string `json:"tags"`
	Online bool     `json:"online"`
}

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
		deviceInfo.WithLabelValues(d.ID, d.Name, onlineStr).Set(1)
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

func setupTsnetStateDir(dir string) string {
	if dir == "" {
		dir = "/tmp/tsnet-tsmetrics"
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Fatalf("failed to create state directory: %v", err)
	}
	log.Printf("using tsnet state directory: %s", dir)
	return dir
}

type Config struct {
	UseTsnet             bool
	TsnetHostname        string
	TsnetStateDir        string
	Port                 string
	OAuthClientID        string
	OAuthSecret          string
	TailnetName          string
	ClientMetricsTimeout time.Duration
	MaxConcurrentScrapes int
	TsnetTags            []string
	RequireExporterTag   bool
}

func loadConfig() Config {
	cfg := Config{}
	if strings.ToLower(os.Getenv("USE_TSNET")) == "true" {
		cfg.UseTsnet = true
	}
	cfg.TsnetHostname = os.Getenv("TSNET_HOSTNAME")
	cfg.TsnetStateDir = os.Getenv("TSNET_STATE_DIR")
	cfg.Port = os.Getenv("PORT")
	if cfg.Port == "" {
		cfg.Port = "9100"
	}
	cfg.OAuthClientID = os.Getenv("OAUTH_CLIENT_ID")
	cfg.OAuthSecret = os.Getenv("OAUTH_CLIENT_SECRET")
	cfg.TailnetName = os.Getenv("TAILNET_NAME")

	// CLIENT_METRICS_TIMEOUT as duration string, fallback to 10s
	if v := os.Getenv("CLIENT_METRICS_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.ClientMetricsTimeout = d
		} else if sec, err := strconv.Atoi(v); err == nil {
			cfg.ClientMetricsTimeout = time.Duration(sec) * time.Second
		}
	}
	if cfg.ClientMetricsTimeout == 0 {
		cfg.ClientMetricsTimeout = 10 * time.Second
	}

	// MAX_CONCURRENT_SCRAPES integer, fallback to 10
	cfg.MaxConcurrentScrapes = 10
	if v := os.Getenv("MAX_CONCURRENT_SCRAPES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxConcurrentScrapes = n
		}
	}

	// TSNET_TAGS: comma-separated list of tags assigned to this tsnet device
	if v := os.Getenv("TSNET_TAGS"); v != "" {
		parts := strings.Split(v, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		cfg.TsnetTags = parts
	}

	// REQUIRE_EXPORTER_TAG: if "true", enforce that this tsnet device must carry the "exporter" tag
	if strings.ToLower(os.Getenv("REQUIRE_EXPORTER_TAG")) == "true" {
		cfg.RequireExporterTag = true
	}

	return cfg
}

func setupRoutes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		info := map[string]interface{}{
			"version":    version,
			"build_time": buildTime,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	})
	return mux
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

func runStandalone(cfg Config, ctx context.Context) error {
	env := strings.ToLower(os.Getenv("ENV"))
	host := "127.0.0.1"
	if env == "production" || env == "prod" {
		host = "0.0.0.0"
	}
	addr := fmt.Sprintf("%s:%s", host, cfg.Port)
	mux := setupRoutes()
	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	startBackgroundScraper(cfg, ctx)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func runWithTsnet(cfg Config, ctx context.Context) error {
	stateDir := setupTsnetStateDir(cfg.TsnetStateDir)
	server := &tsnet.Server{
		Hostname: cfg.TsnetHostname,
		Dir:      stateDir,
	}
	defer server.Close()

	listener, err := server.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		return fmt.Errorf("tsnet listen failed: %w", err)
	}

	mux := setupRoutes()
	// HTTP server that will be served over tsnet listener
	tsHTTPServer := &http.Server{Handler: mux}
	// Local HTTP server bound to 127.0.0.1 so localhost requests work as well
	localAddr := fmt.Sprintf("127.0.0.1:%s", cfg.Port)
	localHTTPServer := &http.Server{Addr: localAddr, Handler: mux}

	startBackgroundScraper(cfg, ctx)

	errCh := make(chan error, 2)

	// serve over tsnet listener
	go func() {
		if err := tsHTTPServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("tsnet http serve failed: %w", err)
			return
		}
		errCh <- nil
	}()

	// serve on local loopback
	go func() {
		if err := localHTTPServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("local http serve failed: %w", err)
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = tsHTTPServer.Shutdown(shutdownCtx)
		_ = localHTTPServer.Shutdown(shutdownCtx)
		return nil
	case e := <-errCh:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = tsHTTPServer.Shutdown(shutdownCtx)
		_ = localHTTPServer.Shutdown(shutdownCtx)
		if e != nil {
			return e
		}
		return nil
	}
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
	if strings.Contains(host, "\r") || strings.Contains(host, "\n") {
		return fmt.Errorf("invalid hostname contains newline characters: %s", host)
	}
	if strings.Contains(host, " ") {
		return fmt.Errorf("invalid hostname contains spaces: %s", host)
	}
	if len(host) > 253 {
		return fmt.Errorf("hostname too long: %d characters", len(host))
	}
	return nil
}

var metricsTracker = NewDeviceMetricsTracker()

type APIClient struct {
	httpClient  *http.Client
	oauthConfig *clientcredentials.Config
	baseURL     string
}

func NewAPIClient(clientID, clientSecret, tailnet string) *APIClient {
	var httpClient *http.Client

	if clientID != "" && clientSecret != "" {
		config := &clientcredentials.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			TokenURL:     "https://api.tailscale.com/api/v2/oauth/token",
		}
		httpClient = config.Client(context.Background())
	} else {
		httpClient = &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     30 * time.Second,
				DisableCompression:  false,
				MaxIdleConnsPerHost: 2,
			},
		}
	}

	return &APIClient{
		httpClient:  httpClient,
		oauthConfig: nil,
		baseURL:     fmt.Sprintf("https://api.tailscale.com/api/v2/tailnet/%s", tailnet),
	}
}

func NewAPIClientWithToken(token, tailnet string) *APIClient {
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			DisableCompression:  false,
			MaxIdleConnsPerHost: 2,
		},
	}

	// Wrap client to add Bearer token
	transport := httpClient.Transport
	httpClient.Transport = &tokenTransport{
		token:     token,
		transport: transport,
	}

	return &APIClient{
		httpClient:  httpClient,
		oauthConfig: nil,
		baseURL:     fmt.Sprintf("https://api.tailscale.com/api/v2/tailnet/%s", tailnet),
	}
}

type tokenTransport struct {
	token     string
	transport http.RoundTripper
}

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.transport.RoundTrip(req)
}

func (c *APIClient) fetchDevicesFromAPI() ([]Device, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("no tailnet configured")
	}

	apiURL := fmt.Sprintf("%s/devices", c.baseURL)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Devices []struct {
			ID        string   `json:"id"`
			Name      string   `json:"name"`
			Hostname  string   `json:"hostname"`
			Addresses []string `json:"addresses"`
			Online    bool     `json:"online"`
			Tags      []string `json:"tags"`
		} `json:"devices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	devices := make([]Device, 0, len(result.Devices))
	for _, d := range result.Devices {
		host := d.Hostname
		if host == "" && len(d.Addresses) > 0 {
			host = d.Addresses[0]
		}
		// Input validation
		if d.ID == "" || d.Name == "" {
			log.Printf("skipping device with missing ID or Name: %+v", d)
			continue
		}
		devices = append(devices, Device{
			ID:     d.ID,
			Name:   d.Name,
			Host:   host,
			Tags:   d.Tags,
			Online: d.Online,
		})
	}

	return devices, nil
}

func cleanupDeviceMetrics(deviceID string) {
	// Clean up all metric series for the given device
	// This prevents memory leaks when devices go offline permanently

	// For each metric vector, delete all series with this device_id
	d_inbound_bytes.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	d_outbound_bytes.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	d_inbound_packets.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	d_outbound_packets.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	d_inbound_dropped_packets.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	d_outbound_dropped_packets.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	d_health_messages.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	d_advertised_routes.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	d_approved_routes.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	deviceInfo.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
}

func main() {
	cfg := loadConfig()

	if v := os.Getenv("VERSION"); v != "" {
		version = v
	}
	if bt := os.Getenv("BUILD_TIME"); bt != "" {
		buildTime = bt
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	if cfg.UseTsnet {
		if cfg.TsnetHostname == "" {
			cfg.TsnetHostname = "tsmetrics"
		}
		// Enforce exporter tag requirement if requested
		if cfg.RequireExporterTag {
			has := false
			for _, t := range cfg.TsnetTags {
				if t == "exporter" {
					has = true
					break
				}
			}
			if !has {
				log.Fatalf("REQUIRE_EXPORTER_TAG is set but TSNET_TAGS does not include 'exporter'. Set TSNET_TAGS=exporter or add the exporter tag via auth key/console.")
			}
		}
		log.Printf("starting with tsnet hostname=%s port=%s", cfg.TsnetHostname, cfg.Port)
		err = runWithTsnet(cfg, ctx)
	} else {
		log.Printf("starting standalone on port=%s", cfg.Port)
		err = runStandalone(cfg, ctx)
	}

	if err != nil {
		log.Printf("shutdown with error: %v", err)
	} else {
		log.Printf("shutdown complete")
	}
}
