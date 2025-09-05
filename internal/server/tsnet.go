package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"tailscale.com/tsnet"

	"github.com/sbaerlocher/tsmetrics/internal/config"
	"github.com/sbaerlocher/tsmetrics/internal/metrics"
)

func getLocalBindHost() string {
	env := strings.ToLower(os.Getenv("ENV"))
	if env == "production" || env == "prod" {
		return "0.0.0.0"
	}
	return "127.0.0.1"
}

func RunWithTsnet(cfg config.Config, ctx context.Context, collector *metrics.Collector) error {
	stateDir := config.SetupTsnetStateDir(cfg.TsnetStateDir)

	server := &tsnet.Server{
		Hostname: cfg.TsnetHostname,
		Dir:      stateDir,
	}

	if cfg.TsnetAuthKey != "" {
		server.AuthKey = cfg.TsnetAuthKey
		slog.Info("Tailscale authentication configured", "mode", "auth_key")
		if len(cfg.TsnetOwnTags) > 0 {
			slog.Info("Tailscale tags expected", "tags", cfg.TsnetOwnTags)
		}
	} else if len(cfg.TsnetOwnTags) > 0 {
		slog.Warn("tsnet configuration requires tags",
			"tags", cfg.TsnetOwnTags,
			"note", "auth_key required for tagged devices")
	}

	defer server.Close()

	tsnetProvider := &metrics.TsnetHTTPClientProvider{
		Server:  server,
		Timeout: cfg.ClientMetricsTimeout,
	}
	metrics.SetHTTPClientProvider(tsnetProvider)
	slog.Info("HTTP client configured for Tailscale network", "timeout", cfg.ClientMetricsTimeout)

	slog.Info("Tailscale connection establishing", "note", "device scraping may initially fail until connected")
	slog.Debug("Tailscale startup messages expected", "normal_errors", "routerIP/FetchRIB")

	listener, err := server.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		return fmt.Errorf("tsnet listen failed: %w", err)
	}

	mux := SetupRoutes()
	tsHTTPServer := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	host := getLocalBindHost()
	localAddr := fmt.Sprintf("%s:%s", host, cfg.Port)

	localHTTPServer := &http.Server{
		Addr:              localAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Initialize health checker with appropriate components
	initializeHealthChecker(cfg, collector)

	StartBackgroundScraper(cfg, ctx, collector)

	errCh := make(chan error, 2)

	slog.Info("HTTP servers starting", "port", cfg.Port, "local_bind", host)

	go func() {
		slog.Info("Tailscale server ready", "bind", ":"+cfg.Port)
		if err := tsHTTPServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("tsnet http serve failed: %w", err)
			return
		}
		errCh <- nil
	}()

	go func() {
		slog.Info("Local server ready", "bind", localAddr)
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
