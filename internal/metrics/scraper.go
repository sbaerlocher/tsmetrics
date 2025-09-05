// Package metrics provides client metrics scraping functionality.
package metrics

import (
	"bufio"
	"context"
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

func SetHTTPClientProvider(provider HTTPClientProvider) {
	httpClientProvider = provider
}

// ScrapeClientMetrics scrapes metrics from the provided devices using the given configuration.
func ScrapeClientMetrics(devices []device.Device, cfg config.Config) error {
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

			if err := scrapeClient(dev, client, cfg); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("device %s: %w", dev.Name.String(), err))
				mu.Unlock()

				if isTsnetStartupError(err) {
					slog.Debug("device not yet reachable via tsnet (startup)", "device", dev.Name.String(), "error", err)
				} else {
					slog.Error("scrapeClient error", "device", dev.Name.String(), "error", err)
				}
				ScrapeErrors.WithLabelValues(dev.Name.String(), "client_fetch_failed").Inc()
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

func scrapeClient(dev device.Device, client *http.Client, cfg config.Config) error {
	resp, err := fetchDeviceMetrics(dev, client, cfg)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return handleHTTPError(dev, resp, buildMetricsURL(dev, cfg))
	}

	return parseMetricsResponse(dev, resp.Body)
}

func buildMetricsURL(dev device.Device, cfg config.Config) string {
	hostForURL := dev.Host
	if hostForURL == "" {
		hostForURL = dev.Name.String()
	}
	host := net.JoinHostPort(hostForURL, cfg.ClientMetricsPort)
	u := url.URL{Scheme: "http", Host: host, Path: "/metrics"}
	return u.String()
}

func fetchDeviceMetrics(dev device.Device, client *http.Client, cfg config.Config) (*http.Response, error) {
	hostForURL := dev.Host
	if hostForURL == "" {
		hostForURL = dev.Name.String()
	}

	if err := validateHostname(hostForURL); err != nil {
		return nil, fmt.Errorf("invalid hostname %s: %w", hostForURL, err)
	}

	urlStr := buildMetricsURL(dev, cfg)
	resp, err := client.Get(urlStr)
	if err != nil {
		deviceErr := errors.DeviceError{
			DeviceID:   dev.ID.String(),
			DeviceName: dev.Name.String(),
			ErrorType:  "network",
			Underlying: err,
			Retryable:  true,
			RetryAfter: 30 * time.Second,
			Timestamp:  time.Now(),
		}
		DeviceErrors.WithLabelValues(dev.ID.String(), dev.Name.String(), "network", "true").Inc()
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

func validateHostname(hostname string) error {
	if hostname == "" {
		return fmt.Errorf("hostname cannot be empty")
	}
	if strings.Contains(hostname, "\n") || strings.Contains(hostname, "\r") {
		return fmt.Errorf("hostname contains invalid characters")
	}
	return nil
}
