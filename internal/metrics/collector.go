// Package metrics provides the core metrics collection functionality
// for gathering and exposing Tailscale device metrics via Prometheus.
package metrics

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sbaerlocher/tsmetrics/internal/api"
	"github.com/sbaerlocher/tsmetrics/internal/config"
	"github.com/sbaerlocher/tsmetrics/internal/types"
	"github.com/sbaerlocher/tsmetrics/pkg/device"
)

var (
	apiCredentialsWarningShown bool
	apiCredentialsWarningMutex sync.Mutex
	testDevicesWarningShown    bool
	testDevicesWarningMutex    sync.Mutex
)

// Collector manages the collection of metrics from Tailscale devices.
type Collector struct {
	cfg       config.Config
	apiClient *api.Client
	tracker   *DeviceMetricsTracker
}

// NewCollector creates a new metrics collector with the given configuration.
func NewCollector(cfg config.Config) *Collector {
	var apiClient *api.Client

	clientID := os.Getenv("OAUTH_CLIENT_ID")
	clientSecret := os.Getenv("OAUTH_CLIENT_SECRET")
	tailnet := os.Getenv("TAILNET_NAME")

	if tailnet != "" && (clientID != "" || os.Getenv("OAUTH_TOKEN") != "") {
		if clientID != "" && clientSecret != "" {
			apiClient = api.NewClient(clientID, clientSecret, tailnet)
		} else {
			apiClient = api.NewClientWithToken(os.Getenv("OAUTH_TOKEN"), tailnet)
		}
	}

	return &Collector{
		cfg:       cfg,
		apiClient: apiClient,
		tracker:   NewDeviceMetricsTracker(),
	}
}

// FetchDevices retrieves the list of devices from the Tailscale API or returns test devices.
func (c *Collector) FetchDevices() ([]device.Device, error) {
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

	if len(targetDevices) == 0 {
		if v := os.Getenv("TEST_DEVICES"); v != "" {
			for _, part := range strings.Split(v, ",") {
				p := strings.TrimSpace(part)
				if p != "" {
					targetDevices = append(targetDevices, p)
				}
			}
			testDevicesWarningMutex.Lock()
			if !testDevicesWarningShown {
				slog.Warn("TEST_DEVICES is deprecated, use TARGET_DEVICES instead", "devices", targetDevices)
				testDevicesWarningShown = true
			}
			testDevicesWarningMutex.Unlock()
		}
	}

	if c.apiClient != nil {
		slog.Info("attempting Tailscale API discovery")
		devices, err := c.apiClient.FetchDevices()
		if err != nil {
			slog.Error("Tailscale API failed", "error", err)
		} else {
			if len(targetDevices) > 0 {
				filtered := make([]device.Device, 0)
				for _, d := range devices {
					for _, td := range targetDevices {
						if strings.EqualFold(d.Name.String(), td) || strings.EqualFold(d.ID.String(), td) || strings.EqualFold(d.Host, td) {
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
		apiCredentialsWarningMutex.Lock()
		if !apiCredentialsWarningShown {
			slog.Warn("Tailscale API credentials not provided", "required", "TAILNET_NAME/OAUTH_CLIENT_ID+SECRET or OAUTH_TOKEN")
			apiCredentialsWarningShown = true
		}
		apiCredentialsWarningMutex.Unlock()
	}

	if len(targetDevices) > 0 {
		out := make([]device.Device, 0, len(targetDevices))
		for _, name := range targetDevices {
			deviceID, _ := types.NewDeviceID(name)
			deviceName, _ := types.NewDeviceName(name)
			exporterTag, _ := types.NewTagName("exporter")

			out = append(out, device.Device{
				ID:     deviceID,
				Name:   deviceName,
				Host:   name,
				Tags:   []types.TagName{exporterTag},
				Online: true,
			})
		}
		slog.Info("using TARGET_DEVICES as static device list", "devices", targetDevices)
		return out, nil
	}

	slog.Error("no devices to discover", "hint", "set TARGET_DEVICES or provide Tailscale API credentials")
	return []device.Device{}, nil
}

// UpdateMetrics updates all device metrics for the specified target.
func (c *Collector) UpdateMetrics(target string) error {
	start := time.Now()
	defer func() {
		ScrapeDuration.WithLabelValues(target).Observe(time.Since(start).Seconds())
	}()

	devices, err := c.FetchDevices()
	if err != nil {
		ScrapeErrors.WithLabelValues(target, "fetch_failed").Inc()
		return err
	}

	DeviceCount.Set(float64(len(devices)))

	// Mark all current devices as active in tracker
	for _, d := range devices {
		c.tracker.MarkDeviceActive(d.ID.String())
	}

	// Clean up stale devices and their metrics BEFORE setting new ones
	// Clean up stale devices first
	staleDevices := c.tracker.CleanupStaleDevices(5 * time.Minute)
	if len(staleDevices) > 0 {
		slog.Info("cleaning up stale device metrics", "count", len(staleDevices), "devices", staleDevices)

		// Delete metrics for stale devices to prevent duplicate entries
		for _, deviceID := range staleDevices {
			DeviceInfo.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
			DeviceOnline.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
			DeviceAuthorized.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
			DeviceLastSeen.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
			DeviceUser.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
			DeviceMachineKeyExpiry.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
			DeviceRoutesAdvertised.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
			DeviceRoutesEnabled.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
			DeviceExitNode.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
			DeviceExitNodeOption.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
			DeviceSubnetRouter.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})

			// Clean up new metrics for stale devices
			DeviceUpdateAvailable.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
			DeviceCreated.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
			DeviceExternal.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
			DeviceBlocksIncoming.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
			DeviceEphemeral.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
			DeviceMultipleConnections.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
			DeviceLatency.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
			DeviceEndpoints.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
			DeviceClientSupports.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
			DeviceTailnetLockError.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
			DevicePostureSerialNumbers.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
		}
	}

	seen := map[string]struct{}{}

	onlineCount := 0
	for _, d := range devices {
		// Set static device info (always present, no online status in labels)
		DeviceInfo.WithLabelValues(d.ID.String(), d.Name.String(), d.OS, d.ClientVersion).Set(1)

		// Set online status as numeric value (1=online, 0=offline)
		onlineValue := 0.0
		if d.Online {
			onlineValue = 1.0
			onlineCount++
		}
		DeviceOnline.WithLabelValues(d.ID.String(), d.Name.String()).Set(onlineValue)

		authValue := 0.0
		if d.Authorized {
			authValue = 1.0
		}
		DeviceAuthorized.WithLabelValues(d.ID.String(), d.Name.String()).Set(authValue)

		if !d.LastSeen.IsZero() {
			DeviceLastSeen.WithLabelValues(d.ID.String(), d.Name.String()).Set(float64(d.LastSeen.Unix()))
		}

		if d.User != "" {
			DeviceUser.WithLabelValues(d.ID.String(), d.Name.String(), d.User).Set(1)
		}

		if !d.KeyExpiryDisabled && !d.Expires.IsZero() {
			DeviceMachineKeyExpiry.WithLabelValues(d.ID.String(), d.Name.String()).Set(float64(d.Expires.Unix()))
		} else {
			DeviceMachineKeyExpiry.WithLabelValues(d.ID.String(), d.Name.String()).Set(0)
		}

		for _, route := range d.AdvertisedRoutes {
			DeviceRoutesAdvertised.WithLabelValues(d.ID.String(), d.Name.String(), route).Set(1)
		}

		for _, route := range d.EnabledRoutes {
			DeviceRoutesEnabled.WithLabelValues(d.ID.String(), d.Name.String(), route).Set(1)
		}

		exitNodeValue := 0.0
		if d.IsExitNode {
			exitNodeValue = 1.0
		}
		slog.Debug("setting exit node metric", "device", d.Name.String(), "isExitNode", d.IsExitNode, "value", exitNodeValue)
		DeviceExitNode.WithLabelValues(d.ID.String(), d.Name.String()).Set(exitNodeValue)

		exitNodeOptionValue := 0.0
		if d.ExitNodeOption {
			exitNodeOptionValue = 1.0
		}
		slog.Debug("setting exit node option metric", "device", d.Name.String(), "exitNodeOption", d.ExitNodeOption, "value", exitNodeOptionValue)
		DeviceExitNodeOption.WithLabelValues(d.ID.String(), d.Name.String()).Set(exitNodeOptionValue)

		subnetRouterValue := 0.0
		if c.hasNonExitNodeRoutes(d.AdvertisedRoutes) {
			subnetRouterValue = 1.0
		}
		DeviceSubnetRouter.WithLabelValues(d.ID.String(), d.Name.String()).Set(subnetRouterValue)

		// Set new metrics
		updateAvailableValue := 0.0
		if d.UpdateAvailable {
			updateAvailableValue = 1.0
		}
		DeviceUpdateAvailable.WithLabelValues(d.ID.String(), d.Name.String()).Set(updateAvailableValue)

		if !d.Created.IsZero() {
			DeviceCreated.WithLabelValues(d.ID.String(), d.Name.String()).Set(float64(d.Created.Unix()))
		}

		externalValue := 0.0
		if d.IsExternal {
			externalValue = 1.0
		}
		DeviceExternal.WithLabelValues(d.ID.String(), d.Name.String()).Set(externalValue)

		blocksIncomingValue := 0.0
		if d.BlocksIncomingConnections {
			blocksIncomingValue = 1.0
		}
		DeviceBlocksIncoming.WithLabelValues(d.ID.String(), d.Name.String()).Set(blocksIncomingValue)

		ephemeralValue := 0.0
		if d.IsEphemeral {
			ephemeralValue = 1.0
		}
		DeviceEphemeral.WithLabelValues(d.ID.String(), d.Name.String()).Set(ephemeralValue)

		multipleConnectionsValue := 0.0
		if d.MultipleConnections {
			multipleConnectionsValue = 1.0
		}
		DeviceMultipleConnections.WithLabelValues(d.ID.String(), d.Name.String()).Set(multipleConnectionsValue)

		tailnetLockErrorValue := 0.0
		if d.TailnetLockError != "" {
			tailnetLockErrorValue = 1.0
		}
		DeviceTailnetLockError.WithLabelValues(d.ID.String(), d.Name.String()).Set(tailnetLockErrorValue)

		// Set connectivity metrics
		if d.ClientConnectivity != nil {
			// Set endpoint count
			DeviceEndpoints.WithLabelValues(d.ID.String(), d.Name.String()).Set(float64(len(d.ClientConnectivity.Endpoints)))

			// Set latency metrics
			for region, latencyInfo := range d.ClientConnectivity.Latency {
				preferredStr := "false"
				if latencyInfo.Preferred {
					preferredStr = "true"
				}
				DeviceLatency.WithLabelValues(d.ID.String(), d.Name.String(), region, preferredStr).Set(latencyInfo.LatencyMs)
			}

			// Set client support metrics
			supports := d.ClientConnectivity.ClientSupports
			DeviceClientSupports.WithLabelValues(d.ID.String(), d.Name.String(), "hairpinning").Set(boolToFloat64(supports.HairPinning))
			DeviceClientSupports.WithLabelValues(d.ID.String(), d.Name.String(), "ipv6").Set(boolToFloat64(supports.IPv6))
			DeviceClientSupports.WithLabelValues(d.ID.String(), d.Name.String(), "pcp").Set(boolToFloat64(supports.PCP))
			DeviceClientSupports.WithLabelValues(d.ID.String(), d.Name.String(), "pmp").Set(boolToFloat64(supports.PMP))
			DeviceClientSupports.WithLabelValues(d.ID.String(), d.Name.String(), "udp").Set(boolToFloat64(supports.UDP))
			DeviceClientSupports.WithLabelValues(d.ID.String(), d.Name.String(), "upnp").Set(boolToFloat64(supports.UPnP))
		} else {
			DeviceEndpoints.WithLabelValues(d.ID.String(), d.Name.String()).Set(0)
		}

		// Set posture identity metrics
		if d.PostureIdentity != nil {
			DevicePostureSerialNumbers.WithLabelValues(d.ID.String(), d.Name.String()).Set(float64(len(d.PostureIdentity.SerialNumbers)))
		} else {
			DevicePostureSerialNumbers.WithLabelValues(d.ID.String(), d.Name.String()).Set(0)
		}

		seen[d.ID.String()] = struct{}{}
	}

	OnlineDevicesCount.Set(float64(onlineCount))

	if err := ScrapeClientMetrics(devices, c.cfg); err != nil {
		if errCount := CountTsnetStartupErrors(err); errCount > 0 {
			slog.Debug("device scraping pending tsnet startup", "tsnet_startup_errors", errCount, "details", err)
		} else {
			slog.Error("scrapeClientMetrics error", "error", err)
		}
		ScrapeErrors.WithLabelValues(target, "client_scrape_failed").Inc()
	}

	c.tracker.CleanupRemovedDevices(seen)
	LastScrapeTime.Set(float64(time.Now().Unix()))
	return nil
}

// CountTsnetStartupErrors counts common tsnet startup errors in error messages
func CountTsnetStartupErrors(err error) int {
	if err == nil {
		return 0
	}
	errStr := err.Error()
	count := 0
	if strings.Contains(errStr, "backend in state NoState") {
		count += strings.Count(errStr, "backend in state NoState")
	}
	if strings.Contains(errStr, "connection refused") {
		count += strings.Count(errStr, "connection refused")
	}
	if strings.Contains(errStr, "no such host") {
		count += strings.Count(errStr, "no such host")
	}
	return count
}

// CollectDeviceMetrics implements MetricCollector interface
func (c *Collector) CollectDeviceMetrics(ctx context.Context, device device.Device) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Implementation details would depend on the specific metric collection logic
	// For now, this is a placeholder that would integrate with the existing collection code
	slog.Debug("collecting metrics for device", "device_id", device.ID.String(), "device_name", device.Name.String())

	// In a real implementation, this would call the appropriate scraping functions
	// and handle the metric collection logic for a single device
	return nil
}

// boolToFloat64 converts a boolean to float64 for Prometheus metrics.
func boolToFloat64(b bool) float64 {
	if b {
		return 1.0
	}
	return 0.0
}

// hasNonExitNodeRoutes checks if there are any routes that are not exit node routes (0.0.0.0/0 or ::/0)
func (c *Collector) hasNonExitNodeRoutes(routes []string) bool {
	for _, route := range routes {
		if route != "0.0.0.0/0" && route != "::/0" {
			return true
		}
	}
	return false
}
