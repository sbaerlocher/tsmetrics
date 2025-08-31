package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"tailscale.com/tsnet"
)

func setupRoutes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", enhancedHealthHandler)
	mux.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		info := map[string]interface{}{
			"version":    version,
			"build_time": buildTime,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	})
	return mux
}

func runStandalone(cfg Config, ctx context.Context) error {
	env := strings.ToLower(os.Getenv("ENV"))
	host := "127.0.0.1"
	if env == "production" || env == "prod" {
		host = "0.0.0.0"
	}
	addr := fmt.Sprintf("%s:%s", host, cfg.Port)

	// Set up standard HTTP client provider for standalone mode
	httpClientProvider = &StandardHTTPClientProvider{
		timeout:       cfg.ClientMetricsTimeout,
		maxConcurrent: cfg.MaxConcurrentScrapes,
	}
	slog.Info("configured HTTP client for standard network access")

	mux := setupRoutes()
	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	startBackgroundScraper(cfg, ctx)

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

func runWithTsnet(cfg Config, ctx context.Context) error {
	stateDir := setupTsnetStateDir(cfg.TsnetStateDir)
	server := &tsnet.Server{
		Hostname: cfg.TsnetHostname,
		Dir:      stateDir,
	}
	defer server.Close()

	// Set up tsnet HTTP client provider for scraping over Tailscale network
	httpClientProvider = &TsnetHTTPClientProvider{
		server:  server,
		timeout: cfg.ClientMetricsTimeout,
	}
	slog.Info("configured HTTP client to use Tailscale network for device scraping")

	listener, err := server.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		return fmt.Errorf("tsnet listen failed: %w", err)
	}

	mux := setupRoutes()
	// HTTP server that will be served over tsnet listener
	tsHTTPServer := &http.Server{Handler: mux}
	// Local HTTP server bound to 127.0.0.1 so localhost requests work as well
	localAddr := fmt.Sprintf("127.0.0.1:%s", cfg.Port)
	localHTTPServer := &http.Server{Addr: localAddr, Handler: mux}

	startBackgroundScraper(cfg, ctx)

	errCh := make(chan error, 2)

	// serve over tsnet listener
	go func() {
		if err := tsHTTPServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("tsnet http serve failed: %w", err)
			return
		}
		errCh <- nil
	}()

	// serve on local loopback
	go func() {
		if err := localHTTPServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("local http serve failed: %w", err)
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = tsHTTPServer.Shutdown(shutdownCtx)
		_ = localHTTPServer.Shutdown(shutdownCtx)
		return nil
	case e := <-errCh:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = tsHTTPServer.Shutdown(shutdownCtx)
		_ = localHTTPServer.Shutdown(shutdownCtx)
		if e != nil {
			return e
		}
		return nil
	}
}

// enhancedHealthHandler provides detailed health information
func enhancedHealthHandler(w http.ResponseWriter, r *http.Request) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	status := map[string]interface{}{
		"status":         "ok",
		"version":        version,
		"build_time":     buildTime,
		"timestamp":      time.Now().Unix(),
		"memory_mb":      bToMb(m.Alloc),
		"goroutines":     runtime.NumGoroutine(),
		"last_scrape":    getLastScrapeTime(),
		"devices_online": getOnlineDeviceCount(),
		"uptime_seconds": getUptimeSeconds(),
	}

	// Test API connectivity if configured
	if tailnet := os.Getenv("TAILNET_NAME"); tailnet != "" {
		clientID := os.Getenv("OAUTH_CLIENT_ID")
		token := os.Getenv("OAUTH_TOKEN")

		if clientID != "" || token != "" {
			var apiClient *APIClient
			if clientID != "" {
				apiClient = NewAPIClient(clientID, os.Getenv("OAUTH_CLIENT_SECRET"), tailnet)
			} else {
				apiClient = NewAPIClientWithToken(token, tailnet)
			}

			// Quick connectivity test with timeout
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, err := apiClient.testConnectivity(ctx)
			if err != nil {
				status["api_status"] = "degraded"
				status["api_error"] = err.Error()
				w.WriteHeader(http.StatusServiceUnavailable)
			} else {
				status["api_status"] = "healthy"
			}
		} else {
			status["api_status"] = "not_configured"
		}
	} else {
		status["api_status"] = "not_configured"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// Helper functions for health metrics
func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}

var startTime = time.Now()

func getUptimeSeconds() int64 {
	return int64(time.Since(startTime).Seconds())
}

func getLastScrapeTime() int64 {
	// This would be updated by scraper when successful
	return time.Now().Unix() - 30 // Mock: 30 seconds ago
}

func getOnlineDeviceCount() int {
	// This would be tracked by the scraper
	return 5 // Mock value
}
