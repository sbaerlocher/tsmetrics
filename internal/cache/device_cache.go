package cache

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/sbaerlocher/tsmetrics/internal/types"
	"github.com/sbaerlocher/tsmetrics/pkg/device"
)

var (
	cacheHitRatio = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "tsmetrics_cache_hit_ratio",
		Help: "Cache hit ratio for device discovery",
	})

	cacheSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "tsmetrics_cache_size_devices",
		Help: "Number of devices in cache",
	})

	apiCallsSaved = promauto.NewCounter(prometheus.CounterOpts{
		Name: "tsmetrics_api_calls_saved_total",
		Help: "Number of API calls saved by caching",
	})

	cacheOperationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "tsmetrics_cache_operation_duration_seconds",
			Help:    "Duration of cache operations",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation"},
	)
)

type CachedDevice struct {
	Device      device.Device
	CachedAt    time.Time
	LastAccess  time.Time
	AccessCount uint64
}

type DeviceCache struct {
	devices     map[string]*CachedDevice
	lastFetch   time.Time
	ttl         time.Duration
	maxAge      time.Duration
	mutex       sync.RWMutex
	hitCount    uint64
	missCount   uint64
	refreshFunc func() ([]device.Device, error)
}

type CacheStats struct {
	HitCount    uint64        `json:"hit_count"`
	MissCount   uint64        `json:"miss_count"`
	HitRatio    float64       `json:"hit_ratio"`
	DeviceCount int           `json:"device_count"`
	LastFetch   time.Time     `json:"last_fetch"`
	TTL         time.Duration `json:"ttl"`
	MemoryUsage int64         `json:"memory_usage_bytes"`
}

func NewDeviceCache(ttl, maxAge time.Duration, refreshFunc func() ([]device.Device, error)) *DeviceCache {
	return &DeviceCache{
		devices:     make(map[string]*CachedDevice),
		ttl:         ttl,
		maxAge:      maxAge,
		refreshFunc: refreshFunc,
	}
}

func (dc *DeviceCache) GetDevices(forceRefresh bool) ([]device.Device, error) {
	start := time.Now()
	defer func() {
		cacheOperationDuration.WithLabelValues("get").Observe(time.Since(start).Seconds())
	}()

	dc.mutex.RLock()

	if !forceRefresh && time.Since(dc.lastFetch) < dc.ttl {
		devices := make([]device.Device, 0, len(dc.devices))
		now := time.Now()

		for _, cached := range dc.devices {
			if time.Since(cached.CachedAt) < dc.maxAge {
				cached.LastAccess = now
				atomic.AddUint64(&cached.AccessCount, 1)
				devices = append(devices, cached.Device)
			}
		}

		atomic.AddUint64(&dc.hitCount, 1)
		apiCallsSaved.Inc()
		dc.updateMetrics()
		dc.mutex.RUnlock()
		return devices, nil
	}

	dc.mutex.RUnlock()

	atomic.AddUint64(&dc.missCount, 1)
	return dc.refreshCache()
}

func (dc *DeviceCache) refreshCache() ([]device.Device, error) {
	start := time.Now()
	defer func() {
		cacheOperationDuration.WithLabelValues("refresh").Observe(time.Since(start).Seconds())
	}()

	if dc.refreshFunc == nil {
		return nil, ErrNoRefreshFunction
	}

	devices, err := dc.refreshFunc()
	if err != nil {
		return nil, err
	}

	dc.mutex.Lock()
	defer dc.mutex.Unlock()

	now := time.Now()
	dc.devices = make(map[string]*CachedDevice, len(devices))

	for _, dev := range devices {
		dc.devices[dev.ID.String()] = &CachedDevice{
			Device:      dev,
			CachedAt:    now,
			LastAccess:  now,
			AccessCount: 1,
		}
	}

	dc.lastFetch = now
	dc.updateMetrics()

	return devices, nil
}

func (dc *DeviceCache) GetCacheStats() CacheStats {
	dc.mutex.RLock()
	defer dc.mutex.RUnlock()

	hitCount := atomic.LoadUint64(&dc.hitCount)
	missCount := atomic.LoadUint64(&dc.missCount)
	totalRequests := hitCount + missCount

	var hitRatio float64
	if totalRequests > 0 {
		hitRatio = float64(hitCount) / float64(totalRequests)
	}

	return CacheStats{
		HitCount:    hitCount,
		MissCount:   missCount,
		HitRatio:    hitRatio,
		DeviceCount: len(dc.devices),
		LastFetch:   dc.lastFetch,
		TTL:         dc.ttl,
		MemoryUsage: dc.estimateMemoryUsage(),
	}
}

func (dc *DeviceCache) InvalidateDevice(deviceID types.DeviceID) {
	dc.mutex.Lock()
	defer dc.mutex.Unlock()

	delete(dc.devices, deviceID.String())
	dc.updateMetrics()
}

func (dc *DeviceCache) Clear() {
	dc.mutex.Lock()
	defer dc.mutex.Unlock()

	dc.devices = make(map[string]*CachedDevice)
	dc.lastFetch = time.Time{}
	atomic.StoreUint64(&dc.hitCount, 0)
	atomic.StoreUint64(&dc.missCount, 0)
	dc.updateMetrics()
}

func (dc *DeviceCache) CleanupStaleEntries() int {
	dc.mutex.Lock()
	defer dc.mutex.Unlock()

	var cleaned int
	now := time.Now()

	for id, cached := range dc.devices {
		if now.Sub(cached.CachedAt) > dc.maxAge {
			delete(dc.devices, id)
			cleaned++
		}
	}

	if cleaned > 0 {
		dc.updateMetrics()
	}

	return cleaned
}

func (dc *DeviceCache) updateMetrics() {
	hitCount := atomic.LoadUint64(&dc.hitCount)
	missCount := atomic.LoadUint64(&dc.missCount)
	totalRequests := hitCount + missCount

	if totalRequests > 0 {
		ratio := float64(hitCount) / float64(totalRequests)
		cacheHitRatio.Set(ratio)
	}

	cacheSize.Set(float64(len(dc.devices)))
}

func (dc *DeviceCache) estimateMemoryUsage() int64 {
	const avgDeviceSize = 1024
	return int64(len(dc.devices)) * avgDeviceSize
}

var (
	ErrNoRefreshFunction = fmt.Errorf("no refresh function provided")
)
