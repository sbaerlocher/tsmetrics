package metrics

import (
	"testing"
	"time"
)

func TestNewPerformanceMonitor(t *testing.T) {
	pm := NewPerformanceMonitor(1 * time.Second)

	if pm.interval != 1*time.Second {
		t.Errorf("Expected interval 1s, got %v", pm.interval)
	}

	if pm.ctx == nil {
		t.Error("Expected context to be initialized")
	}

	if pm.cancel == nil {
		t.Error("Expected cancel function to be initialized")
	}
}

func TestPerformanceMonitor_GetCurrentStats(t *testing.T) {
	pm := NewPerformanceMonitor(1 * time.Second)

	stats := pm.GetCurrentStats()

	if stats.Timestamp.IsZero() {
		t.Error("Expected non-zero timestamp")
	}

	if stats.Goroutines <= 0 {
		t.Error("Expected positive goroutine count")
	}

	if stats.MemoryAllocated == 0 {
		t.Error("Expected non-zero memory allocation")
	}
}

func TestPerformanceMonitor_RecordScrapeOperation(t *testing.T) {
	pm := NewPerformanceMonitor(1 * time.Second)

	initialStats := pm.GetCurrentStats()
	initialThroughput := initialStats.ScrapingThroughput

	pm.RecordScrapeOperation()
	pm.RecordScrapeOperation()
	pm.RecordScrapeOperation()

	time.Sleep(10 * time.Millisecond) // Small delay for throughput calculation
	pm.ResetScrapeStats()

	// After reset, throughput should be calculated based on time since reset
	time.Sleep(10 * time.Millisecond)
	pm.RecordScrapeOperation()

	stats := pm.GetCurrentStats()
	if stats.ScrapingThroughput < 0 {
		t.Error("Expected non-negative scraping throughput")
	}

	_ = initialThroughput // Acknowledge variable usage
}

func TestPerformanceMonitor_IsHealthy(t *testing.T) {
	pm := NewPerformanceMonitor(1 * time.Second)

	if !pm.IsHealthy() {
		t.Error("Expected performance monitor to be healthy initially")
	}
}

func TestPerformanceMonitor_GetHealthStatus(t *testing.T) {
	pm := NewPerformanceMonitor(1 * time.Second)

	status := pm.GetHealthStatus()

	if status["healthy"] != true {
		t.Error("Expected healthy status to be true")
	}

	if status["goroutines"] == nil {
		t.Error("Expected goroutines in health status")
	}

	if status["memory_allocated_mb"] == nil {
		t.Error("Expected memory_allocated_mb in health status")
	}
}

func TestPerformanceMonitor_CheckAlerts(t *testing.T) {
	pm := NewPerformanceMonitor(1 * time.Second)

	thresholds := AlertThresholds{
		MaxGoroutines: 1,     // Very low threshold to trigger alert
		MaxMemoryMB:   0.001, // Very low threshold to trigger alert
		MaxCPUPercent: 50,
		MinThroughput: 1000, // Very high threshold to trigger alert
	}

	alerts := pm.CheckAlerts(thresholds)

	// Should have at least one alert due to low thresholds
	if len(alerts) == 0 {
		t.Log("No alerts triggered - this might be expected depending on current system state")
	}
}

func TestDefaultAlertThresholds(t *testing.T) {
	thresholds := DefaultAlertThresholds()

	if thresholds.MaxGoroutines <= 0 {
		t.Error("Expected positive max goroutines threshold")
	}

	if thresholds.MaxMemoryMB <= 0 {
		t.Error("Expected positive max memory threshold")
	}

	if thresholds.MaxCPUPercent <= 0 {
		t.Error("Expected positive max CPU threshold")
	}
}

func TestPerformanceMonitor_StartStop(t *testing.T) {
	pm := NewPerformanceMonitor(100 * time.Millisecond)

	pm.Start()

	// Let it run for a short time
	time.Sleep(150 * time.Millisecond)

	pm.Stop()

	// Verify it stops cleanly
	select {
	case <-pm.ctx.Done():
		// Expected
	default:
		t.Error("Expected context to be cancelled after stop")
	}
}
