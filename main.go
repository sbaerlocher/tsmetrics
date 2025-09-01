package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
)

var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	cfg := loadConfig()

	// Validate configuration early
	if err := cfg.Validate(); err != nil {
		log.Fatalf("configuration validation failed: %v", err)
	}

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
				log.Fatalf("REQUIRE_EXPORTER_TAG is set but TSNET_TAGS does not include 'exporter'. Set TSNET_TAGS=exporter or add the exporter tag via auth key/console.")
			}
		}
		log.Printf("starting with tsnet hostname=%s port=%s", cfg.TsnetHostname, cfg.Port)
		err = runWithTsnet(cfg, ctx)
	} else {
		log.Printf("starting standalone on port=%s", cfg.Port)
		err = runStandalone(cfg, ctx)
	}

	if err != nil {
		log.Printf("shutdown with error: %v", err)
	} else {
		log.Printf("shutdown complete")
	}
}
