// Package server provides HTTP server functionality for TSMetrics.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/sbaerlocher/tsmetrics/internal/config"
	"github.com/sbaerlocher/tsmetrics/internal/health"
	"github.com/sbaerlocher/tsmetrics/internal/metrics"
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
	initializeHealthChecker(cfg, collector)
	StartBackgroundScraper(cfg, ctx, collector)
}

// SetupRoutes configures and returns the HTTP routes for the TSMetrics server.
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

// initializeHealthChecker sets up the health checker with appropriate components based on configuration
func initializeHealthChecker(cfg config.Config, collector *metrics.Collector) {
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
	env := strings.ToLower(os.Getenv("ENV"))
	host := "127.0.0.1" // DevSkim: ignore DS162092 - Localhost binding is intentional for development
	if env == "production" || env == "prod" {
		host = "0.0.0.0"
	}
	addr := fmt.Sprintf("%s:%s", host, cfg.Port)

	metrics.SetHTTPClientProvider(&metrics.StandardHTTPClientProvider{})
	slog.Info("HTTP client configured", "network", "standard")

	mux := SetupRoutes()
	srv := createHTTPServer(addr, mux)

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

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		if err := collector.UpdateMetrics("tailscale"); err != nil {
			if cfg.UseTsnet && metrics.CountTsnetStartupErrors(err) > 0 {
				slog.Info("Initial scrape waiting for connectivity")
			} else {
				slog.Error("initial update failed", "error", err)
			}
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := collector.UpdateMetrics("tailscale"); err != nil {
					if cfg.UseTsnet && metrics.CountTsnetStartupErrors(err) > 0 {
						slog.Debug("Scrape waiting for connectivity")
					} else {
						slog.Error("updateMetrics error", "error", err)
					}
				}
			}
		}
	}()
}
