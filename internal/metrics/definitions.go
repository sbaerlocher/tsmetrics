// Package metrics provides Prometheus metrics definitions and collection utilities.
package metrics

import (
	dto "github.com/prometheus/client_model/go"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Label keys shared across metric definitions and lookups.
const (
	labelDeviceID   = "device_id"
	labelDeviceName = "device_name"
	labelErrorType  = "error_type"
	labelPath       = "path"
	labelStrategy   = "strategy"
	labelTarget     = "target"
)

// errorTypeClientFetchFailed is the error_type of a device's own scrape error
// series (target=<device name>), written by the scraper and deleted on cleanup.
const errorTypeClientFetchFailed = "client_fetch_failed"

var (
	// ScrapeDuration tracks the time spent scraping metrics from targets.
	ScrapeDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "tsmetrics_scrape_duration_seconds",
			Help: "Time spent scraping target",
		},
		[]string{labelTarget},
	)

	// ScrapeErrors tracks the number of scraping errors by type and target
	ScrapeErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tsmetrics_scrape_errors_total",
			Help: "Scrape errors by target",
		},
		[]string{labelTarget, labelErrorType},
	)

	DeviceErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tsmetrics_device_errors_total",
			Help: "Device errors by type and device",
		},
		[]string{labelDeviceID, labelDeviceName, labelErrorType, "retryable"},
	)

	LastScrapeTime = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "tsmetrics_last_scrape_timestamp_seconds",
			Help: "Unix timestamp of last successful scrape",
		},
	)

	OnlineDevicesCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "tsmetrics_online_devices_total",
			Help: "Number of online devices",
		},
	)

	// The tailscale_device_* vectors below are set from device.Device fields.
	// Any field that feeds a label value, or decides whether a series is set,
	// must also feed deviceLabelFingerprint (collector.go). Otherwise a change
	// of that field leaves the old series behind.

	// DeviceCount tracks the total number of devices discovered.
	DeviceCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "tailscale_device_count",
			Help: "Number of devices discovered",
		},
	)

	// DeviceInfo provides static information about Tailscale devices.
	DeviceInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_info",
			Help: "Static info about devices (value=1 always present)",
		},
		[]string{labelDeviceID, labelDeviceName, "os", "version"},
	)

	// DeviceOnline indicates if a device is currently online (1=online, 0=offline).
	DeviceOnline = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_online",
			Help: "Device online status (1=online, 0=offline)",
		},
		[]string{labelDeviceID, labelDeviceName},
	)

	InboundBytes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_inbound_bytes_total", Help: "Current inbound bytes from device (reflects remote counter)"},
		[]string{labelDeviceID, labelDeviceName, labelPath},
	)
	OutboundBytes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_outbound_bytes_total", Help: "Current outbound bytes from device (reflects remote counter)"},
		[]string{labelDeviceID, labelDeviceName, labelPath},
	)
	InboundPackets = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_inbound_packets_total", Help: "Current inbound packets from device (reflects remote counter)"},
		[]string{labelDeviceID, labelDeviceName, labelPath},
	)
	OutboundPackets = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_outbound_packets_total", Help: "Current outbound packets from device (reflects remote counter)"},
		[]string{labelDeviceID, labelDeviceName, labelPath},
	)
	InboundDroppedPackets = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_inbound_dropped_packets_total", Help: "Current dropped inbound packets from device (reflects remote counter)"},
		[]string{labelDeviceID, labelDeviceName},
	)
	OutboundDroppedPackets = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_outbound_dropped_packets_total", Help: "Current dropped outbound packets from device (reflects remote counter)"},
		[]string{labelDeviceID, labelDeviceName, "reason"},
	)
	HealthMessages = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_health_messages", Help: "Health message count from device"},
		[]string{labelDeviceID, labelDeviceName, "type"},
	)
	AdvertisedRoutes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_advertised_routes", Help: "Number of advertised network routes by device"},
		[]string{labelDeviceID, labelDeviceName},
	)
	ApprovedRoutes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_approved_routes", Help: "Number of approved network routes by device"},
		[]string{labelDeviceID, labelDeviceName},
	)

	DeviceAuthorized = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_authorized",
			Help: "Device authorization status (1=authorized, 0=unauthorized)",
		},
		[]string{labelDeviceID, labelDeviceName},
	)

	DeviceLastSeen = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_last_seen_timestamp",
			Help: "Timestamp when device was last seen (Unix seconds)",
		},
		[]string{labelDeviceID, labelDeviceName},
	)

	DeviceUser = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_user",
			Help: "User assignment for device (value=1)",
		},
		[]string{labelDeviceID, labelDeviceName, "user_email"},
	)

	DeviceMachineKeyExpiry = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_machine_key_expiry",
			Help: "Machine key expiry timestamp (Unix seconds, 0=disabled)",
		},
		[]string{labelDeviceID, labelDeviceName},
	)

	DeviceRoutesAdvertised = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_routes_advertised",
			Help: "Advertised routes by device (value=1)",
		},
		[]string{labelDeviceID, labelDeviceName, "route"},
	)

	DeviceRoutesEnabled = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_routes_enabled",
			Help: "Enabled routes by device (value=1)",
		},
		[]string{labelDeviceID, labelDeviceName, "route"},
	)

	DeviceExitNode = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_exit_node",
			Help: "Device exit node status (1=is exit node, 0=not exit node)",
		},
		[]string{labelDeviceID, labelDeviceName},
	)

	DeviceExitNodeOption = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_exit_node_option",
			Help: "Device exit node option available (1=available as exit node, 0=not available)",
		},
		[]string{labelDeviceID, labelDeviceName},
	)

	DeviceSubnetRouter = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_subnet_router",
			Help: "Device subnet router status (1=advertises routes, 0=no routes)",
		},
		[]string{labelDeviceID, labelDeviceName},
	)

	// New metrics for additional API fields
	DeviceUpdateAvailable = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_update_available",
			Help: "Device update available status (1=update available, 0=up to date)",
		},
		[]string{labelDeviceID, labelDeviceName},
	)

	DeviceCreated = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_created_timestamp",
			Help: "Device creation timestamp (Unix seconds)",
		},
		[]string{labelDeviceID, labelDeviceName},
	)

	DeviceExternal = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_external",
			Help: "Device external status (1=external, 0=internal)",
		},
		[]string{labelDeviceID, labelDeviceName},
	)

	DeviceBlocksIncoming = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_blocks_incoming_connections",
			Help: "Device blocks incoming connections (1=blocks, 0=allows)",
		},
		[]string{labelDeviceID, labelDeviceName},
	)

	DeviceEphemeral = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_ephemeral",
			Help: "Device ephemeral status (1=ephemeral, 0=persistent)",
		},
		[]string{labelDeviceID, labelDeviceName},
	)

	DeviceMultipleConnections = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_multiple_connections",
			Help: "Device multiple connections status (1=supports, 0=single connection)",
		},
		[]string{labelDeviceID, labelDeviceName},
	)

	DeviceLatency = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_latency_ms",
			Help: "Device latency to DERP regions in milliseconds",
		},
		[]string{labelDeviceID, labelDeviceName, "derp_region", "preferred"},
	)

	DeviceEndpoints = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_endpoints_total",
			Help: "Number of connectivity endpoints for device",
		},
		[]string{labelDeviceID, labelDeviceName},
	)

	DeviceClientSupports = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_client_supports",
			Help: "Device client feature support (1=supported, 0=not supported)",
		},
		[]string{labelDeviceID, labelDeviceName, "feature"},
	)

	DeviceTailnetLockError = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_tailnet_lock_error",
			Help: "Device tailnet lock error status (1=has error, 0=no error)",
		},
		[]string{labelDeviceID, labelDeviceName},
	)

	DevicePostureSerialNumbers = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_posture_serial_numbers_total",
			Help: "Number of posture identity serial numbers for device",
		},
		[]string{labelDeviceID, labelDeviceName},
	)
)

// GetLastScrapeTime returns the Unix timestamp of the last successful scrape from the Prometheus metric.
func GetLastScrapeTime() int64 {
	m := &dto.Metric{}
	if err := LastScrapeTime.Write(m); err != nil || m.Gauge == nil {
		return 0
	}
	return int64(m.Gauge.GetValue())
}

// GetOnlineDevicesCount returns the current count of online devices from the Prometheus metric.
func GetOnlineDevicesCount() int {
	m := &dto.Metric{}
	if err := OnlineDevicesCount.Write(m); err != nil || m.Gauge == nil {
		return 0
	}
	return int(m.Gauge.GetValue())
}

// CleanupDeviceMetrics removes all metrics associated with a specific device ID.
// The device's client scrape error series is labeled by name, not ID; remove it
// with deleteDeviceScrapeErrors.
func CleanupDeviceMetrics(deviceID string) {
	DeviceErrors.DeletePartialMatch(prometheus.Labels{labelDeviceID: deviceID})
	cleanupClientDeviceMetrics(deviceID)
	cleanupAPIDeviceMetrics(deviceID)
}

// deleteDeviceScrapeErrors removes the client_fetch_failed scrape error series
// of a device that is gone or renamed. The series is labeled target=<device name>,
// so it is kept when a device seen in this cycle (device ID to device name)
// carries the same name. The collector's own series (fetch_failed,
// client_scrape_failed) share the label but are never touched.
func deleteDeviceScrapeErrors(name string, seen map[string]string) {
	// ponytail: linear scan per removed device, build a name set if tailnets grow large.
	for _, seenName := range seen {
		if seenName == name {
			return
		}
	}
	ScrapeErrors.DeleteLabelValues(name, errorTypeClientFetchFailed)
}

// clientDeviceVecs lists the vectors scraped from a device's client metrics
// endpoint.
func clientDeviceVecs() []*prometheus.GaugeVec {
	return []*prometheus.GaugeVec{
		InboundBytes, OutboundBytes, InboundPackets, OutboundPackets,
		InboundDroppedPackets, OutboundDroppedPackets, HealthMessages,
		AdvertisedRoutes, ApprovedRoutes,
	}
}

// cleanupClientDeviceMetrics removes the series scraped from the device's
// client metrics endpoint.
func cleanupClientDeviceMetrics(deviceID string) {
	for _, vec := range clientDeviceVecs() {
		vec.DeletePartialMatch(prometheus.Labels{labelDeviceID: deviceID})
	}
}

// cleanupAPIDeviceMetrics removes the series derived from the Tailscale API.
func cleanupAPIDeviceMetrics(deviceID string) {
	DeviceInfo.DeletePartialMatch(prometheus.Labels{labelDeviceID: deviceID})
	DeviceOnline.DeletePartialMatch(prometheus.Labels{labelDeviceID: deviceID})
	DeviceAuthorized.DeletePartialMatch(prometheus.Labels{labelDeviceID: deviceID})
	DeviceLastSeen.DeletePartialMatch(prometheus.Labels{labelDeviceID: deviceID})
	DeviceUser.DeletePartialMatch(prometheus.Labels{labelDeviceID: deviceID})
	DeviceMachineKeyExpiry.DeletePartialMatch(prometheus.Labels{labelDeviceID: deviceID})
	DeviceRoutesAdvertised.DeletePartialMatch(prometheus.Labels{labelDeviceID: deviceID})
	DeviceRoutesEnabled.DeletePartialMatch(prometheus.Labels{labelDeviceID: deviceID})
	DeviceExitNode.DeletePartialMatch(prometheus.Labels{labelDeviceID: deviceID})
	DeviceExitNodeOption.DeletePartialMatch(prometheus.Labels{labelDeviceID: deviceID})
	DeviceSubnetRouter.DeletePartialMatch(prometheus.Labels{labelDeviceID: deviceID})

	// Clean up new metrics
	DeviceUpdateAvailable.DeletePartialMatch(prometheus.Labels{labelDeviceID: deviceID})
	DeviceCreated.DeletePartialMatch(prometheus.Labels{labelDeviceID: deviceID})
	DeviceExternal.DeletePartialMatch(prometheus.Labels{labelDeviceID: deviceID})
	DeviceBlocksIncoming.DeletePartialMatch(prometheus.Labels{labelDeviceID: deviceID})
	DeviceEphemeral.DeletePartialMatch(prometheus.Labels{labelDeviceID: deviceID})
	DeviceMultipleConnections.DeletePartialMatch(prometheus.Labels{labelDeviceID: deviceID})
	DeviceLatency.DeletePartialMatch(prometheus.Labels{labelDeviceID: deviceID})
	DeviceEndpoints.DeletePartialMatch(prometheus.Labels{labelDeviceID: deviceID})
	DeviceClientSupports.DeletePartialMatch(prometheus.Labels{labelDeviceID: deviceID})
	DeviceTailnetLockError.DeletePartialMatch(prometheus.Labels{labelDeviceID: deviceID})
	DevicePostureSerialNumbers.DeletePartialMatch(prometheus.Labels{labelDeviceID: deviceID})
}
