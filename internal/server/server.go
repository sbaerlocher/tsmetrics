// Package server provides HTTP server functionality for TSMetrics.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/sbaerlocher/tsmetrics/internal/config"
	"github.com/sbaerlocher/tsmetrics/internal/health"
	"github.com/sbaerlocher/tsmetrics/internal/metrics"
	"github.com/sbaerlocher/tsmetrics/internal/security"
)

// createHTTPServer creates a configured HTTP server with standard timeouts.
func createHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

// initializeServerComponents sets up health checker and background scraping
func initializeServerComponents(cfg config.Config, ctx context.Context, collector *metrics.Collector) {
	initializeHealthChecker()
	StartBackgroundScraper(cfg, ctx, collector)
}

// SetupRoutes configures and returns the main HTTP router with health and metrics endpoints.
func SetupRoutes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", EnhancedHealthHandler)
	mux.HandleFunc("/debug", DebugHandler)

	// Kubernetes health endpoints
	mux.HandleFunc("/livez", LivenessHandler)
	mux.HandleFunc("/readyz", ReadinessHandler)
	mux.HandleFunc("/startupz", StartupHandler)
	mux.HandleFunc("/healthz", DetailedHealthHandler)

	return mux
}

// applySecurityMiddleware wraps the handler with rate limiting, optional token auth,
// and security headers. A background goroutine cleans up idle rate-limiter entries
// every 5 minutes for the lifetime of ctx.
func applySecurityMiddleware(ctx context.Context, handler http.Handler) http.Handler {
	rps := 10.0
	if v := os.Getenv("RATE_LIMIT_RPS"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			rps = f
		}
	}
	burst := 20
	if v := os.Getenv("RATE_LIMIT_BURST"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			burst = n
		}
	}
	rateLimiter := security.NewRateLimiter(rps, burst)

	// Cleanup idle per-IP limiters to prevent unbounded map growth (VULN-002)
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rateLimiter.Cleanup()
			}
		}
	}()

	h := security.RateLimitMiddleware(rateLimiter)(handler)

	// Token auth is opt-in: set METRICS_TOKEN to require a Bearer token on all
	// non-probe endpoints. AuthenticationMiddleware whitelists /health, /healthz,
	// /livez, /readyz, and /startupz so Kubernetes probes always pass.
	//
	// An operator who sets METRICS_TOKEN obviously wants auth to be enforced —
	// refusing to start on a weak/malformed token is safer than silently
	// accepting it and shipping trivially bypassable protection to production.
	if token := os.Getenv("METRICS_TOKEN"); token != "" {
		validator := security.NewInputValidator()
		if err := validator.ValidateToken(token); err != nil {
			slog.Error("METRICS_TOKEN rejected — refusing to start with weak auth", "error", err)
			os.Exit(1)
		}
		auth := security.NewAuthValidator()
		auth.AddValidToken(token)
		h = security.AuthenticationMiddleware(auth)(h)
		slog.Info("metrics endpoint protected with bearer token authentication")
	}

	return security.SecurityHeadersMiddleware(h)
}

// initializeHealthChecker sets up the health checker with appropriate components based on configuration
func initializeHealthChecker() {
	hc := health.NewHealthChecker()

	// Only register API health checker if API client is available and not using TARGET_DEVICES
	targetDevices := os.Getenv("TARGET_DEVICES")
	if targetDevices == "" {
		// No TARGET_DEVICES configured, we depend on API - create a simple health check
		hc.RegisterComponent(&simpleHealthChecker{name: "service"})
	}
	// If TARGET_DEVICES is configured, skip API health checker to allow API-independent operation

	SetHealthChecker(hc)
}

// simpleHealthChecker is a basic health checker that always returns healthy
type simpleHealthChecker struct {
	name string
}

func (s *simpleHealthChecker) ComponentName() string {
	return s.name
}

func (s *simpleHealthChecker) CheckHealth(ctx context.Context) error {
	return nil // Always healthy
}

// RunStandalone starts the HTTP server in standalone mode.
func RunStandalone(cfg config.Config, ctx context.Context, collector *metrics.Collector) error {
	addr := fmt.Sprintf("%s:%s", cfg.BindHost, cfg.Port)

	metrics.SetHTTPClientProvider(&metrics.StandardHTTPClientProvider{})
	slog.Info("HTTP client configured", "network", "standard")

	mux := SetupRoutes()
	srv := createHTTPServer(addr, applySecurityMiddleware(ctx, mux))

	// Initialize health checker and background scraper
	initializeServerComponents(cfg, ctx, collector)

	slog.Info("Server ready", "bind", addr)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// scrapeInterval and scrapeCycleTimeout bound the background scrape loop.
// scrapeCycleTimeout must stay >= scrapeInterval: the ticker fires every
// scrapeInterval, but time.Ticker silently drops ticks while runCycle is
// still executing, so a slow cycle turns into backpressure rather than
// overlapping runs.
const (
	scrapeInterval     = 30 * time.Second
	scrapeCycleTimeout = 60 * time.Second
)

func StartBackgroundScraper(cfg config.Config, ctx context.Context, collector *metrics.Collector) {
	go func() {
		if cfg.UseTsnet {
			slog.Info("Waiting for Tailscale connectivity")
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
			}
		}

		ticker := time.NewTicker(scrapeInterval)
		defer ticker.Stop()

		runCycle := func(label string) {
			cycleCtx, cancel := context.WithTimeout(ctx, scrapeCycleTimeout)
			defer cancel()
			if err := collector.UpdateMetrics(cycleCtx, "tailscale"); err != nil {
				if cfg.UseTsnet && metrics.CountTsnetStartupErrors(err) > 0 {
					slog.Debug(label+" waiting for connectivity", "tsnet_startup_errors", metrics.CountTsnetStartupErrors(err))
				} else {
					slog.Error(label+" failed", "error", err)
				}
			}
		}

		runCycle("initial update")

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runCycle("updateMetrics")
			}
		}
	}()
}
