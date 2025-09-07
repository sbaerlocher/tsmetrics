package metrics

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sbaerlocher/tsmetrics/internal/types"
	"github.com/sbaerlocher/tsmetrics/pkg/device"
)

type mockMetricCollector struct {
	collectFunc func(ctx context.Context, device device.Device) error
	callCount   int64
}

func (m *mockMetricCollector) CollectDeviceMetrics(ctx context.Context, device device.Device) error {
	atomic.AddInt64(&m.callCount, 1)
	if m.collectFunc != nil {
		return m.collectFunc(ctx, device)
	}
	return nil
}

func (m *mockMetricCollector) getCallCount() int {
	return int(atomic.LoadInt64(&m.callCount))
}

func createTestDevices(count int) []device.Device {
	devices := make([]device.Device, count)
	for i := 0; i < count; i++ {
		devices[i] = device.Device{
			ID:     types.DeviceID(fmt.Sprintf("device%d", i+1)),
			Name:   types.DeviceName(fmt.Sprintf("Device %d", i+1)),
			Host:   "192.168.1.1",
			Online: true,
		}
	}
	return devices
}

func TestSequentialStrategy_Execute(t *testing.T) {
	collector := &mockMetricCollector{}
	strategy := NewSequentialStrategy(collector, 10*time.Millisecond, 100*time.Millisecond)

	devices := createTestDevices(3)
	ctx := context.Background()

	err := strategy.Execute(ctx, devices)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if collector.getCallCount() != 3 {
		t.Errorf("Expected 3 calls to collector, got %d", collector.getCallCount())
	}
}

func TestSequentialStrategy_Timeout(t *testing.T) {
	collector := &mockMetricCollector{
		collectFunc: func(ctx context.Context, device device.Device) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
				return nil
			}
		},
	}

	strategy := NewSequentialStrategy(collector, 10*time.Millisecond, 50*time.Millisecond)
	devices := createTestDevices(1)
	ctx := context.Background()

	start := time.Now()
	err := strategy.Execute(ctx, devices)
	duration := time.Since(start)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if duration > 100*time.Millisecond {
		t.Errorf("Expected quick timeout, but took %v", duration)
	}
}

func TestSequentialStrategy_ContextCancellation(t *testing.T) {
	collector := &mockMetricCollector{}
	strategy := NewSequentialStrategy(collector, 10*time.Millisecond, 100*time.Millisecond)

	devices := createTestDevices(5)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(25 * time.Millisecond)
		cancel()
	}()

	err := strategy.Execute(ctx, devices)
	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got %v", err)
	}

	if collector.getCallCount() >= 5 {
		t.Errorf("Expected fewer than 5 calls due to cancellation, got %d", collector.getCallCount())
	}
}

func TestParallelStrategy_Execute(t *testing.T) {
	collector := &mockMetricCollector{}
	strategy := NewParallelStrategy(collector, 10*time.Millisecond, 100*time.Millisecond, 2)

	devices := createTestDevices(4)
	ctx := context.Background()

	start := time.Now()
	err := strategy.Execute(ctx, devices)
	duration := time.Since(start)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if collector.getCallCount() != 4 {
		t.Errorf("Expected 4 calls to collector, got %d", collector.getCallCount())
	}

	if duration > 300*time.Millisecond {
		t.Errorf("Parallel execution took too long: %v", duration)
	}
}

func TestParallelStrategy_ConcurrencyLimit(t *testing.T) {
	collector := &mockMetricCollector{
		collectFunc: func(ctx context.Context, device device.Device) error {
			time.Sleep(50 * time.Millisecond)
			return nil
		},
	}

	strategy := NewParallelStrategy(collector, 10*time.Millisecond, 100*time.Millisecond, 2)
	devices := createTestDevices(4)
	ctx := context.Background()

	start := time.Now()
	err := strategy.Execute(ctx, devices)
	duration := time.Since(start)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	expectedMinDuration := 100 * time.Millisecond
	if duration < expectedMinDuration {
		t.Errorf("Expected at least %v due to concurrency limit, but took %v", expectedMinDuration, duration)
	}
}

func TestPriorityStrategy_Execute(t *testing.T) {
	collector := &mockMetricCollector{}
	strategy := NewPriorityStrategy(collector, 10*time.Millisecond, 100*time.Millisecond)

	devices := createTestDevices(3)

	strategy.AddPriorityGroup("high", []types.DeviceID{
		types.DeviceID("device1"),
		types.DeviceID("device2"),
	}, 5*time.Millisecond)

	strategy.AddPriorityGroup("low", []types.DeviceID{
		types.DeviceID("device3"),
	}, 20*time.Millisecond)

	ctx := context.Background()

	err := strategy.Execute(ctx, devices)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if collector.getCallCount() != 3 {
		t.Errorf("Expected 3 calls to collector, got %d", collector.getCallCount())
	}
}

func TestAdaptiveStrategy_Execute(t *testing.T) {
	collector := &mockMetricCollector{}
	strategy := NewAdaptiveStrategy(collector, 10*time.Millisecond, 100*time.Millisecond, 3, 0.5)

	devices := createTestDevices(5)
	ctx := context.Background()

	err := strategy.Execute(ctx, devices)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if collector.getCallCount() != 5 {
		t.Errorf("Expected 5 calls to collector, got %d", collector.getCallCount())
	}
}

func TestAdaptiveStrategy_PerformanceTracking(t *testing.T) {
	collector := &mockMetricCollector{
		collectFunc: func(ctx context.Context, device device.Device) error {
			if device.ID == types.DeviceID("device2") {
				return errors.New("simulated error")
			}
			return nil
		},
	}

	strategy := NewAdaptiveStrategy(collector, 10*time.Millisecond, 100*time.Millisecond, 3, 0.5)
	devices := createTestDevices(3)
	ctx := context.Background()

	strategy.Execute(ctx, devices)

	perf, exists := strategy.performanceHist[types.DeviceID("device2")]
	if !exists {
		t.Error("Expected performance history for device2")
	}

	if perf.ConsecutiveErrors != 1 {
		t.Errorf("Expected 1 consecutive error, got %d", perf.ConsecutiveErrors)
	}

	if perf.ErrorRate <= 0 {
		t.Errorf("Expected positive error rate, got %f", perf.ErrorRate)
	}
}

func TestStrategyManager_RegisterStrategy(t *testing.T) {
	manager := NewStrategyManager()
	collector := &mockMetricCollector{}

	strategy := NewSequentialStrategy(collector, 10*time.Millisecond, 100*time.Millisecond)

	err := manager.RegisterStrategy(strategy)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	strategies := manager.ListStrategies()
	if len(strategies) != 1 {
		t.Errorf("Expected 1 strategy, got %d", len(strategies))
	}

	if strategies[0] != "sequential" {
		t.Errorf("Expected 'sequential', got %s", strategies[0])
	}
}

func TestStrategyManager_SetActiveStrategy(t *testing.T) {
	manager := NewStrategyManager()
	collector := &mockMetricCollector{}

	strategy := NewSequentialStrategy(collector, 10*time.Millisecond, 100*time.Millisecond)
	manager.RegisterStrategy(strategy)

	err := manager.SetActiveStrategy("sequential")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	active := manager.GetActiveStrategy()
	if active == nil {
		t.Error("Expected active strategy to be set")
	}

	if active.Name() != "sequential" {
		t.Errorf("Expected 'sequential', got %s", active.Name())
	}
}

func TestStrategyManager_SetActiveStrategy_NotFound(t *testing.T) {
	manager := NewStrategyManager()

	err := manager.SetActiveStrategy("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent strategy")
	}
}

func TestStrategy_Validation(t *testing.T) {
	tests := []struct {
		name      string
		strategy  ScrapingStrategy
		expectErr bool
	}{
		{
			name:      "valid sequential strategy",
			strategy:  NewSequentialStrategy(&mockMetricCollector{}, 10*time.Millisecond, 100*time.Millisecond),
			expectErr: false,
		},
		{
			name:      "invalid sequential strategy - nil collector",
			strategy:  NewSequentialStrategy(nil, 10*time.Millisecond, 100*time.Millisecond),
			expectErr: true,
		},
		{
			name:      "invalid sequential strategy - zero interval",
			strategy:  NewSequentialStrategy(&mockMetricCollector{}, 0, 100*time.Millisecond),
			expectErr: true,
		},
		{
			name:      "invalid sequential strategy - zero timeout",
			strategy:  NewSequentialStrategy(&mockMetricCollector{}, 10*time.Millisecond, 0),
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.strategy.Validate()
			if tt.expectErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Expected no error but got %v", err)
			}
		})
	}
}
