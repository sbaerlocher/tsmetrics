// Package main provides the TSMetrics application entry point.
// TSMetrics is a Tailscale device metrics collector that exposes Prometheus metrics
// for monitoring and alerting on Tailscale network devices.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/sbaerlocher/tsmetrics/internal/config"
	"github.com/sbaerlocher/tsmetrics/internal/metrics"
	"github.com/sbaerlocher/tsmetrics/internal/server"
	tsversion "tailscale.com/version"
)

var (
	version   = "dev"
	buildTime = "unknown"
)

func setupLogger(cfg config.Config) {
	var handler slog.Handler
	level := slog.LevelInfo

	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	opts := &slog.HandlerOptions{Level: level}

	if cfg.LogFormat == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
}

// performHealthCheck performs a simple health check by trying to connect to the health endpoint
func performHealthCheck() error {
	cfg := config.Load()

	// Try to connect to the health endpoint
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	// Use configurable host for health checks
	host := os.Getenv("HEALTH_CHECK_HOST")
	if host == "" {
		host = "127.0.0.1" // DevSkim: ignore DS162092 - Localhost is appropriate for health checks
	}
	url := fmt.Sprintf("http://%s:%s/livez", host, cfg.Port)
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed: status %d", resp.StatusCode)
	}

	return nil
}

func main() {
	var showVersion bool
	var showHelp bool
	var healthCheck bool

	flag.BoolVar(&showVersion, "version", false, "show version information")
	flag.BoolVar(&showHelp, "help", false, "show help information")
	flag.BoolVar(&healthCheck, "health-check", false, "perform health check and exit")
	flag.Parse()

	if healthCheck {
		if err := performHealthCheck(); err != nil {
			slog.Error("Health check failed", "error", err)
			os.Exit(1)
		}
		slog.Info("Health check passed")
		os.Exit(0)
	}

	if showVersion {
		fmt.Printf("tsmetrics %s (built: %s)\n", version, buildTime)

		fmt.Printf("tailscale library: %s\n", tsversion.Long())

		if info, ok := debug.ReadBuildInfo(); ok {
			fmt.Printf("build info available: yes\n")
			fmt.Printf("go version: %s\n", info.GoVersion)
		} else {
			fmt.Printf("build info available: no\n")
		}

		os.Exit(0)
	}

	if showHelp {
		fmt.Printf("TSMetrics - Tailscale device metrics collector\n\n")
		fmt.Printf("Usage: tsmetrics [options]\n\n")
		fmt.Printf("Options:\n")
		flag.PrintDefaults()
		fmt.Printf("\nEnvironment variables:\n")
		fmt.Printf("  TAILSCALE_API_KEY     Tailscale API key for device discovery\n")
		fmt.Printf("  TAILSCALE_TAILNET     Tailscale tailnet name\n")
		fmt.Printf("  PORT                  Server port (default: 8080)\n")
		fmt.Printf("  LOG_LEVEL             Log level: debug, info, warn, error (default: info)\n")
		fmt.Printf("  LOG_FORMAT            Log format: text, json (default: text)\n")
		fmt.Printf("  USE_TSNET             Use tsnet for networking (default: false)\n")
		fmt.Printf("  TSNET_HOSTNAME        Hostname for tsnet (default: tsmetrics)\n")
		fmt.Printf("  TSNET_TAGS            Comma-separated list of tags for this tsnet instance\n")
		fmt.Printf("  SCRAPE_TAG            Tag filter for devices to scrape (empty = all devices)\n")
		fmt.Printf("\nFor more information, visit: https://github.com/sbaerlocher/tsmetrics\n")
		os.Exit(0)
	}

	cfg := config.Load()

	setupLogger(cfg)

	if err := cfg.Validate(); err != nil {
		slog.Error("Configuration validation failed", "error", err)
		os.Exit(1)
	}

	server.SetVersion(version, buildTime)

	slog.Info("Starting tsmetrics",
		"version", version,
		"build_time", buildTime,
		"use_tsnet", cfg.UseTsnet,
		"log_level", cfg.LogLevel,
		"log_format", cfg.LogFormat)

	if v := os.Getenv("VERSION"); v != "" {
		version = v
	}
	if bt := os.Getenv("BUILD_TIME"); bt != "" {
		buildTime = bt
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	collector := metrics.NewCollector(cfg)

	var err error
	if cfg.UseTsnet {
		if cfg.TsnetHostname == "" {
			cfg.TsnetHostname = "tsmetrics"
		}
		slog.Info("Tailscale mode", "hostname", cfg.TsnetHostname, "port", cfg.Port)
		slog.Info("Initial scraping errors are normal during connection setup")
		err = server.RunWithTsnet(cfg, ctx, collector)
	} else {
		slog.Info("Standalone mode", "port", cfg.Port)
		err = server.RunStandalone(cfg, ctx, collector)
	}

	if err != nil {
		slog.Error("Shutdown with error", "error", err)
	} else {
		slog.Info("Shutdown complete")
	}
}
