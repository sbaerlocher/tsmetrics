package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Prometheus metrics definitions
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

	// Enhanced error metrics
	apiCallDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "tsmetrics_api_call_duration_seconds",
			Help:    "API call duration",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"endpoint", "status"},
	)

	deviceErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tsmetrics_device_errors_total",
			Help: "Device errors by type and device",
		},
		[]string{"device_id", "device_name", "error_type", "retryable"},
	)

	circuitBreakerState = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tsmetrics_circuit_breaker_state",
			Help: "Circuit breaker state (0=closed, 1=open)",
		},
		[]string{"endpoint"},
	)

	retryAttempts = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tsmetrics_retry_attempts_total",
			Help: "Number of retry attempts",
		},
		[]string{"device_id", "device_name", "reason"},
	)

	// System metrics
	memoryUsage = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "tsmetrics_memory_usage_bytes",
			Help: "Current memory usage in bytes",
		},
	)

	goroutineCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "tsmetrics_goroutines_total",
			Help: "Number of active goroutines",
		},
	)

	lastScrapeTime = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "tsmetrics_last_scrape_timestamp_seconds",
			Help: "Unix timestamp of last successful scrape",
		},
	)

	onlineDevicesCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "tsmetrics_online_devices_total",
			Help: "Number of online devices",
		},
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
		[]string{"device_id", "device_name", "online", "os", "version"},
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

	// API-based metrics (from Tailscale REST API)
	deviceAuthorized = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_authorized",
			Help: "Device authorization status (1=authorized, 0=unauthorized)",
		},
		[]string{"device_id", "device_name"},
	)

	deviceLastSeen = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_last_seen_timestamp",
			Help: "Timestamp when device was last seen (Unix seconds)",
		},
		[]string{"device_id", "device_name"},
	)

	deviceUser = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_user",
			Help: "User assignment for device (value=1)",
		},
		[]string{"device_id", "device_name", "user_email"},
	)

	deviceMachineKeyExpiry = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_machine_key_expiry",
			Help: "Machine key expiry timestamp (Unix seconds, 0=disabled)",
		},
		[]string{"device_id", "device_name"},
	)

	deviceRoutesAdvertised = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_routes_advertised",
			Help: "Advertised routes by device (value=1)",
		},
		[]string{"device_id", "device_name", "route"},
	)

	deviceRoutesEnabled = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_routes_enabled",
			Help: "Enabled routes by device (value=1)",
		},
		[]string{"device_id", "device_name", "route"},
	)

	deviceExitNode = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_exit_node",
			Help: "Device exit node status (1=is exit node, 0=not exit node)",
		},
		[]string{"device_id", "device_name"},
	)

	deviceSubnetRouter = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_device_subnet_router",
			Help: "Device subnet router status (1=advertises routes, 0=no routes)",
		},
		[]string{"device_id", "device_name"},
	)
)

// cleanupDeviceMetrics removes all metric series for a given device
// This prevents memory leaks when devices go offline permanently
func cleanupDeviceMetrics(deviceID string) {
	// Client metrics (from device endpoints)
	d_inbound_bytes.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	d_outbound_bytes.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	d_inbound_packets.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	d_outbound_packets.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	d_inbound_dropped_packets.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	d_outbound_dropped_packets.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	d_health_messages.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	d_advertised_routes.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	d_approved_routes.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})

	// API metrics (from Tailscale API)
	deviceInfo.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	deviceAuthorized.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	deviceLastSeen.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	deviceUser.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	deviceMachineKeyExpiry.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	deviceRoutesAdvertised.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	deviceRoutesEnabled.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	deviceExitNode.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
	deviceSubnetRouter.DeletePartialMatch(prometheus.Labels{"device_id": deviceID})
}
