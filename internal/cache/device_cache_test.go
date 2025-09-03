package cache

import (
	"errors"
	"testing"
	"time"

	"github.com/sbaerlocher/tsmetrics/internal/types"
	"github.com/sbaerlocher/tsmetrics/pkg/device"
)

func createTestDevice(id, name string) device.Device {
	return device.Device{
		ID:     types.DeviceID(id),
		Name:   types.DeviceName(name),
		Host:   "192.168.1.1",
		Online: true,
	}
}

func TestNewDeviceCache(t *testing.T) {
	ttl := 5 * time.Minute
	maxAge := 30 * time.Minute
	refreshFunc := func() ([]device.Device, error) {
		return nil, nil
	}

	cache := NewDeviceCache(ttl, maxAge, refreshFunc)

	if cache.ttl != ttl {
		t.Errorf("Expected TTL %v, got %v", ttl, cache.ttl)
	}
	if cache.maxAge != maxAge {
		t.Errorf("Expected maxAge %v, got %v", maxAge, cache.maxAge)
	}
	if cache.devices == nil {
		t.Error("Expected devices map to be initialized")
	}
}

func TestDeviceCache_GetDevices_CacheHit(t *testing.T) {
	ttl := 5 * time.Minute
	maxAge := 30 * time.Minute
	refreshFunc := func() ([]device.Device, error) {
		return []device.Device{
			createTestDevice("device1", "Device 1"),
		}, nil
	}

	cache := NewDeviceCache(ttl, maxAge, refreshFunc)

	devices, err := cache.GetDevices(false)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(devices) != 1 {
		t.Errorf("Expected 1 device, got %d", len(devices))
	}

	devices2, err := cache.GetDevices(false)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(devices2) != 1 {
		t.Errorf("Expected 1 device from cache, got %d", len(devices2))
	}

	stats := cache.GetCacheStats()
	if stats.HitCount != 1 {
		t.Errorf("Expected 1 cache hit, got %d", stats.HitCount)
	}
	if stats.MissCount != 1 {
		t.Errorf("Expected 1 cache miss, got %d", stats.MissCount)
	}
}

func TestDeviceCache_GetDevices_CacheMiss(t *testing.T) {
	ttl := 100 * time.Millisecond
	maxAge := 30 * time.Minute
	callCount := 0
	refreshFunc := func() ([]device.Device, error) {
		callCount++
		return []device.Device{
			createTestDevice("device1", "Device 1"),
		}, nil
	}

	cache := NewDeviceCache(ttl, maxAge, refreshFunc)

	devices, err := cache.GetDevices(false)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(devices) != 1 {
		t.Errorf("Expected 1 device, got %d", len(devices))
	}

	time.Sleep(200 * time.Millisecond)

	devices2, err := cache.GetDevices(false)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(devices2) != 1 {
		t.Errorf("Expected 1 device, got %d", len(devices2))
	}

	if callCount != 2 {
		t.Errorf("Expected refresh function to be called 2 times, got %d", callCount)
	}

	stats := cache.GetCacheStats()
	if stats.MissCount != 2 {
		t.Errorf("Expected 2 cache misses, got %d", stats.MissCount)
	}
}

func TestDeviceCache_ForceRefresh(t *testing.T) {
	ttl := 5 * time.Minute
	maxAge := 30 * time.Minute
	callCount := 0
	refreshFunc := func() ([]device.Device, error) {
		callCount++
		return []device.Device{
			createTestDevice("device1", "Device 1"),
		}, nil
	}

	cache := NewDeviceCache(ttl, maxAge, refreshFunc)

	devices, err := cache.GetDevices(false)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(devices) != 1 {
		t.Errorf("Expected 1 device, got %d", len(devices))
	}

	devices2, err := cache.GetDevices(true)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(devices2) != 1 {
		t.Errorf("Expected 1 device, got %d", len(devices2))
	}

	if callCount != 2 {
		t.Errorf("Expected refresh function to be called 2 times, got %d", callCount)
	}
}

func TestDeviceCache_RefreshError(t *testing.T) {
	ttl := 5 * time.Minute
	maxAge := 30 * time.Minute
	expectedErr := errors.New("api error")
	refreshFunc := func() ([]device.Device, error) {
		return nil, expectedErr
	}

	cache := NewDeviceCache(ttl, maxAge, refreshFunc)

	devices, err := cache.GetDevices(false)
	if err != expectedErr {
		t.Errorf("Expected error %v, got %v", expectedErr, err)
	}
	if devices != nil {
		t.Error("Expected nil devices on error")
	}
}

func TestDeviceCache_NoRefreshFunction(t *testing.T) {
	cache := NewDeviceCache(5*time.Minute, 30*time.Minute, nil)

	devices, err := cache.GetDevices(false)
	if err != ErrNoRefreshFunction {
		t.Errorf("Expected ErrNoRefreshFunction, got %v", err)
	}
	if devices != nil {
		t.Error("Expected nil devices")
	}
}

func TestDeviceCache_InvalidateDevice(t *testing.T) {
	ttl := 5 * time.Minute
	maxAge := 30 * time.Minute
	refreshFunc := func() ([]device.Device, error) {
		return []device.Device{
			createTestDevice("device1", "Device 1"),
			createTestDevice("device2", "Device 2"),
		}, nil
	}

	cache := NewDeviceCache(ttl, maxAge, refreshFunc)

	devices, err := cache.GetDevices(false)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(devices) != 2 {
		t.Errorf("Expected 2 devices, got %d", len(devices))
	}

	cache.InvalidateDevice(types.DeviceID("device1"))

	stats := cache.GetCacheStats()
	if stats.DeviceCount != 1 {
		t.Errorf("Expected 1 device in cache after invalidation, got %d", stats.DeviceCount)
	}
}

func TestDeviceCache_Clear(t *testing.T) {
	ttl := 5 * time.Minute
	maxAge := 30 * time.Minute
	refreshFunc := func() ([]device.Device, error) {
		return []device.Device{
			createTestDevice("device1", "Device 1"),
		}, nil
	}

	cache := NewDeviceCache(ttl, maxAge, refreshFunc)

	devices, err := cache.GetDevices(false)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(devices) != 1 {
		t.Errorf("Expected 1 device, got %d", len(devices))
	}

	cache.Clear()

	stats := cache.GetCacheStats()
	if stats.DeviceCount != 0 {
		t.Errorf("Expected 0 devices after clear, got %d", stats.DeviceCount)
	}
	if stats.HitCount != 0 {
		t.Errorf("Expected 0 hit count after clear, got %d", stats.HitCount)
	}
	if stats.MissCount != 0 {
		t.Errorf("Expected 0 miss count after clear, got %d", stats.MissCount)
	}
}

func TestDeviceCache_CleanupStaleEntries(t *testing.T) {
	ttl := 5 * time.Minute
	maxAge := 100 * time.Millisecond
	refreshFunc := func() ([]device.Device, error) {
		return []device.Device{
			createTestDevice("device1", "Device 1"),
		}, nil
	}

	cache := NewDeviceCache(ttl, maxAge, refreshFunc)

	devices, err := cache.GetDevices(false)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(devices) != 1 {
		t.Errorf("Expected 1 device, got %d", len(devices))
	}

	time.Sleep(200 * time.Millisecond)

	cleaned := cache.CleanupStaleEntries()
	if cleaned != 1 {
		t.Errorf("Expected 1 stale entry to be cleaned, got %d", cleaned)
	}

	stats := cache.GetCacheStats()
	if stats.DeviceCount != 0 {
		t.Errorf("Expected 0 devices after cleanup, got %d", stats.DeviceCount)
	}
}

func TestDeviceCache_GetCacheStats(t *testing.T) {
	ttl := 5 * time.Minute
	maxAge := 30 * time.Minute
	refreshFunc := func() ([]device.Device, error) {
		return []device.Device{
			createTestDevice("device1", "Device 1"),
		}, nil
	}

	cache := NewDeviceCache(ttl, maxAge, refreshFunc)

	devices, err := cache.GetDevices(false)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(devices) != 1 {
		t.Errorf("Expected 1 device, got %d", len(devices))
	}

	cache.GetDevices(false)

	stats := cache.GetCacheStats()
	if stats.HitCount != 1 {
		t.Errorf("Expected 1 hit, got %d", stats.HitCount)
	}
	if stats.MissCount != 1 {
		t.Errorf("Expected 1 miss, got %d", stats.MissCount)
	}
	if stats.HitRatio != 0.5 {
		t.Errorf("Expected hit ratio 0.5, got %f", stats.HitRatio)
	}
	if stats.DeviceCount != 1 {
		t.Errorf("Expected 1 device, got %d", stats.DeviceCount)
	}
	if stats.TTL != ttl {
		t.Errorf("Expected TTL %v, got %v", ttl, stats.TTL)
	}
	if stats.MemoryUsage <= 0 {
		t.Error("Expected positive memory usage")
	}
}
