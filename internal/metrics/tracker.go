// Package metrics provides functionality for collecting and tracking Tailscale network metrics.
package metrics

import (
	"log/slog"
	"sync"
)

// DeviceMetricsTracker tracks the devices of the last discovery cycle by ID and name.
type DeviceMetricsTracker struct {
	mu           sync.Mutex
	knownDevices map[string]string
}

// NewDeviceMetricsTracker creates a new instance of DeviceMetricsTracker.
func NewDeviceMetricsTracker() *DeviceMetricsTracker {
	return &DeviceMetricsTracker{knownDevices: make(map[string]string)}
}

// CleanupRemovedDevices removes metrics for devices that are no longer discovered and
// tracks the currently seen devices (device ID to device name) for the next cycle.
func (d *DeviceMetricsTracker) CleanupRemovedDevices(seenDevices map[string]string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for deviceID, deviceName := range d.knownDevices {
		if _, exists := seenDevices[deviceID]; !exists {
			slog.Info("device no longer discovered, cleaning up metrics", "device_id", deviceID)
			CleanupDeviceMetrics(deviceID, deviceName)
			delete(d.knownDevices, deviceID)
		}
	}

	for deviceID, deviceName := range seenDevices {
		d.knownDevices[deviceID] = deviceName
	}
}
