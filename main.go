package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

var (
	version   = "dev"
	buildTime = "unknown"
)

// setupLogger configures structured logging based on configuration
func setupLogger(cfg Config) {
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
	cfg := loadConfig()

	// Setup structured logging
	setupLogger(cfg)

	// Validate configuration early
	if err := cfg.Validate(); err != nil {
		slog.Error("Configuration validation failed", "error", err)
		os.Exit(1)
	}

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

	var err error
	if cfg.UseTsnet {
		if cfg.TsnetHostname == "" {
			cfg.TsnetHostname = "tsmetrics"
		}
		// Enforce exporter tag requirement if requested
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
		err = runWithTsnet(cfg, ctx)
	} else {
		slog.Info("starting standalone", "port", cfg.Port)
		err = runStandalone(cfg, ctx)
	}

	if err != nil {
		slog.Error("shutdown with error", "error", err)
	} else {
		slog.Info("shutdown complete")
	}
}
