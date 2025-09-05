// Package main provides the TSMetrics application entry point.
// TSMetrics is a Tailscale device metrics collector that exposes Prometheus metrics
// for monitoring and alerting on Tailscale network devices.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

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

func main() {
	var showVersion bool
	var showHelp bool

	flag.BoolVar(&showVersion, "version", false, "show version information")
	flag.BoolVar(&showHelp, "help", false, "show help information")
	flag.Parse()

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
		fmt.Printf("  REQUIRE_EXPORTER_TAG  Require 'exporter' tag in tsnet tags (default: false)\n")
		fmt.Printf("  TSNET_TAGS            Comma-separated list of tsnet tags\n")
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
		if cfg.RequireExporterTag {
			has := false
			for _, t := range cfg.TsnetTags {
				if t == "exporter" {
					has = true
					break
				}
			}
			if !has {
				slog.Error("REQUIRE_EXPORTER_TAG is set but TSNET_TAGS does not include 'exporter'",
					"hint", "Set TSNET_TAGS=exporter or add the exporter tag via auth key/console")
				os.Exit(1)
			}
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
