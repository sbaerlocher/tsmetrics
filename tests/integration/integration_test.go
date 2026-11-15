package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	devices     []device.Device
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
	devices := []device.Device{
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
	}

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
		devices:     devices,
		strategyMgr: strategyMgr,
		shutdownMgr: shutdownMgr,
	}
}

func TestFullWorkflow_StrategyExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite := setupTestSuite(t)

	t.Run("strategy execution", func(t *testing.T) {
		strategy := suite.strategyMgr.GetActiveStrategy()
		if strategy == nil {
			t.Fatal("No active strategy found")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err := strategy.Execute(ctx, suite.devices)
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
		strategy := suite.strategyMgr.GetActiveStrategy()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		start := time.Now()
		err := strategy.Execute(ctx, suite.devices)
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
			t.Fatal("Workers performed no operations before shutdown")
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

	t.Run("concurrent strategy executions", func(t *testing.T) {
		var wg sync.WaitGroup
		errors := make(chan error, 5)

		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				strategy := suite.strategyMgr.GetActiveStrategy()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				err := strategy.Execute(ctx, suite.devices)
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

		parallelStrategy := metrics.NewParallelStrategy(suite.collector, 10*time.Millisecond, 1*time.Second, 10)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		start := time.Now()
		err := parallelStrategy.Execute(ctx, devices)
		duration := time.Since(start)

		if err != nil {
			t.Errorf("Parallel strategy execution with 100 devices failed: %v", err)
		}

		if duration > 15*time.Second {
			t.Errorf("Parallel strategy with 100 devices took too long: %v", duration)
		}
	})
}

func init() {
	if os.Getenv("INTEGRATION_TESTS") == "" {
		_ = os.Setenv("INTEGRATION_TESTS", "1")
	}
}
