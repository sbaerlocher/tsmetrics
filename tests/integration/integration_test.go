package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sbaerlocher/tsmetrics/internal/cache"
	"github.com/sbaerlocher/tsmetrics/internal/config"
	"github.com/sbaerlocher/tsmetrics/internal/metrics"
	"github.com/sbaerlocher/tsmetrics/internal/server"
	"github.com/sbaerlocher/tsmetrics/internal/types"
	"github.com/sbaerlocher/tsmetrics/pkg/device"
)

type TestSuite struct {
	config      config.Config
	server      *httptest.Server
	collector   *metrics.Collector
	deviceCache *cache.DeviceCache
	strategyMgr *metrics.StrategyManager
	shutdownMgr *server.ShutdownManager
}

func setupTestSuite(t *testing.T) *TestSuite {
	cfg := config.Config{
		Port:                 "9100",
		ClientMetricsTimeout: 5 * time.Second,
		MaxConcurrentScrapes: 10,
		LogLevel:             "debug",
	}

	collector := metrics.NewCollector(cfg)
	deviceCache := cache.NewDeviceCache(
		1*time.Minute,
		5*time.Minute,
		func() ([]device.Device, error) {
			return []device.Device{
				{
					ID:     types.DeviceID("test-device-1"),
					Name:   types.DeviceName("test-device-1"),
					Host:   "192.168.1.10",
					Online: true,
				},
				{
					ID:     types.DeviceID("test-device-2"),
					Name:   types.DeviceName("test-device-2"),
					Host:   "192.168.1.11",
					Online: true,
				},
			}, nil
		},
	)

	strategyMgr := metrics.NewStrategyManager()
	seqStrategy := metrics.NewSequentialStrategy(collector, 100*time.Millisecond, 1*time.Second)
	parallelStrategy := metrics.NewParallelStrategy(collector, 100*time.Millisecond, 1*time.Second, 5)

	err := strategyMgr.RegisterStrategy(seqStrategy)
	if err != nil {
		t.Fatalf("Failed to register sequential strategy: %v", err)
	}

	err = strategyMgr.RegisterStrategy(parallelStrategy)
	if err != nil {
		t.Fatalf("Failed to register parallel strategy: %v", err)
	}

	err = strategyMgr.SetActiveStrategy("sequential")
	if err != nil {
		t.Fatalf("Failed to set active strategy: %v", err)
	}

	shutdownMgr := server.NewShutdownManager(30 * time.Second)

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, "# HELP test_metric Test metric\n# TYPE test_metric counter\ntest_metric 1\n")
		case "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, "OK")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	t.Cleanup(func() {
		httpServer.Close()
		shutdownMgr.Shutdown()
	})

	return &TestSuite{
		config:      cfg,
		server:      httpServer,
		collector:   collector,
		deviceCache: deviceCache,
		strategyMgr: strategyMgr,
		shutdownMgr: shutdownMgr,
	}
}

func TestFullWorkflow_DeviceDiscoveryToCacheToMetrics(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite := setupTestSuite(t)

	t.Run("device discovery and caching", func(t *testing.T) {
		devices, err := suite.deviceCache.GetDevices(false)
		if err != nil {
			t.Fatalf("Failed to get devices from cache: %v", err)
		}

		if len(devices) != 2 {
			t.Errorf("Expected 2 devices, got %d", len(devices))
		}

		stats := suite.deviceCache.GetCacheStats()
		if stats.MissCount != 1 {
			t.Errorf("Expected 1 cache miss, got %d", stats.MissCount)
		}

		devices2, err := suite.deviceCache.GetDevices(false)
		if err != nil {
			t.Fatalf("Failed to get devices from cache (second call): %v", err)
		}

		if len(devices2) != 2 {
			t.Errorf("Expected 2 devices from cache, got %d", len(devices2))
		}

		stats2 := suite.deviceCache.GetCacheStats()
		if stats2.HitCount != 1 {
			t.Errorf("Expected 1 cache hit, got %d", stats2.HitCount)
		}
	})

	t.Run("strategy execution", func(t *testing.T) {
		devices, err := suite.deviceCache.GetDevices(false)
		if err != nil {
			t.Fatalf("Failed to get devices: %v", err)
		}

		strategy := suite.strategyMgr.GetActiveStrategy()
		if strategy == nil {
			t.Fatal("No active strategy found")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err = strategy.Execute(ctx, devices)
		if err != nil {
			t.Errorf("Strategy execution failed: %v", err)
		}
	})
}

func TestFullWorkflow_ConfigReloadWithStrategySwitching(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite := setupTestSuite(t)

	t.Run("initial strategy verification", func(t *testing.T) {
		strategy := suite.strategyMgr.GetActiveStrategy()
		if strategy == nil {
			t.Fatal("No active strategy found")
		}

		if strategy.Name() != "sequential" {
			t.Errorf("Expected 'sequential' strategy, got %s", strategy.Name())
		}
	})

	t.Run("strategy switching", func(t *testing.T) {
		err := suite.strategyMgr.SetActiveStrategy("parallel")
		if err != nil {
			t.Fatalf("Failed to switch to parallel strategy: %v", err)
		}

		strategy := suite.strategyMgr.GetActiveStrategy()
		if strategy.Name() != "parallel" {
			t.Errorf("Expected 'parallel' strategy after switch, got %s", strategy.Name())
		}
	})

	t.Run("strategy execution after switch", func(t *testing.T) {
		devices, err := suite.deviceCache.GetDevices(false)
		if err != nil {
			t.Fatalf("Failed to get devices: %v", err)
		}

		strategy := suite.strategyMgr.GetActiveStrategy()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		start := time.Now()
		err = strategy.Execute(ctx, devices)
		duration := time.Since(start)

		if err != nil {
			t.Errorf("Parallel strategy execution failed: %v", err)
		}

		if duration > 5*time.Second {
			t.Errorf("Parallel strategy took too long: %v", duration)
		}
	})
}

func TestFullWorkflow_GracefulShutdownUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite := setupTestSuite(t)

	t.Run("setup load workers", func(t *testing.T) {
		var wg sync.WaitGroup
		var operations atomic.Int64

		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()

				worker := server.NewGracefulWorker(
					fmt.Sprintf("load-worker-%d", workerID),
					func(ctx context.Context) error {
						ticker := time.NewTicker(50 * time.Millisecond)
						defer ticker.Stop()

						for {
							select {
							case <-ctx.Done():
								return ctx.Err()
							case <-ticker.C:
								if _, err := suite.deviceCache.GetDevices(false); err != nil {
									return err
								}
								operations.Add(1)
							}
						}
					},
					suite.shutdownMgr,
				)

				worker.Start()
			}(i)
		}

		wg.Wait()
		time.Sleep(500 * time.Millisecond)
		if operations.Load() == 0 {
			t.Fatal("Workers performed no cache operations before shutdown")
		}

		shutdownStart := time.Now()
		suite.shutdownMgr.Shutdown()
		shutdownDuration := time.Since(shutdownStart)

		if shutdownDuration > 10*time.Second {
			t.Errorf("Graceful shutdown took too long: %v", shutdownDuration)
		}

	})
}

func TestConcurrentOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite := setupTestSuite(t)

	t.Run("concurrent cache operations", func(t *testing.T) {
		var wg sync.WaitGroup
		errors := make(chan error, 20)

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := suite.deviceCache.GetDevices(false)
				if err != nil {
					errors <- err
				}
			}()
		}

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := suite.deviceCache.GetDevices(true)
				if err != nil {
					errors <- err
				}
			}()
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			t.Errorf("Concurrent cache operation failed: %v", err)
		}

		stats := suite.deviceCache.GetCacheStats()
		if stats.HitCount+stats.MissCount == 0 {
			t.Error("Expected some cache operations to be recorded")
		}
	})

	t.Run("concurrent strategy executions", func(t *testing.T) {
		devices, err := suite.deviceCache.GetDevices(false)
		if err != nil {
			t.Fatalf("Failed to get devices: %v", err)
		}

		var wg sync.WaitGroup
		errors := make(chan error, 5)

		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				strategy := suite.strategyMgr.GetActiveStrategy()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				err := strategy.Execute(ctx, devices)
				if err != nil {
					errors <- err
				}
			}()
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			t.Errorf("Concurrent strategy execution failed: %v", err)
		}
	})
}

func TestErrorHandlingAndRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite := setupTestSuite(t)

	t.Run("cache error recovery", func(t *testing.T) {
		errorCache := cache.NewDeviceCache(
			1*time.Minute,
			5*time.Minute,
			func() ([]device.Device, error) {
				return nil, fmt.Errorf("simulated API error")
			},
		)

		devices, err := errorCache.GetDevices(false)
		if err == nil {
			t.Error("Expected error from cache with failing refresh function")
		}

		if devices != nil {
			t.Error("Expected nil devices on error")
		}

		successCache := cache.NewDeviceCache(
			1*time.Minute,
			5*time.Minute,
			func() ([]device.Device, error) {
				return []device.Device{
					{
						ID:     types.DeviceID("recovered-device"),
						Name:   types.DeviceName("recovered-device"),
						Host:   "192.168.1.100",
						Online: true,
					},
				}, nil
			},
		)

		devices, err = successCache.GetDevices(false)
		if err != nil {
			t.Errorf("Expected successful recovery, got error: %v", err)
		}

		if len(devices) != 1 {
			t.Errorf("Expected 1 device after recovery, got %d", len(devices))
		}
	})

	t.Run("strategy validation errors", func(t *testing.T) {
		invalidStrategy := metrics.NewSequentialStrategy(nil, 0, 0)
		err := suite.strategyMgr.RegisterStrategy(invalidStrategy)
		if err == nil {
			t.Error("Expected error when registering invalid strategy")
		}

		err = suite.strategyMgr.SetActiveStrategy("nonexistent")
		if err == nil {
			t.Error("Expected error when setting nonexistent strategy")
		}
	})
}

func TestPerformanceUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	suite := setupTestSuite(t)

	t.Run("high device count performance", func(t *testing.T) {
		devices := make([]device.Device, 100)
		for i := range devices {
			devices[i] = device.Device{
				ID:     types.DeviceID(fmt.Sprintf("load-device-%d", i)),
				Name:   types.DeviceName(fmt.Sprintf("load-device-%d", i)),
				Host:   fmt.Sprintf("192.168.1.%d", i+1),
				Online: true,
			}
		}

		highLoadCache := cache.NewDeviceCache(
			1*time.Minute,
			5*time.Minute,
			func() ([]device.Device, error) {
				return devices, nil
			},
		)

		start := time.Now()
		result, err := highLoadCache.GetDevices(false)
		duration := time.Since(start)

		if err != nil {
			t.Fatalf("High load cache operation failed: %v", err)
		}

		if len(result) != 100 {
			t.Errorf("Expected 100 devices, got %d", len(result))
		}

		if duration > 100*time.Millisecond {
			t.Errorf("High load cache operation took too long: %v", duration)
		}

		parallelStrategy := metrics.NewParallelStrategy(suite.collector, 10*time.Millisecond, 1*time.Second, 10)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		start = time.Now()
		err = parallelStrategy.Execute(ctx, result)
		duration = time.Since(start)

		if err != nil {
			t.Errorf("Parallel strategy execution with 100 devices failed: %v", err)
		}

		if duration > 15*time.Second {
			t.Errorf("Parallel strategy with 100 devices took too long: %v", duration)
		}
	})

	t.Run("memory usage stability", func(t *testing.T) {
		var initial, current runtime.MemStats
		runtime.ReadMemStats(&initial)

		for i := 0; i < 100; i++ {
			if _, err := suite.deviceCache.GetDevices(false); err != nil {
				t.Fatalf("Cache operation %d failed: %v", i, err)
			}

			if i%10 == 0 {
				runtime.ReadMemStats(&current)
				growth := float64(int64(current.Alloc)-int64(initial.Alloc)) / float64(initial.Alloc)
				if growth > 2.0 {
					t.Errorf("Memory usage grew by %.2f%% after %d operations", growth*100, i)
				}
			}
		}

		runtime.ReadMemStats(&current)
		growth := float64(int64(current.Alloc)-int64(initial.Alloc)) / float64(initial.Alloc)
		t.Logf("Memory usage after cache operations: initial=%d final=%d growth=%.2f%%",
			initial.Alloc, current.Alloc, growth*100)
	})
}

func init() {
	if os.Getenv("INTEGRATION_TESTS") == "" {
		_ = os.Setenv("INTEGRATION_TESTS", "1")
	}
}
