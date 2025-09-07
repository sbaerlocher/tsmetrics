// Package metrics provides performance monitoring and statistics collection
// for the tsmetrics application, including real-time tracking of scraping
// operations, resource usage, and alert management.
package metrics

import (
	"context"
	"math"
	"runtime"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	performanceGoroutines = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "tsmetrics_performance_goroutines",
		Help: "Number of goroutines currently running",
	})

	performanceMemoryUsage = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "tsmetrics_performance_memory_bytes",
		Help: "Memory usage in bytes",
	}, []string{"type"})

	performanceCPUUsage = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "tsmetrics_performance_cpu_usage_percent",
		Help: "CPU usage percentage",
	})

	performanceScrapingThroughput = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "tsmetrics_performance_scraping_throughput_devices_per_second",
		Help: "Number of devices scraped per second",
	})

	performanceActiveConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "tsmetrics_performance_active_connections",
		Help: "Number of active connections",
	})

	performanceQueueDepth = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "tsmetrics_performance_queue_depth",
		Help: "Depth of processing queues",
	}, []string{"queue_type"})
)

// PerformanceMonitor provides real-time monitoring and statistics collection
// for application performance metrics including CPU, memory, and scraping operations.
type PerformanceMonitor struct {
	interval       time.Duration
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	lastGCStats    runtime.MemStats
	lastScrapeTime time.Time
	scrapeCount    uint64
	mutex          sync.RWMutex
}

// PerformanceStats represents a snapshot of application performance metrics
// at a specific point in time, including resource usage and operational statistics.
type PerformanceStats struct {
	Timestamp          time.Time `json:"timestamp"`
	Goroutines         int       `json:"goroutines"`
	MemoryAllocated    uint64    `json:"memory_allocated_bytes"`
	MemorySystem       uint64    `json:"memory_system_bytes"`
	MemoryHeapInUse    uint64    `json:"memory_heap_inuse_bytes"`
	MemoryStackInUse   uint64    `json:"memory_stack_inuse_bytes"`
	GCCycles           uint32    `json:"gc_cycles"`
	GCPauseTotal       uint64    `json:"gc_pause_total_ns"`
	LastGCTime         time.Time `json:"last_gc_time"`
	CPUUsagePercent    float64   `json:"cpu_usage_percent"`
	ScrapingThroughput float64   `json:"scraping_throughput_dps"`
	ActiveConnections  int       `json:"active_connections"`
}

// NewPerformanceMonitor creates a new performance monitoring instance with the specified
// collection interval and returns a configured PerformanceMonitor ready for use.
func NewPerformanceMonitor(interval time.Duration) *PerformanceMonitor {
	ctx, cancel := context.WithCancel(context.Background())

	pm := &PerformanceMonitor{
		interval: interval,
		ctx:      ctx,
		cancel:   cancel,
	}

	runtime.ReadMemStats(&pm.lastGCStats)
	pm.lastScrapeTime = time.Now()

	return pm
}

func (pm *PerformanceMonitor) Start() {
	pm.wg.Add(1)
	go pm.monitorLoop()
}

func (pm *PerformanceMonitor) Stop() {
	pm.cancel()
	pm.wg.Wait()
}

func (pm *PerformanceMonitor) monitorLoop() {
	defer pm.wg.Done()

	ticker := time.NewTicker(pm.interval)
	defer ticker.Stop()

	for {
		select {
		case <-pm.ctx.Done():
			return
		case <-ticker.C:
			pm.collectMetrics()
		}
	}
}

func (pm *PerformanceMonitor) collectMetrics() {
	stats := pm.GetCurrentStats()

	performanceGoroutines.Set(float64(stats.Goroutines))
	performanceMemoryUsage.WithLabelValues("allocated").Set(float64(stats.MemoryAllocated))
	performanceMemoryUsage.WithLabelValues("system").Set(float64(stats.MemorySystem))
	performanceMemoryUsage.WithLabelValues("heap_inuse").Set(float64(stats.MemoryHeapInUse))
	performanceMemoryUsage.WithLabelValues("stack_inuse").Set(float64(stats.MemoryStackInUse))
	performanceScrapingThroughput.Set(stats.ScrapingThroughput)
	performanceActiveConnections.Set(float64(stats.ActiveConnections))

	if stats.CPUUsagePercent >= 0 {
		performanceCPUUsage.Set(stats.CPUUsagePercent)
	}
}

func (pm *PerformanceMonitor) GetCurrentStats() PerformanceStats {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	now := time.Now()

	scrapeRate := 0.0
	timeSinceLastScrape := now.Sub(pm.lastScrapeTime).Seconds()
	if timeSinceLastScrape > 0 {
		scrapeRate = float64(pm.scrapeCount) / timeSinceLastScrape
	}

	var lastGCTime time.Time
	if memStats.LastGC > 0 {
		gcNano := memStats.LastGC
		if gcNano <= math.MaxInt64 {
			lastGCTime = time.Unix(0, int64(gcNano))
		} else {
			lastGCTime = time.Unix(0, math.MaxInt64)
		}
	}

	return PerformanceStats{
		Timestamp:          now,
		Goroutines:         runtime.NumGoroutine(),
		MemoryAllocated:    memStats.Alloc,
		MemorySystem:       memStats.Sys,
		MemoryHeapInUse:    memStats.HeapInuse,
		MemoryStackInUse:   memStats.StackInuse,
		GCCycles:           memStats.NumGC,
		GCPauseTotal:       memStats.PauseTotalNs,
		LastGCTime:         lastGCTime,
		CPUUsagePercent:    pm.calculateCPUUsage(),
		ScrapingThroughput: scrapeRate,
		ActiveConnections:  pm.getActiveConnections(),
	}
}

func (pm *PerformanceMonitor) RecordScrapeOperation() {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	pm.scrapeCount++
}

func (pm *PerformanceMonitor) ResetScrapeStats() {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	pm.scrapeCount = 0
	pm.lastScrapeTime = time.Now()
}

func (pm *PerformanceMonitor) calculateCPUUsage() float64 {
	// This is a simplified CPU usage calculation
	// In a real implementation, you might want to use system-specific libraries
	// or track CPU time over intervals
	return -1 // Indicates unavailable
}

func (pm *PerformanceMonitor) getActiveConnections() int {
	// This would need to be implemented based on your connection tracking
	// For now, returning 0 as placeholder
	return 0
}

func (pm *PerformanceMonitor) SetActiveConnections(count int) {
	performanceActiveConnections.Set(float64(count))
}

func (pm *PerformanceMonitor) RecordQueueDepth(queueType string, depth int) {
	performanceQueueDepth.WithLabelValues(queueType).Set(float64(depth))
}

func (pm *PerformanceMonitor) IsHealthy() bool {
	stats := pm.GetCurrentStats()

	if stats.Goroutines > 10000 {
		return false
	}

	const maxMemoryMB = 1000 * 1024 * 1024
	if stats.MemoryAllocated > maxMemoryMB {
		return false
	}

	if stats.CPUUsagePercent > 90 {
		return false
	}

	return true
}

func (pm *PerformanceMonitor) GetHealthStatus() map[string]interface{} {
	stats := pm.GetCurrentStats()

	status := map[string]interface{}{
		"healthy":             pm.IsHealthy(),
		"timestamp":           stats.Timestamp,
		"goroutines":          stats.Goroutines,
		"memory_allocated_mb": float64(stats.MemoryAllocated) / 1024 / 1024,
		"memory_system_mb":    float64(stats.MemorySystem) / 1024 / 1024,
		"gc_cycles":           stats.GCCycles,
		"scraping_throughput": stats.ScrapingThroughput,
		"active_connections":  stats.ActiveConnections,
	}

	if stats.CPUUsagePercent >= 0 {
		status["cpu_usage_percent"] = stats.CPUUsagePercent
	}

	return status
}

type AlertThresholds struct {
	MaxGoroutines     int     `json:"max_goroutines"`
	MaxMemoryMB       float64 `json:"max_memory_mb"`
	MaxCPUPercent     float64 `json:"max_cpu_percent"`
	MinThroughput     float64 `json:"min_throughput_dps"`
	MaxResponseTimeMS int64   `json:"max_response_time_ms"`
}

func DefaultAlertThresholds() AlertThresholds {
	return AlertThresholds{
		MaxGoroutines:     1000,
		MaxMemoryMB:       500,
		MaxCPUPercent:     80,
		MinThroughput:     1.0,
		MaxResponseTimeMS: 5000,
	}
}

func (pm *PerformanceMonitor) CheckAlerts(thresholds AlertThresholds) []string {
	stats := pm.GetCurrentStats()
	var alerts []string

	if stats.Goroutines > thresholds.MaxGoroutines {
		alerts = append(alerts, "High goroutine count")
	}

	memoryMB := float64(stats.MemoryAllocated) / 1024 / 1024
	if memoryMB > thresholds.MaxMemoryMB {
		alerts = append(alerts, "High memory usage")
	}

	if stats.CPUUsagePercent > thresholds.MaxCPUPercent {
		alerts = append(alerts, "High CPU usage")
	}

	if stats.ScrapingThroughput < thresholds.MinThroughput && stats.ScrapingThroughput > 0 {
		alerts = append(alerts, "Low scraping throughput")
	}

	return alerts
}
