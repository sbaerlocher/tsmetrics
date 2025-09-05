// Package metrics provides Prometheus metrics definitions and collection utilities.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// ScrapeDuration tracks the time spent scraping metrics from targets.
	ScrapeDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "tsmetrics_scrape_duration_seconds",
			Help: "Time spent scraping target",
		},
		[]string{"target"},
	)

	// ScrapeErrors tracks the number of scraping errors by type and target
	ScrapeErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tsmetrics_scrape_errors_total",
			Help: "Scrape errors by target",
		},
		[]string{"target", "error_type"},
	)

	// APICallDuration tracks the duration of API calls by endpoint and status.
	APICallDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "tsmetrics_api_call_duration_seconds",
			Help:    "API call duration",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"endpoint", "status"},
	)

	DeviceErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tsmetrics_device_errors_total",
			Help: "Device errors by type and device",
		},
		[]string{"device_id", "device_name", "error_type", "retryable"},
	)

	RetryAttempts = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tsmetrics_retry_attempts_total",
			Help: "Number of retry attempts",
		},
		[]string{"device_id", "device_name", "reason"},
	)

	MemoryUsage = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "tsmetrics_memory_usage_bytes",
			Help: "Current memory usage in bytes",
		},
	)

	GoroutineCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "tsmetrics_goroutines_total",
			Help: "Number of active goroutines",
		},
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
			Help: "Static info about devices (value=1)",
		},
		[]string{"device_id", "device_name", "online", "os", "version"},
	)

	InboundBytes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_inbound_bytes_total", Help: "Current inbound bytes from device (reflects remote counter)"},
		[]string{"device_id", "device_name", "path"},
	)
	OutboundBytes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_outbound_bytes_total", Help: "Current outbound bytes from device (reflects remote counter)"},
		[]string{"device_id", "device_name", "path"},
	)
	InboundPackets = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_inbound_packets_total", Help: "Current inbound packets from device (reflects remote counter)"},
		[]string{"device_id", "device_name", "path"},
	)
	OutboundPackets = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_outbound_packets_total", Help: "Current outbound packets from device (reflects remote counter)"},
		[]string{"device_id", "device_name", "path"},
	)
	InboundDroppedPackets = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_inbound_dropped_packets_total", Help: "Current dropped inbound packets from device (reflects remote counter)"},
		[]string{"device_id", "device_name"},
	)
	OutboundDroppedPackets = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_outbound_dropped_packets_total", Help: "Current dropped outbound packets from device (reflects remote counter)"},
		[]string{"device_id", "device_name", "reason"},
	)
	HealthMessages = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_health_messages", Help: "Health message count from device"},
		[]string{"device_id", "device_name", "type"},
	)
	AdvertisedRoutes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_advertised_routes", Help: "Number of advertised network routes by device"},
		[]string{"device_id", "device_name"},
	)
	ApprovedRoutes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{Name: "tailscaled_approved_routes", Help: "Number of approved network routes by device"},
		[]string{"device_id", "device_name"},
	)

	DeviceAuthorized = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_authorized",
			Help: "Device authorization status (1=authorized, 0=unauthorized)",
		},
		[]string{"device_id", "device_name"},
	)

	DeviceLastSeen = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_last_seen_timestamp",
			Help: "Timestamp when device was last seen (Unix seconds)",
		},
		[]string{"device_id", "device_name"},
	)

	DeviceUser = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_user",
			Help: "User assignment for device (value=1)",
		},
		[]string{"device_id", "device_name", "user_email"},
	)

	DeviceMachineKeyExpiry = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_machine_key_expiry",
			Help: "Machine key expiry timestamp (Unix seconds, 0=disabled)",
		},
		[]string{"device_id", "device_name"},
	)

	DeviceRoutesAdvertised = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_routes_advertised",
			Help: "Advertised routes by device (value=1)",
		},
		[]string{"device_id", "device_name", "route"},
	)

	DeviceRoutesEnabled = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_routes_enabled",
			Help: "Enabled routes by device (value=1)",
		},
		[]string{"device_id", "device_name", "route"},
	)

	DeviceExitNode = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_exit_node",
			Help: "Device exit node status (1=is exit node, 0=not exit node)",
		},
		[]string{"device_id", "device_name"},
	)

	DeviceSubnetRouter = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_subnet_router",
			Help: "Device subnet router status (1=advertises routes, 0=no routes)",
		},
		[]string{"device_id", "device_name"},
	)
)

// CleanupDeviceMetrics removes all metrics associated with a specific device.
func CleanupDeviceMetrics(deviceID string) {
	InboundBytes.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	OutboundBytes.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	InboundPackets.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	OutboundPackets.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	InboundDroppedPackets.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	OutboundDroppedPackets.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	HealthMessages.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	AdvertisedRoutes.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	ApprovedRoutes.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})

	DeviceInfo.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	DeviceAuthorized.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	DeviceLastSeen.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	DeviceUser.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	DeviceMachineKeyExpiry.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	DeviceRoutesAdvertised.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	DeviceRoutesEnabled.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	DeviceExitNode.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	DeviceSubnetRouter.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
}
