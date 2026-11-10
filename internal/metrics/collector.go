// Package metrics provides the core metrics collection functionality
// for gathering and exposing Tailscale device metrics via Prometheus.
package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sbaerlocher/tsmetrics/internal/api"
	"github.com/sbaerlocher/tsmetrics/internal/config"
	"github.com/sbaerlocher/tsmetrics/internal/types"
	"github.com/sbaerlocher/tsmetrics/pkg/device"
)

const maxLabelLength = 128

// truncateLabel limits a Prometheus label value to maxLabelLength to prevent
// cardinality bombs from compromised API data.
func truncateLabel(s string) string {
	if len(s) <= maxLabelLength {
		return s
	}
	r := []rune(s)
	if len(r) <= maxLabelLength {
		return s
	}
	return string(r[:maxLabelLength])
}

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

	// labelState holds each device's label fingerprint from the previous
	// cycle. Only runCycle calls UpdateMetrics, sequentially, so no lock.
	labelState map[string]deviceLabelState
}

// deviceLabelState is what a device's series were labelled with last cycle.
type deviceLabelState struct {
	name        string
	fingerprint string
}

// deviceLabelFingerprint covers every input that decides which label sets a
// device's API series carry. A change means some old series would linger, so
// they must be deleted before the new ones are set. Values that only feed
// gauge values (latency, timestamps) and the order of routes are left out.
func deviceLabelFingerprint(d device.Device) string {
	parts := []string{
		d.Name.String(),
		d.OS,
		d.ClientVersion,
		d.User,
		sortedJoin(d.AdvertisedRoutes),
		sortedJoin(d.EnabledRoutes),
		strconv.FormatBool(d.LastSeen.IsZero()),
		strconv.FormatBool(d.Created.IsZero()),
		strconv.FormatBool(d.ClientConnectivity != nil),
	}
	if d.ClientConnectivity != nil {
		regions := make([]string, 0, len(d.ClientConnectivity.Latency))
		for region, info := range d.ClientConnectivity.Latency {
			regions = append(regions, region+"="+strconv.FormatBool(info.Preferred))
		}
		parts = append(parts, sortedJoin(regions))
	}
	return strings.Join(parts, "\x00")
}

// dropRenamedDeviceSeries removes the client and error series that still carry
// a renamed device's previous name. Call it after the client scrape has written
// the new-name series.
func dropRenamedDeviceSeries(renamed map[string]string) {
	for id, oldName := range renamed {
		old := prometheus.Labels{labelDeviceID: id, labelDeviceName: oldName}
		for _, vec := range clientDeviceVecs() {
			vec.DeletePartialMatch(old)
		}
		DeviceErrors.DeletePartialMatch(old)
	}
}

func sortedJoin(values []string) string {
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return strings.Join(sorted, "\x1f")
}

// APIClient returns the underlying Tailscale API client, or nil if the
// collector was initialized without OAuth credentials. Callers (e.g. the
// HTTP health handler) use this to avoid re-running the OAuth flow per
// request.
func (c *Collector) APIClient() *api.Client {
	return c.apiClient
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

// FetchDevices retrieves the list of devices from the Tailscale API or returns
// test devices. The context is propagated to the API client so a shutdown or
// per-cycle timeout can abort in-flight retries on the devices + routes calls.
func (c *Collector) FetchDevices(ctx context.Context) ([]device.Device, error) {
	targetDevices := []string{}
	if v := os.Getenv("TARGET_DEVICES"); v != "" {
		for _, part := range strings.Split(v, ",") {
			p := strings.TrimSpace(part)
			if p != "" {
				targetDevices = append(targetDevices, p)
			}
		}
		slog.Debug("TARGET_DEVICES specified", "count", len(targetDevices))
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
				slog.Warn("TEST_DEVICES is deprecated, use TARGET_DEVICES instead", "count", len(targetDevices))
				testDevicesWarningShown = true
			}
			testDevicesWarningMutex.Unlock()
		}
	}

	if c.apiClient != nil {
		slog.Info("attempting Tailscale API discovery")
		devices, err := c.apiClient.FetchDevices(ctx)
		if err != nil {
			// Keep the last cycle's series: treating a failed call as "no devices"
			// would make UpdateMetrics delete every device's metrics.
			return nil, fmt.Errorf("tailscale API discovery: %w", err)
		}
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

	apiCredentialsWarningMutex.Lock()
	if !apiCredentialsWarningShown {
		slog.Warn("Tailscale API credentials not provided", "required", "TAILNET_NAME/OAUTH_CLIENT_ID+SECRET or OAUTH_TOKEN")
		apiCredentialsWarningShown = true
	}
	apiCredentialsWarningMutex.Unlock()

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
		slog.Info("using TARGET_DEVICES as static device list", "count", len(targetDevices))
		return out, nil
	}

	slog.Error("no devices to discover", "hint", "set TARGET_DEVICES or provide Tailscale API credentials")
	return []device.Device{}, nil
}

// UpdateMetrics updates all device metrics for the specified target.
// The context is propagated to concurrent device scrapes so that a parent
// cancellation (shutdown, per-cycle timeout) stops in-flight requests.
func (c *Collector) UpdateMetrics(ctx context.Context, target string) error {
	start := time.Now()
	defer func() {
		ScrapeDuration.WithLabelValues(target).Observe(time.Since(start).Seconds())
	}()

	devices, err := c.FetchDevices(ctx)
	if err != nil {
		ScrapeErrors.WithLabelValues(target, "fetch_failed").Inc()
		return err
	}

	DeviceCount.Set(float64(len(devices)))

	seen, renamed := c.updateAPIMetrics(devices)

	if err := ScrapeClientMetrics(ctx, devices, c.cfg); err != nil {
		if errCount := CountTsnetStartupErrors(err); errCount > 0 {
			slog.Debug("device scraping pending tsnet startup", "tsnet_startup_errors", errCount, "details", err)
		} else {
			slog.Error("scrapeClientMetrics error", "error", err)
		}
		ScrapeErrors.WithLabelValues(target, "client_scrape_failed").Inc()
	}

	// Only now, with the new-name client series written, drop the old-name
	// ones, so a concurrent /metrics scrape never sees the device without any.
	dropRenamedDeviceSeries(renamed)

	c.tracker.CleanupRemovedDevices(seen)
	LastScrapeTime.Set(float64(time.Now().Unix()))
	return nil
}

// updateAPIMetrics sets the Tailscale API derived series for every device.
// It returns the devices seen in this cycle, device ID to device name, and,
// for devices renamed since the last cycle, their previous name (see
// dropRenamedDeviceSeries).
func (c *Collector) updateAPIMetrics(devices []device.Device) (seen, renamed map[string]string) {
	seen = map[string]string{}
	renamed = map[string]string{}
	next := make(map[string]deviceLabelState, len(devices))

	onlineCount := 0
	for _, d := range devices {
		id := d.ID.String()
		state := deviceLabelState{name: d.Name.String(), fingerprint: deviceLabelFingerprint(d)}
		if prev, ok := c.labelState[id]; ok && prev.fingerprint != state.fingerprint {
			slog.Debug("device labels changed, dropping old series", "device_id", id)
			cleanupAPIDeviceMetrics(id)
			if prev.name != state.name {
				renamed[id] = prev.name
			}
		}
		next[id] = state

		// Set static device info (always present, no online status in labels)
		DeviceInfo.WithLabelValues(d.ID.String(), d.Name.String(), truncateLabel(d.OS), truncateLabel(d.ClientVersion)).Set(1)

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
			DeviceUser.WithLabelValues(d.ID.String(), d.Name.String(), truncateLabel(d.User)).Set(1)
		}

		if !d.KeyExpiryDisabled && !d.Expires.IsZero() {
			DeviceMachineKeyExpiry.WithLabelValues(d.ID.String(), d.Name.String()).Set(float64(d.Expires.Unix()))
		} else {
			DeviceMachineKeyExpiry.WithLabelValues(d.ID.String(), d.Name.String()).Set(0)
		}

		for _, route := range d.AdvertisedRoutes {
			DeviceRoutesAdvertised.WithLabelValues(d.ID.String(), d.Name.String(), truncateLabel(route)).Set(1)
		}

		for _, route := range d.EnabledRoutes {
			DeviceRoutesEnabled.WithLabelValues(d.ID.String(), d.Name.String(), truncateLabel(route)).Set(1)
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
				DeviceLatency.WithLabelValues(d.ID.String(), d.Name.String(), truncateLabel(region), preferredStr).Set(latencyInfo.LatencyMs)
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

		seen[d.ID.String()] = d.Name.String()
	}

	OnlineDevicesCount.Set(float64(onlineCount))
	c.labelState = next

	return seen, renamed
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
