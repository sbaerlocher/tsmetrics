// Package config provides configuration management for TSMetrics.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all configuration settings for TSMetrics.
type Config struct {
	UseTsnet             bool
	TsnetHostname        string
	TsnetStateDir        string
	TsnetAuthKey         string
	Port                 string
	OAuthClientID        string
	OAuthSecret          string
	TailnetName          string
	ClientMetricsTimeout time.Duration
	MaxConcurrentScrapes int
	TsnetTags            []string
	RequireExporterTag   bool
	LogLevel             string
	LogFormat            string
	ClientMetricsPort    string
}

// Load reads configuration from environment variables and returns a Config struct.
func Load() Config {
	cfg := Config{}
	if strings.ToLower(os.Getenv("USE_TSNET")) == "true" {
		cfg.UseTsnet = true
	}
	cfg.TsnetHostname = os.Getenv("TSNET_HOSTNAME")
	cfg.TsnetStateDir = os.Getenv("TSNET_STATE_DIR")
	cfg.TsnetAuthKey = os.Getenv("TS_AUTHKEY")
	cfg.Port = os.Getenv("PORT")
	if cfg.Port == "" {
		cfg.Port = "9100"
	}
	cfg.OAuthClientID = os.Getenv("OAUTH_CLIENT_ID")
	cfg.OAuthSecret = os.Getenv("OAUTH_CLIENT_SECRET")
	cfg.TailnetName = os.Getenv("TAILNET_NAME")

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

	cfg.MaxConcurrentScrapes = 10
	if v := os.Getenv("MAX_CONCURRENT_SCRAPES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxConcurrentScrapes = n
		}
	}

	cfg.LogLevel = "info"
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.LogLevel = strings.ToLower(v)
	}

	cfg.LogFormat = "text"
	if v := os.Getenv("LOG_FORMAT"); v != "" {
		cfg.LogFormat = strings.ToLower(v)
	}

	cfg.ClientMetricsPort = "5252"
	if v := os.Getenv("CLIENT_METRICS_PORT"); v != "" {
		cfg.ClientMetricsPort = v
	}

	if v := os.Getenv("TSNET_TAGS"); v != "" {
		parts := strings.Split(v, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		cfg.TsnetTags = parts
	}

	if strings.ToLower(os.Getenv("REQUIRE_EXPORTER_TAG")) == "true" {
		cfg.RequireExporterTag = true
	}

	return cfg
}

// Validate checks the configuration for consistency and required values.
func (cfg Config) Validate() error {
	hasOAuth := cfg.OAuthClientID != "" && cfg.OAuthSecret != ""
	hasToken := os.Getenv("OAUTH_TOKEN") != ""

	if hasOAuth && hasToken {
		return fmt.Errorf("cannot use both OAuth credentials and direct token")
	}

	validLogLevels := []string{"debug", "info", "warn", "error"}
	if cfg.LogLevel != "" && !contains(validLogLevels, cfg.LogLevel) {
		return fmt.Errorf("invalid log level: %s, valid options: %v", cfg.LogLevel, validLogLevels)
	}

	validLogFormats := []string{"json", "text"}
	if cfg.LogFormat != "" && !contains(validLogFormats, cfg.LogFormat) {
		return fmt.Errorf("invalid log format: %s, valid options: %v", cfg.LogFormat, validLogFormats)
	}

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

	hasOAuth = cfg.OAuthClientID != "" && cfg.OAuthSecret != ""
	hasToken = os.Getenv("OAUTH_TOKEN") != ""
	hasTailnet := cfg.TailnetName != ""

	if hasTailnet && !hasOAuth && !hasToken {
		return fmt.Errorf("TAILNET_NAME specified but missing OAuth credentials (OAUTH_CLIENT_ID+SECRET) or OAUTH_TOKEN")
	}

	return nil
}

// SetupTsnetStateDir creates and validates the tsnet state directory.
func SetupTsnetStateDir(dir string) string {
	if dir == "" {
		dir = "/tmp/tsnet-tsmetrics"
	}
	if err := os.MkdirAll(dir, 0750); err != nil {
		slog.Warn("failed to create state directory", "dir", dir, "error", err)
		return ""
	}
	slog.Info("using tsnet state directory", "dir", dir)
	return dir
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
