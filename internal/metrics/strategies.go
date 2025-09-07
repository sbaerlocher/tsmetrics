// Package metrics provides different strategies for scraping device metrics,
// including sequential, parallel, priority-based, and adaptive approaches
// to optimize performance and reliability based on system conditions.
package metrics

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/sbaerlocher/tsmetrics/internal/errors"
	"github.com/sbaerlocher/tsmetrics/internal/types"
	"github.com/sbaerlocher/tsmetrics/pkg/device"
)

var (
	scrapingStrategyExecutionDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "tsmetrics_scraping_strategy_duration_seconds",
			Help:    "Time spent executing scraping strategies",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"strategy", "status"},
	)

	scrapingStrategyDevicesProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tsmetrics_scraping_strategy_devices_processed_total",
			Help: "Number of devices processed by scraping strategies",
		},
		[]string{"strategy"},
	)

	scrapingStrategyErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tsmetrics_scraping_strategy_errors_total",
			Help: "Number of errors in scraping strategies",
		},
		[]string{"strategy", "error_type"},
	)
)

type ScrapingStrategy interface {
	Execute(ctx context.Context, devices []device.Device) error
	Name() string
	Validate() error
}

type SequentialStrategy struct {
	collector MetricCollector
	interval  time.Duration
	timeout   time.Duration
}

type ParallelStrategy struct {
	collector   MetricCollector
	interval    time.Duration
	timeout     time.Duration
	concurrency int
}

type PriorityStrategy struct {
	collector      MetricCollector
	interval       time.Duration
	timeout        time.Duration
	priorityGroups map[string][]types.DeviceID
	groupIntervals map[string]time.Duration
}

type AdaptiveStrategy struct {
	collector       MetricCollector
	baseInterval    time.Duration
	timeout         time.Duration
	maxConcurrency  int
	loadThreshold   float64
	performanceHist map[types.DeviceID]*DevicePerformance
	mutex           sync.RWMutex
}

type DevicePerformance struct {
	AverageLatency    time.Duration
	ErrorRate         float64
	LastSuccess       time.Time
	ConsecutiveErrors int
}

func NewSequentialStrategy(collector MetricCollector, interval, timeout time.Duration) *SequentialStrategy {
	return &SequentialStrategy{
		collector: collector,
		interval:  interval,
		timeout:   timeout,
	}
}

func (s *SequentialStrategy) Name() string {
	return "sequential"
}

func (s *SequentialStrategy) Validate() error {
	if s.collector == nil {
		return errors.ErrInvalidCollector
	}
	if s.interval <= 0 {
		return errors.ErrInvalidInterval
	}
	if s.timeout <= 0 {
		return errors.ErrInvalidTimeout
	}
	return nil
}

func (s *SequentialStrategy) Execute(ctx context.Context, devices []device.Device) error {
	start := time.Now()
	defer func() {
		scrapingStrategyExecutionDuration.WithLabelValues(s.Name(), "complete").Observe(time.Since(start).Seconds())
	}()

	for _, dev := range devices {
		select {
		case <-ctx.Done():
			scrapingStrategyErrors.WithLabelValues(s.Name(), "context_cancelled").Inc()
			return ctx.Err()
		default:
		}

		deviceCtx, cancel := context.WithTimeout(ctx, s.timeout)
		err := s.collector.CollectDeviceMetrics(deviceCtx, dev)
		cancel()

		if err != nil {
			scrapingStrategyErrors.WithLabelValues(s.Name(), "collection_error").Inc()
			continue
		}

		scrapingStrategyDevicesProcessed.WithLabelValues(s.Name()).Inc()

		if len(devices) > 1 {
			time.Sleep(s.interval)
		}
	}

	return nil
}

func NewParallelStrategy(collector MetricCollector, interval, timeout time.Duration, concurrency int) *ParallelStrategy {
	return &ParallelStrategy{
		collector:   collector,
		interval:    interval,
		timeout:     timeout,
		concurrency: concurrency,
	}
}

func (s *ParallelStrategy) Name() string {
	return "parallel"
}

func (s *ParallelStrategy) Validate() error {
	if s.collector == nil {
		return errors.ErrInvalidCollector
	}
	if s.interval <= 0 {
		return errors.ErrInvalidInterval
	}
	if s.timeout <= 0 {
		return errors.ErrInvalidTimeout
	}
	if s.concurrency <= 0 {
		return errors.ErrInvalidConcurrency
	}
	return nil
}

func (s *ParallelStrategy) Execute(ctx context.Context, devices []device.Device) error {
	start := time.Now()
	defer func() {
		scrapingStrategyExecutionDuration.WithLabelValues(s.Name(), "complete").Observe(time.Since(start).Seconds())
	}()

	semaphore := make(chan struct{}, s.concurrency)
	var wg sync.WaitGroup
	errorsChan := make(chan error, len(devices))

	for _, dev := range devices {
		wg.Add(1)
		go func(device device.Device) {
			defer wg.Done()

			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				errorsChan <- ctx.Err()
				return
			}

			deviceCtx, cancel := context.WithTimeout(ctx, s.timeout)
			defer cancel()

			err := s.collector.CollectDeviceMetrics(deviceCtx, device)
			if err != nil {
				scrapingStrategyErrors.WithLabelValues(s.Name(), "collection_error").Inc()
				errorsChan <- err
				return
			}

			scrapingStrategyDevicesProcessed.WithLabelValues(s.Name()).Inc()
		}(dev)
	}

	wg.Wait()
	close(errorsChan)

	var lastErr error
	for err := range errorsChan {
		lastErr = err
	}

	return lastErr
}

func NewPriorityStrategy(collector MetricCollector, interval, timeout time.Duration) *PriorityStrategy {
	return &PriorityStrategy{
		collector:      collector,
		interval:       interval,
		timeout:        timeout,
		priorityGroups: make(map[string][]types.DeviceID),
		groupIntervals: make(map[string]time.Duration),
	}
}

func (s *PriorityStrategy) Name() string {
	return "priority"
}

func (s *PriorityStrategy) Validate() error {
	if s.collector == nil {
		return errors.ErrInvalidCollector
	}
	if s.interval <= 0 {
		return errors.ErrInvalidInterval
	}
	if s.timeout <= 0 {
		return errors.ErrInvalidTimeout
	}
	return nil
}

func (s *PriorityStrategy) AddPriorityGroup(groupName string, deviceIDs []types.DeviceID, interval time.Duration) {
	s.priorityGroups[groupName] = deviceIDs
	s.groupIntervals[groupName] = interval
}

func (s *PriorityStrategy) Execute(ctx context.Context, devices []device.Device) error {
	start := time.Now()
	defer func() {
		scrapingStrategyExecutionDuration.WithLabelValues(s.Name(), "complete").Observe(time.Since(start).Seconds())
	}()

	deviceMap := make(map[types.DeviceID]device.Device)
	for _, dev := range devices {
		deviceMap[dev.ID] = dev
	}

	for groupName, deviceIDs := range s.priorityGroups {
		interval := s.groupIntervals[groupName]

		for _, deviceID := range deviceIDs {
			if dev, exists := deviceMap[deviceID]; exists {
				select {
				case <-ctx.Done():
					scrapingStrategyErrors.WithLabelValues(s.Name(), "context_cancelled").Inc()
					return ctx.Err()
				default:
				}

				deviceCtx, cancel := context.WithTimeout(ctx, s.timeout)
				err := s.collector.CollectDeviceMetrics(deviceCtx, dev)
				cancel()

				if err != nil {
					scrapingStrategyErrors.WithLabelValues(s.Name(), "collection_error").Inc()
					continue
				}

				scrapingStrategyDevicesProcessed.WithLabelValues(s.Name()).Inc()
				time.Sleep(interval)
			}
		}
	}

	return nil
}

func NewAdaptiveStrategy(collector MetricCollector, baseInterval, timeout time.Duration, maxConcurrency int, loadThreshold float64) *AdaptiveStrategy {
	return &AdaptiveStrategy{
		collector:       collector,
		baseInterval:    baseInterval,
		timeout:         timeout,
		maxConcurrency:  maxConcurrency,
		loadThreshold:   loadThreshold,
		performanceHist: make(map[types.DeviceID]*DevicePerformance),
	}
}

func (s *AdaptiveStrategy) Name() string {
	return "adaptive"
}

func (s *AdaptiveStrategy) Validate() error {
	if s.collector == nil {
		return errors.ErrInvalidCollector
	}
	if s.baseInterval <= 0 {
		return errors.ErrInvalidInterval
	}
	if s.timeout <= 0 {
		return errors.ErrInvalidTimeout
	}
	if s.maxConcurrency <= 0 {
		return errors.ErrInvalidConcurrency
	}
	if s.loadThreshold <= 0 || s.loadThreshold >= 1 {
		return errors.ErrInvalidLoadThreshold
	}
	return nil
}

func (s *AdaptiveStrategy) Execute(ctx context.Context, devices []device.Device) error {
	start := time.Now()
	defer func() {
		scrapingStrategyExecutionDuration.WithLabelValues(s.Name(), "complete").Observe(time.Since(start).Seconds())
	}()

	deviceGroups := s.categorizeDevices(devices)

	for priority, devs := range deviceGroups {
		concurrency := s.calculateConcurrency(priority)
		err := s.processDeviceGroup(ctx, devs, concurrency)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *AdaptiveStrategy) categorizeDevices(devices []device.Device) map[string][]device.Device {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	groups := map[string][]device.Device{
		"high":   {},
		"medium": {},
		"low":    {},
	}

	for _, dev := range devices {
		perf, exists := s.performanceHist[dev.ID]
		if !exists {
			groups["medium"] = append(groups["medium"], dev)
			continue
		}

		if perf.ConsecutiveErrors > 3 || perf.ErrorRate > s.loadThreshold {
			groups["low"] = append(groups["low"], dev)
		} else if perf.AverageLatency < 100*time.Millisecond && perf.ErrorRate < 0.1 {
			groups["high"] = append(groups["high"], dev)
		} else {
			groups["medium"] = append(groups["medium"], dev)
		}
	}

	return groups
}

func (s *AdaptiveStrategy) calculateConcurrency(priority string) int {
	switch priority {
	case "high":
		return s.maxConcurrency
	case "medium":
		return s.maxConcurrency / 2
	case "low":
		return 1
	default:
		return 1
	}
}

func (s *AdaptiveStrategy) processDeviceGroup(ctx context.Context, devices []device.Device, concurrency int) error {
	if len(devices) == 0 {
		return nil
	}

	semaphore := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, dev := range devices {
		wg.Add(1)
		go func(device device.Device) {
			defer wg.Done()

			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				return
			}

			s.collectWithPerformanceTracking(ctx, device)
		}(dev)
	}

	wg.Wait()
	return nil
}

func (s *AdaptiveStrategy) collectWithPerformanceTracking(ctx context.Context, dev device.Device) {
	startTime := time.Now()
	deviceCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	err := s.collector.CollectDeviceMetrics(deviceCtx, dev)
	latency := time.Since(startTime)

	s.updatePerformanceHistory(dev.ID, latency, err)

	if err != nil {
		scrapingStrategyErrors.WithLabelValues(s.Name(), "collection_error").Inc()
		return
	}

	scrapingStrategyDevicesProcessed.WithLabelValues(s.Name()).Inc()
}

func (s *AdaptiveStrategy) updatePerformanceHistory(deviceID types.DeviceID, latency time.Duration, err error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	perf, exists := s.performanceHist[deviceID]
	if !exists {
		perf = &DevicePerformance{}
		s.performanceHist[deviceID] = perf
	}

	perf.AverageLatency = (perf.AverageLatency + latency) / 2

	if err != nil {
		perf.ConsecutiveErrors++
		perf.ErrorRate = (perf.ErrorRate*0.9 + 1.0*0.1)
	} else {
		perf.ConsecutiveErrors = 0
		perf.LastSuccess = time.Now()
		perf.ErrorRate = perf.ErrorRate * 0.9
	}
}

type StrategyManager struct {
	strategies map[string]ScrapingStrategy
	current    ScrapingStrategy
	mutex      sync.RWMutex
}

func NewStrategyManager() *StrategyManager {
	return &StrategyManager{
		strategies: make(map[string]ScrapingStrategy),
	}
}

func (sm *StrategyManager) RegisterStrategy(strategy ScrapingStrategy) error {
	if err := strategy.Validate(); err != nil {
		return fmt.Errorf("invalid strategy %s: %w", strategy.Name(), err)
	}

	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.strategies[strategy.Name()] = strategy
	return nil
}

func (sm *StrategyManager) SetActiveStrategy(name string) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	strategy, exists := sm.strategies[name]
	if !exists {
		return fmt.Errorf("strategy %s not found", name)
	}

	sm.current = strategy
	return nil
}

func (sm *StrategyManager) GetActiveStrategy() ScrapingStrategy {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.current
}

func (sm *StrategyManager) ListStrategies() []string {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	names := make([]string, 0, len(sm.strategies))
	for name := range sm.strategies {
		names = append(names, name)
	}
	return names
}
