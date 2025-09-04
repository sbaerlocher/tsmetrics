package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"tailscale.com/tsnet"

	"github.com/sbaerlocher/tsmetrics/internal/config"
	"github.com/sbaerlocher/tsmetrics/internal/metrics"
)

func RunWithTsnet(cfg config.Config, ctx context.Context, collector *metrics.Collector) error {
	stateDir := config.SetupTsnetStateDir(cfg.TsnetStateDir)

	server := &tsnet.Server{
		Hostname: cfg.TsnetHostname,
		Dir:      stateDir,
	}

	if cfg.TsnetAuthKey != "" {
		server.AuthKey = cfg.TsnetAuthKey
		slog.Info("tsnet configured with auth key for automatic registration")
		if len(cfg.TsnetTags) > 0 {
			slog.Info("expected tags from auth key", "tags", cfg.TsnetTags)
		}
	} else if len(cfg.TsnetTags) > 0 {
		slog.Warn("TSNET_TAGS configured but no TS_AUTHKEY provided",
			"tags", cfg.TsnetTags,
			"hint", "Set TS_AUTHKEY with pre-tagged auth key for automatic tag assignment")
	}

	defer server.Close()

	tsnetProvider := &metrics.TsnetHTTPClientProvider{
		Server:  server,
		Timeout: cfg.ClientMetricsTimeout,
	}
	metrics.SetHTTPClientProvider(tsnetProvider)
	slog.Info("configured HTTP client to use Tailscale network for device scraping")

	slog.Info("tsnet will establish connection in background (device scraping may initially fail until connected)")
	slog.Debug("note: tsnet may log internal messages during startup (routerIP/FetchRIB errors are normal)")

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
	localAddr := fmt.Sprintf("127.0.0.1:%s", cfg.Port)
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

	slog.Info("starting HTTP servers", "tsnet_port", cfg.Port, "local_port", cfg.Port)

	go func() {
		slog.Info("starting tsnet HTTP server", "address", ":"+cfg.Port)
		if err := tsHTTPServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("tsnet http serve failed: %w", err)
			return
		}
		errCh <- nil
	}()

	go func() {
		slog.Info("starting local HTTP server", "address", localAddr)
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
