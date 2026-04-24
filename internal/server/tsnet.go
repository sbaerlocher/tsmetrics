// Package server provides Tailscale tsnet integration for running
// the metrics server within the Tailscale network mesh.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"tailscale.com/ipn/store"
	_ "tailscale.com/ipn/store/kubestore" // Register "kube:" state store provider
	"tailscale.com/tsnet"

	"github.com/sbaerlocher/tsmetrics/internal/config"
	"github.com/sbaerlocher/tsmetrics/internal/metrics"
)

func RunWithTsnet(cfg config.Config, ctx context.Context, collector *metrics.Collector) error {
	server := &tsnet.Server{
		Hostname: cfg.TsnetHostname,
	}

	if cfg.TsnetStateSecret != "" {
		if cfg.TsnetStateDir != "" {
			slog.Warn("both TSNET_STATE_SECRET and TSNET_STATE_DIR set, using Secret store")
		}
		logf := func(format string, args ...any) {
			slog.Debug(fmt.Sprintf(format, args...))
		}
		stateStore, err := store.New(logf, "kube:"+cfg.TsnetStateSecret)
		if err != nil {
			return fmt.Errorf("failed to create kube state store: %w", err)
		}
		server.Store = stateStore
		slog.Info("tsnet using Kubernetes Secret as state store", "secret", cfg.TsnetStateSecret)
	} else {
		stateDir := config.SetupTsnetStateDir(cfg.TsnetStateDir)
		server.Dir = stateDir
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
	defer func() { _ = server.Close() }()

	mux := SetupRoutes()
	handler, err := applySecurityMiddleware(ctx, mux)
	if err != nil {
		return err
	}

	tsHTTPServer := createHTTPServer("", handler) // No Addr for tsnet server
	tsHTTPServer.Addr = ""                        // Clear Addr for tsnet

	localAddr := fmt.Sprintf("%s:%s", cfg.BindHost, cfg.Port)
	localHTTPServer := createHTTPServer(localAddr, handler)

	// Initialize health checker and background scraper
	initializeServerComponents(cfg, ctx, collector)

	errCh := make(chan error, 2)

	slog.Info("HTTP servers starting", "port", cfg.Port, "local_bind", cfg.BindHost)

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
