package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/sbaerlocher/tsmetrics/internal/config"
	"github.com/sbaerlocher/tsmetrics/internal/metrics"
	"github.com/sbaerlocher/tsmetrics/internal/server"
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
	default:
		level = slog.LevelInfo
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
		slog.Info("starting with tsnet", "hostname", cfg.TsnetHostname, "port", cfg.Port)
		slog.Info("note: initial device scraping errors are normal while tsnet establishes connection")
		err = server.RunWithTsnet(cfg, ctx, collector)
	} else {
		slog.Info("starting standalone", "port", cfg.Port)
		err = server.RunStandalone(cfg, ctx, collector)
	}

	if err != nil {
		slog.Error("shutdown with error", "error", err)
	} else {
		slog.Info("shutdown complete")
	}
}
