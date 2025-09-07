// Package metrics provides functionality for collecting and tracking Tailscale network metrics.
package metrics

import (
	"log/slog"
	"sync"
	"time"
)

// DeviceMetricsTracker tracks active devices and their last seen timestamps.
type DeviceMetricsTracker struct {
	mu           sync.RWMutex
	knownDevices map[string]bool
	lastSeen     map[string]time.Time
}

// NewDeviceMetricsTracker creates a new instance of DeviceMetricsTracker.
func NewDeviceMetricsTracker() *DeviceMetricsTracker {
	return &DeviceMetricsTracker{
		knownDevices: make(map[string]bool),
		lastSeen:     make(map[string]time.Time),
	}
}

// MarkDeviceActive marks a device as active and updates its last seen timestamp.
func (d *DeviceMetricsTracker) MarkDeviceActive(deviceID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.knownDevices[deviceID] = true
	d.lastSeen[deviceID] = time.Now()
}

// CleanupStaleDevices removes devices that haven't been seen for longer than the specified duration and returns their IDs.
func (d *DeviceMetricsTracker) CleanupStaleDevices(staleDuration time.Duration) []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	var staleDevices []string
	now := time.Now()

	for deviceID, lastSeen := range d.lastSeen {
		if now.Sub(lastSeen) > staleDuration {
			staleDevices = append(staleDevices, deviceID)
			delete(d.knownDevices, deviceID)
			delete(d.lastSeen, deviceID)
		}
	}

	return staleDevices
}

// CleanupRemovedDevices removes metrics for devices that are no longer discovered and updates tracking for currently seen devices.
func (d *DeviceMetricsTracker) CleanupRemovedDevices(seenDevices map[string]struct{}) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for deviceID := range d.knownDevices {
		if _, exists := seenDevices[deviceID]; !exists {
			slog.Info("device no longer discovered, cleaning up metrics", "device_id", deviceID)
			CleanupDeviceMetrics(deviceID)
			delete(d.knownDevices, deviceID)
			delete(d.lastSeen, deviceID)
		}
	}

	for deviceID := range seenDevices {
		d.knownDevices[deviceID] = true
		d.lastSeen[deviceID] = time.Now()
	}
}
