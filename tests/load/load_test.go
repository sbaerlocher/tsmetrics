package load

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sbaerlocher/tsmetrics/internal/cache"
	"github.com/sbaerlocher/tsmetrics/internal/metrics"
	"github.com/sbaerlocher/tsmetrics/internal/types"
	"github.com/sbaerlocher/tsmetrics/pkg/device"
)

// Load Test Configuration
type LoadTestConfig struct {
	DeviceCount       int
	ConcurrentWorkers int
	TestDuration      time.Duration
	CacheTTL          time.Duration
	MaxMemoryMB       int64
}

type LoadTestResult struct {
	TotalOperations   int64
	SuccessOperations int64
	FailedOperations  int64
	AvgLatency        time.Duration
	MaxLatency        time.Duration
	MinLatency        time.Duration
	MemoryPeakMB      int64
	MemoryLeakMB      int64
	ErrorRate         float64
}

// Generate test devices with correct types
func generateTestDevices(count int) []device.Device {
	devices := make([]device.Device, count)
	for i := 0; i < count; i++ {
		id := types.DeviceID(fmt.Sprintf("device-%d", i))
		name := types.DeviceName(fmt.Sprintf("test-device-%d", i))
		tag1 := types.TagName("test")
		tag2 := types.TagName(fmt.Sprintf("group-%d", i%10))

		devices[i] = device.Device{
			ID:         id,
			Name:       name,
			Host:       fmt.Sprintf("device%d.example.com", i),
			Tags:       []types.TagName{tag1, tag2},
			Online:     true,
			Authorized: true,
		}
	}
	return devices
}

func BenchmarkDeviceCache_HighConcurrency(b *testing.B) {
	config := LoadTestConfig{
		DeviceCount:       1000,
		ConcurrentWorkers: 50,
		CacheTTL:          5 * time.Minute,
	}

	devices := generateTestDevices(config.DeviceCount)

	refreshFunc := func() ([]device.Device, error) {
		// Simulate API call delay
		time.Sleep(100 * time.Millisecond)
		return devices, nil
	}

	cache := cache.NewDeviceCache(config.CacheTTL, 2*config.CacheTTL, refreshFunc)

	var ops int64
	var errors int64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			start := time.Now()
			_, err := cache.GetDevices(false)

			atomic.AddInt64(&ops, 1)
			if err != nil {
				atomic.AddInt64(&errors, 1)
			}

			// Simulate processing time
			if time.Since(start) < 10*time.Millisecond {
				time.Sleep(10*time.Millisecond - time.Since(start))
			}
		}
	})

	errorRate := float64(errors) / float64(ops) * 100
	b.ReportMetric(float64(ops), "ops")
	b.ReportMetric(errorRate, "error_%")

	stats := cache.GetCacheStats()
	b.ReportMetric(stats.HitRatio*100, "hit_ratio_%")
}

func TestLoad_CacheOperationsUnderStress(t *testing.T) {
	config := LoadTestConfig{
		DeviceCount:       500,
		ConcurrentWorkers: 20,
		TestDuration:      15 * time.Second,
		CacheTTL:          1 * time.Minute,
		MaxMemoryMB:       200,
	}

	devices := generateTestDevices(config.DeviceCount)

	refreshFunc := func() ([]device.Device, error) {
		time.Sleep(50 * time.Millisecond) // Simulate API latency
		return devices, nil
	}

	cache := cache.NewDeviceCache(config.CacheTTL, 2*config.CacheTTL, refreshFunc)

	// Memory monitoring
	var initialMem, peakMem runtime.MemStats
	runtime.ReadMemStats(&initialMem)
	initialMemMB := int64(initialMem.Alloc) / 1024 / 1024

	var operations int64
	var errors int64
	var totalLatency int64

	ctx, cancel := context.WithTimeout(context.Background(), config.TestDuration)
	defer cancel()

	var wg sync.WaitGroup

	// Worker goroutines performing various cache operations
	for i := 0; i < config.ConcurrentWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				default:
					start := time.Now()

					// Mix of operations
					switch workerID % 5 {
					case 0:
						// Regular cache access
						_, err := cache.GetDevices(false)
						if err != nil {
							atomic.AddInt64(&errors, 1)
						}
					case 1:
						// Force refresh
						_, err := cache.GetDevices(true)
						if err != nil {
							atomic.AddInt64(&errors, 1)
						}
					case 2:
						// Individual device invalidation
						cache.InvalidateDevice(types.DeviceID(fmt.Sprintf("device-%d", workerID%config.DeviceCount)))
					case 3:
						// Cache stats access
						cache.GetCacheStats()
					case 4:
						// Cache clear (occasionally)
						if atomic.LoadInt64(&operations)%100 == 0 {
							cache.Clear()
						}
					}

					latency := time.Since(start).Nanoseconds()
					atomic.AddInt64(&operations, 1)
					atomic.AddInt64(&totalLatency, latency)

					// Memory check
					runtime.ReadMemStats(&peakMem)
					currentMemMB := int64(peakMem.Alloc) / 1024 / 1024
					if currentMemMB > config.MaxMemoryMB {
						t.Errorf("Memory usage exceeded limit: %d MB > %d MB", currentMemMB, config.MaxMemoryMB)
						cancel()
						return
					}

					time.Sleep(10 * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()

	// Final memory check
	runtime.GC()
	var finalMem runtime.MemStats
	runtime.ReadMemStats(&finalMem)
	finalMemMB := int64(finalMem.Alloc) / 1024 / 1024
	peakMemMB := int64(peakMem.Alloc) / 1024 / 1024

	result := LoadTestResult{
		TotalOperations:   operations,
		SuccessOperations: operations - errors,
		FailedOperations:  errors,
		MemoryPeakMB:      peakMemMB,
		MemoryLeakMB:      finalMemMB - initialMemMB,
		ErrorRate:         float64(errors) / float64(operations) * 100,
	}

	if operations > 0 {
		result.AvgLatency = time.Duration(totalLatency / operations)
	}

	t.Logf("Cache Load Test Results:")
	t.Logf("  Total Operations: %d", result.TotalOperations)
	t.Logf("  Success Rate: %.2f%%", float64(result.SuccessOperations)/float64(result.TotalOperations)*100)
	t.Logf("  Error Rate: %.2f%%", result.ErrorRate)
	t.Logf("  Avg Latency: %v", result.AvgLatency)
	t.Logf("  Peak Memory: %d MB", result.MemoryPeakMB)
	t.Logf("  Memory Leak: %d MB", result.MemoryLeakMB)

	// Get final cache stats
	stats := cache.GetCacheStats()
	t.Logf("  Cache Hit Ratio: %.2f%%", stats.HitRatio*100)
	t.Logf("  Cache Size: %d devices", stats.DeviceCount)

	// Assertions
	if result.ErrorRate > 10.0 {
		t.Errorf("Error rate too high: %.2f%%", result.ErrorRate)
	}

	if result.MemoryLeakMB > 30 {
		t.Errorf("Potential memory leak detected: %d MB growth", result.MemoryLeakMB)
	}

	if result.AvgLatency > 5*time.Second {
		t.Errorf("Average latency too high: %v", result.AvgLatency)
	}

	if operations < 100 {
		t.Errorf("Not enough operations completed: %d", operations)
	}
}

func TestLoad_PerformanceMonitorUnderStress(t *testing.T) {
	config := LoadTestConfig{
		ConcurrentWorkers: 15,
		TestDuration:      10 * time.Second,
		MaxMemoryMB:       150,
	}

	monitor := metrics.NewPerformanceMonitor(1 * time.Second)
	monitor.Start()
	defer monitor.Stop()

	var operations int64
	var errors int64

	ctx, cancel := context.WithTimeout(context.Background(), config.TestDuration)
	defer cancel()

	var wg sync.WaitGroup

	// Simulate high-frequency scraping operations
	for i := 0; i < config.ConcurrentWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				default:
					// Simulate a scraping operation
					time.Sleep(time.Duration(workerID%10+1) * time.Millisecond)

					// Record operation in performance monitor
					monitor.RecordScrapeOperation()

					// Check if monitor is still healthy
					if !monitor.IsHealthy() {
						atomic.AddInt64(&errors, 1)
					}

					atomic.AddInt64(&operations, 1)
					time.Sleep(50 * time.Millisecond)
				}
			}
		}(i)
	}

	// Monitor health status continuously
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				stats := monitor.GetCurrentStats()
				status := monitor.GetHealthStatus()

				t.Logf("Performance Stats - Goroutines: %d, Memory: %.1f MB, Healthy: %v",
					stats.Goroutines,
					float64(stats.MemoryAllocated)/1024/1024,
					status["healthy"])
			}
		}
	}()

	wg.Wait()

	// Final assessment
	finalStats := monitor.GetCurrentStats()
	finalHealth := monitor.GetHealthStatus()

	t.Logf("Performance Monitor Load Test Results:")
	t.Logf("  Total Operations: %d", operations)
	t.Logf("  Errors: %d", errors)
	t.Logf("  Final Goroutines: %d", finalStats.Goroutines)
	t.Logf("  Final Memory: %.1f MB", float64(finalStats.MemoryAllocated)/1024/1024)
	t.Logf("  Final Health: %v", finalHealth["healthy"])
	t.Logf("  Scraping Throughput: %.2f ops/sec", finalStats.ScrapingThroughput)

	// Assertions
	if errors > operations/10 {
		t.Errorf("Too many health check failures: %d/%d", errors, operations)
	}

	if finalStats.Goroutines > 1000 {
		t.Errorf("Too many goroutines: %d", finalStats.Goroutines)
	}

	memoryMB := float64(finalStats.MemoryAllocated) / 1024 / 1024
	if memoryMB > float64(config.MaxMemoryMB) {
		t.Errorf("Memory usage too high: %.1f MB", memoryMB)
	}

	if operations < 50 {
		t.Errorf("Not enough operations completed: %d", operations)
	}
}

func TestLoad_MemoryLeakDetection(t *testing.T) {
	// Test for memory leaks under sustained load
	devices := generateTestDevices(100)
	cache := cache.NewDeviceCache(30*time.Second, 1*time.Minute, func() ([]device.Device, error) {
		return devices, nil
	})

	var initialMem, peakMem, finalMem runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&initialMem)

	// Run operations for a sustained period
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var operations int64

	for i := 0; i < 8; i++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				default:
					cache.GetDevices(false)
					cache.InvalidateDevice(types.DeviceID("device-1"))
					cache.GetCacheStats()
					atomic.AddInt64(&operations, 1)
					time.Sleep(20 * time.Millisecond)
				}
			}
		}()
	}

	// Monitor memory during test
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runtime.ReadMemStats(&peakMem)
				currentMB := int64(peakMem.Alloc) / 1024 / 1024
				t.Logf("Current memory usage: %d MB", currentMB)
			}
		}
	}()

	<-ctx.Done()

	// Force cleanup and measure final memory
	runtime.GC()
	time.Sleep(200 * time.Millisecond)
	runtime.GC()
	runtime.ReadMemStats(&finalMem)

	initialMemMB := int64(initialMem.Alloc) / 1024 / 1024
	peakMemMB := int64(peakMem.Alloc) / 1024 / 1024
	finalMemMB := int64(finalMem.Alloc) / 1024 / 1024
	leakMB := finalMemMB - initialMemMB

	t.Logf("Memory Leak Detection Results:")
	t.Logf("  Operations: %d", atomic.LoadInt64(&operations))
	t.Logf("  Initial Memory: %d MB", initialMemMB)
	t.Logf("  Peak Memory: %d MB", peakMemMB)
	t.Logf("  Final Memory: %d MB", finalMemMB)
	t.Logf("  Memory Growth: %d MB", leakMB)

	// Allow some growth, but detect significant leaks
	if leakMB > 15 {
		t.Errorf("Potential memory leak detected: %d MB growth", leakMB)
	}

	if operations < 200 {
		t.Errorf("Not enough operations completed: %d", operations)
	}
}
