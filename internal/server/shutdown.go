// Package server provides graceful shutdown management and lifecycle
// control for HTTP servers and background workers.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	shutdownDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "tsmetrics_shutdown_duration_seconds",
		Help:    "Time taken to gracefully shutdown the application",
		Buckets: []float64{0.1, 0.5, 1.0, 2.0, 5.0, 10.0, 30.0},
	})

	shutdownErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "tsmetrics_shutdown_errors_total",
		Help: "Number of errors during shutdown",
	}, []string{"component"})

	gracefulShutdownStatus = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "tsmetrics_graceful_shutdown_status",
		Help: "Status of graceful shutdown (1 = in progress, 0 = not active)",
	})
)

type ShutdownManager struct {
	timeout     time.Duration
	hooks       []ShutdownHook
	httpServers []*http.Server
	mutex       sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

type ShutdownHook struct {
	Name     string
	Priority int
	Timeout  time.Duration
	Handler  func(ctx context.Context) error
}

func NewShutdownManager(timeout time.Duration) *ShutdownManager {
	ctx, cancel := context.WithCancel(context.Background())

	return &ShutdownManager{
		timeout: timeout,
		hooks:   make([]ShutdownHook, 0),
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (sm *ShutdownManager) AddHTTPServer(server *http.Server) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	sm.httpServers = append(sm.httpServers, server)
}

func (sm *ShutdownManager) RegisterHook(hook ShutdownHook) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	if hook.Timeout == 0 {
		hook.Timeout = 30 * time.Second
	}

	sm.hooks = append(sm.hooks, hook)

	sm.sortHooksByPriority()
}

func (sm *ShutdownManager) sortHooksByPriority() {
	for i := 0; i < len(sm.hooks)-1; i++ {
		for j := 0; j < len(sm.hooks)-i-1; j++ {
			if sm.hooks[j].Priority > sm.hooks[j+1].Priority {
				sm.hooks[j], sm.hooks[j+1] = sm.hooks[j+1], sm.hooks[j]
			}
		}
	}
}

func (sm *ShutdownManager) ListenForShutdown() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGHUP,
		syscall.SIGQUIT,
	)

	go func() {
		sig := <-sigChan
		slog.Info("shutdown signal received", "signal", sig.String())
		sm.Shutdown()
	}()
}

func (sm *ShutdownManager) Shutdown() {
	start := time.Now()
	gracefulShutdownStatus.Set(1)
	defer func() {
		gracefulShutdownStatus.Set(0)
		shutdownDuration.Observe(time.Since(start).Seconds())
	}()

	slog.Info("starting graceful shutdown", "timeout", sm.timeout)

	ctx, cancel := context.WithTimeout(context.Background(), sm.timeout)
	defer cancel()

	sm.cancel()

	sm.shutdownHTTPServers(ctx)
	sm.executeShutdownHooks(ctx)

	sm.wg.Wait()

	slog.Info("graceful shutdown completed", "duration", time.Since(start))
}

func (sm *ShutdownManager) shutdownHTTPServers(ctx context.Context) {
	sm.mutex.RLock()
	servers := make([]*http.Server, len(sm.httpServers))
	copy(servers, sm.httpServers)
	sm.mutex.RUnlock()

	if len(servers) == 0 {
		return
	}

	slog.Info("shutting down HTTP servers", "count", len(servers))

	var wg sync.WaitGroup
	for _, server := range servers {
		wg.Add(1)
		go func(srv *http.Server) {
			defer wg.Done()

			shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

			if err := srv.Shutdown(shutdownCtx); err != nil {
				slog.Error("HTTP server shutdown error", "error", err)
				shutdownErrors.WithLabelValues("http_server").Inc()
				if closeErr := srv.Close(); closeErr != nil {
					slog.Error("HTTP server close error", "error", closeErr)
				}
			} else {
				slog.Info("HTTP server shutdown complete")
			}
		}(server)
	}

	wg.Wait()
}

func (sm *ShutdownManager) executeShutdownHooks(ctx context.Context) {
	sm.mutex.RLock()
	hooks := make([]ShutdownHook, len(sm.hooks))
	copy(hooks, sm.hooks)
	sm.mutex.RUnlock()

	if len(hooks) == 0 {
		return
	}

	slog.Info("executing shutdown hooks", "count", len(hooks))

	for _, hook := range hooks {
		select {
		case <-ctx.Done():
			slog.Warn("shutdown timeout reached, skipping remaining hooks")
			return
		default:
		}

		sm.executeHook(ctx, hook)
	}
}

func (sm *ShutdownManager) executeHook(ctx context.Context, hook ShutdownHook) {
	hookStart := time.Now()

	hookCtx, cancel := context.WithTimeout(ctx, hook.Timeout)
	defer cancel()

	slog.Debug("executing shutdown hook", "name", hook.Name, "timeout", hook.Timeout)

	done := make(chan error, 1)
	go func() {
		done <- hook.Handler(hookCtx)
	}()

	select {
	case err := <-done:
		if err != nil {
			slog.Error("shutdown hook failed", "name", hook.Name, "error", err)
			shutdownErrors.WithLabelValues(hook.Name).Inc()
		} else {
			slog.Debug("shutdown hook completed", "name", hook.Name, "duration", time.Since(hookStart))
		}
	case <-hookCtx.Done():
		slog.Warn("shutdown hook timeout", "name", hook.Name, "timeout", hook.Timeout)
		shutdownErrors.WithLabelValues(hook.Name).Inc()
	}
}

func (sm *ShutdownManager) Context() context.Context {
	return sm.ctx
}

func (sm *ShutdownManager) AddWorker() {
	sm.wg.Add(1)
}

func (sm *ShutdownManager) WorkerDone() {
	sm.wg.Done()
}

func (sm *ShutdownManager) IsShuttingDown() bool {
	select {
	case <-sm.ctx.Done():
		return true
	default:
		return false
	}
}

type GracefulWorker struct {
	name     string
	handler  func(ctx context.Context) error
	shutdown *ShutdownManager
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewGracefulWorker(name string, handler func(ctx context.Context) error, shutdown *ShutdownManager) *GracefulWorker {
	ctx, cancel := context.WithCancel(shutdown.Context())

	return &GracefulWorker{
		name:     name,
		handler:  handler,
		shutdown: shutdown,
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (gw *GracefulWorker) Start() {
	gw.shutdown.AddWorker()
	go func() {
		defer gw.shutdown.WorkerDone()
		defer gw.cancel()

		slog.Debug("starting graceful worker", "name", gw.name)

		err := gw.handler(gw.ctx)
		if err != nil && err != context.Canceled {
			slog.Error("graceful worker failed", "name", gw.name, "error", err)
		} else {
			slog.Debug("graceful worker completed", "name", gw.name)
		}
	}()
}

func (gw *GracefulWorker) Stop() {
	gw.cancel()
}

type HealthChecker struct {
	name     string
	interval time.Duration
	timeout  time.Duration
	handler  func(ctx context.Context) error
	shutdown *ShutdownManager
	healthy  bool
	mutex    sync.RWMutex
}

func NewHealthChecker(name string, interval, timeout time.Duration, handler func(ctx context.Context) error, shutdown *ShutdownManager) *HealthChecker {
	return &HealthChecker{
		name:     name,
		interval: interval,
		timeout:  timeout,
		handler:  handler,
		shutdown: shutdown,
		healthy:  true,
	}
}

func (hc *HealthChecker) Start() {
	worker := NewGracefulWorker(hc.name+"-health-checker", hc.run, hc.shutdown)
	worker.Start()
}

func (hc *HealthChecker) run(ctx context.Context) error {
	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			hc.checkHealth(ctx)
		}
	}
}

func (hc *HealthChecker) checkHealth(ctx context.Context) {
	checkCtx, cancel := context.WithTimeout(ctx, hc.timeout)
	defer cancel()

	err := hc.handler(checkCtx)

	hc.mutex.Lock()
	hc.healthy = (err == nil)
	hc.mutex.Unlock()

	if err != nil {
		slog.Debug("health check failed", "name", hc.name, "error", err)
	}
}

func (hc *HealthChecker) IsHealthy() bool {
	hc.mutex.RLock()
	defer hc.mutex.RUnlock()
	return hc.healthy
}

func CreateDefaultShutdownHooks(performanceMonitor interface{}, cache interface{}) []ShutdownHook {
	hooks := []ShutdownHook{
		{
			Name:     "stop-new-requests",
			Priority: 1,
			Timeout:  5 * time.Second,
			Handler: func(ctx context.Context) error {
				slog.Info("stopping acceptance of new requests")
				return nil
			},
		},
		{
			Name:     "drain-active-connections",
			Priority: 2,
			Timeout:  30 * time.Second,
			Handler: func(ctx context.Context) error {
				slog.Info("draining active connections")
				time.Sleep(2 * time.Second)
				return nil
			},
		},
		{
			Name:     "stop-background-workers",
			Priority: 3,
			Timeout:  20 * time.Second,
			Handler: func(ctx context.Context) error {
				slog.Info("stopping background workers")
				return nil
			},
		},
		{
			Name:     "flush-metrics",
			Priority: 4,
			Timeout:  10 * time.Second,
			Handler: func(ctx context.Context) error {
				slog.Info("flushing final metrics")
				return nil
			},
		},
		{
			Name:     "cleanup-resources",
			Priority: 5,
			Timeout:  15 * time.Second,
			Handler: func(ctx context.Context) error {
				slog.Info("cleaning up resources")
				return nil
			},
		},
	}

	return hooks
}
