package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	UseTsnet             bool
	TsnetHostname        string
	TsnetStateDir        string
	Port                 string
	OAuthClientID        string
	OAuthSecret          string
	TailnetName          string
	ClientMetricsTimeout time.Duration
	MaxConcurrentScrapes int
	TsnetTags            []string
	RequireExporterTag   bool
}

func loadConfig() Config {
	cfg := Config{}
	if strings.ToLower(os.Getenv("USE_TSNET")) == "true" {
		cfg.UseTsnet = true
	}
	cfg.TsnetHostname = os.Getenv("TSNET_HOSTNAME")
	cfg.TsnetStateDir = os.Getenv("TSNET_STATE_DIR")
	cfg.Port = os.Getenv("PORT")
	if cfg.Port == "" {
		cfg.Port = "9100"
	}
	cfg.OAuthClientID = os.Getenv("OAUTH_CLIENT_ID")
	cfg.OAuthSecret = os.Getenv("OAUTH_CLIENT_SECRET")
	cfg.TailnetName = os.Getenv("TAILNET_NAME")

	// CLIENT_METRICS_TIMEOUT as duration string, fallback to 10s
	if v := os.Getenv("CLIENT_METRICS_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.ClientMetricsTimeout = d
		} else if sec, err := strconv.Atoi(v); err == nil {
			cfg.ClientMetricsTimeout = time.Duration(sec) * time.Second
		}
	}
	if cfg.ClientMetricsTimeout == 0 {
		cfg.ClientMetricsTimeout = 10 * time.Second
	}

	// MAX_CONCURRENT_SCRAPES integer, fallback to 10
	cfg.MaxConcurrentScrapes = 10
	if v := os.Getenv("MAX_CONCURRENT_SCRAPES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxConcurrentScrapes = n
		}
	}

	// TSNET_TAGS: comma-separated list of tags assigned to this tsnet device
	if v := os.Getenv("TSNET_TAGS"); v != "" {
		parts := strings.Split(v, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		cfg.TsnetTags = parts
	}

	// REQUIRE_EXPORTER_TAG: if "true", enforce that this tsnet device must carry the "exporter" tag
	if strings.ToLower(os.Getenv("REQUIRE_EXPORTER_TAG")) == "true" {
		cfg.RequireExporterTag = true
	}

	return cfg
}

// Validate validates the configuration and returns an error if invalid
func (cfg Config) Validate() error {
	if cfg.UseTsnet && cfg.RequireExporterTag {
		hasExporter := false
		for _, tag := range cfg.TsnetTags {
			if tag == "exporter" {
				hasExporter = true
				break
			}
		}
		if !hasExporter {
			return fmt.Errorf("REQUIRE_EXPORTER_TAG is true but 'exporter' tag not found in TSNET_TAGS")
		}
	}

	if cfg.Port == "" {
		return fmt.Errorf("PORT cannot be empty")
	}

	if cfg.ClientMetricsTimeout <= 0 {
		return fmt.Errorf("CLIENT_METRICS_TIMEOUT must be positive")
	}

	if cfg.MaxConcurrentScrapes <= 0 {
		return fmt.Errorf("MAX_CONCURRENT_SCRAPES must be positive")
	}

	if cfg.UseTsnet && cfg.TsnetHostname == "" {
		return fmt.Errorf("TSNET_HOSTNAME required when USE_TSNET=true")
	}

	// Validate OAuth configuration
	hasOAuth := cfg.OAuthClientID != "" && cfg.OAuthSecret != ""
	hasToken := os.Getenv("OAUTH_TOKEN") != ""
	hasTailnet := cfg.TailnetName != ""

	if hasTailnet && !hasOAuth && !hasToken {
		return fmt.Errorf("TAILNET_NAME specified but missing OAuth credentials (OAUTH_CLIENT_ID+SECRET) or OAUTH_TOKEN")
	}

	return nil
}

func setupTsnetStateDir(dir string) string {
	if dir == "" {
		dir = "/tmp/tsnet-tsmetrics"
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("failed to create state directory %s: %v", dir, err)
		return ""
	}
	log.Printf("using tsnet state directory: %s", dir)
	return dir
}
