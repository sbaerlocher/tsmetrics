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
	TsnetStateSecret     string
	TsnetAuthKey         string
	Port                 string
	BindHost             string
	OAuthClientID        string
	OAuthSecret          string //nolint:gosec // G117: not a hardcoded credential, loaded from env
	TailnetName          string
	ClientMetricsTimeout time.Duration
	MaxConcurrentScrapes int
	TsnetOwnTags         []string
	TsnetScrapeTag       string
	LogLevel             string
	LogFormat            string
	ClientMetricsPort    string
}

// Load reads configuration from environment variables and returns a Config struct.
func Load() Config {
	cfg := Config{}

	cfg.loadNetworkSettings()
	cfg.loadAuthSettings()
	cfg.loadTsnetSettings()
	cfg.loadLoggingSettings()
	cfg.loadMetricsSettings()

	return cfg
}

func (cfg *Config) loadNetworkSettings() {
	cfg.Port = os.Getenv("PORT")
	if cfg.Port == "" {
		cfg.Port = "9100"
	}

	cfg.BindHost = resolveBindHost()

	cfg.ClientMetricsPort = "5252"
	if v := os.Getenv("CLIENT_METRICS_PORT"); v != "" {
		cfg.ClientMetricsPort = v
	}

	cfg.MaxConcurrentScrapes = 10
	if v := os.Getenv("MAX_CONCURRENT_SCRAPES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxConcurrentScrapes = n
		}
	}
}

// resolveBindHost returns the listen address for the HTTP server.
// BIND_HOST env wins; otherwise auto-detect: containers → 0.0.0.0, host → loopback.
func resolveBindHost() string {
	if v := strings.TrimSpace(os.Getenv("BIND_HOST")); v != "" {
		return v
	}
	if detectContainer() {
		slog.Debug("container runtime detected, binding on all interfaces")
		return "0.0.0.0"
	}
	return "127.0.0.1" // DevSkim: ignore DS162092 - Host-native default avoids exposing metrics on a dev LAN
}

// detectContainer heuristically identifies whether the process runs inside
// a containerized runtime (Docker, Kubernetes, containerd, etc.). When the
// heuristic fails (e.g. a cgroups-v2 host with no container markers exposed
// to the process), operators can force the result via BIND_HOST.
func detectContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		return true
	}
	// cgroups v1: the hierarchy paths in /proc/self/cgroup carry the runtime
	// slice name ("/docker/<id>", "/kubepods/...", etc.). On cgroups v2 the
	// file typically collapses to "0::/" and these markers disappear, so the
	// v1 read is checked first and the v2 fallback below covers the rest.
	if b, err := os.ReadFile("/proc/self/cgroup"); err == nil {
		s := string(b)
		for _, marker := range []string{"/docker/", "/kubepods/", "/containerd/", "/docker-", "/lxc/"} {
			if strings.Contains(s, marker) {
				return true
			}
		}
	}
	// cgroups v2 fallback: container mount namespaces surface their overlay
	// root (and frequently the container id) in /proc/self/mountinfo even
	// when /proc/self/cgroup is generic. Match on the overlay filesystem
	// type plus the runtime-specific paths used by Docker/Podman/K8s.
	if b, err := os.ReadFile("/proc/self/mountinfo"); err == nil {
		s := string(b)
		for _, marker := range []string{"/docker/containers/", "/containers/storage/overlay", "/kubelet/pods/"} {
			if strings.Contains(s, marker) {
				return true
			}
		}
	}
	return false
}

func (cfg *Config) loadAuthSettings() {
	cfg.OAuthClientID = os.Getenv("OAUTH_CLIENT_ID")
	cfg.OAuthSecret = os.Getenv("OAUTH_CLIENT_SECRET")
	cfg.TailnetName = os.Getenv("TAILNET_NAME")
}

func (cfg *Config) loadTsnetSettings() {
	if strings.ToLower(os.Getenv("USE_TSNET")) == "true" {
		cfg.UseTsnet = true
	}
	cfg.TsnetHostname = os.Getenv("TSNET_HOSTNAME")
	cfg.TsnetStateDir = os.Getenv("TSNET_STATE_DIR")
	cfg.TsnetStateSecret = os.Getenv("TSNET_STATE_SECRET")
	cfg.TsnetAuthKey = os.Getenv("TS_AUTHKEY")

	if v := os.Getenv("TSNET_TAGS"); v != "" {
		parts := strings.Split(v, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		cfg.TsnetOwnTags = parts
	}

	cfg.TsnetScrapeTag = "exporter"
	if v := os.Getenv("SCRAPE_TAG"); v != "" {
		cfg.TsnetScrapeTag = strings.TrimSpace(v)
	}
}

func (cfg *Config) loadLoggingSettings() {
	cfg.LogLevel = "info"
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.LogLevel = strings.ToLower(v)
	}

	cfg.LogFormat = "text"
	if v := os.Getenv("LOG_FORMAT"); v != "" {
		cfg.LogFormat = strings.ToLower(v)
	}
}

func (cfg *Config) loadMetricsSettings() {
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
}

// Validate checks the configuration for consistency and required values.
func (cfg Config) Validate() error {
	if err := cfg.validateAuth(); err != nil {
		return err
	}

	if err := cfg.validateLogSettings(); err != nil {
		return err
	}

	if err := cfg.validateTsnetSettings(); err != nil {
		return err
	}

	if err := cfg.validateNetworkSettings(); err != nil {
		return err
	}

	return cfg.validateTailnetSettings()
}

func (cfg Config) validateAuth() error {
	hasOAuth := cfg.OAuthClientID != "" && cfg.OAuthSecret != ""
	hasToken := os.Getenv("OAUTH_TOKEN") != ""

	if hasOAuth && hasToken {
		return fmt.Errorf("cannot use both OAuth credentials and direct token")
	}
	return nil
}

func (cfg Config) validateLogSettings() error {
	validLogLevels := []string{"debug", "info", "warn", "error"}
	if cfg.LogLevel != "" && !contains(validLogLevels, cfg.LogLevel) {
		return fmt.Errorf("invalid log level: %s, valid options: %v", cfg.LogLevel, validLogLevels)
	}

	validLogFormats := []string{"json", "text"}
	if cfg.LogFormat != "" && !contains(validLogFormats, cfg.LogFormat) {
		return fmt.Errorf("invalid log format: %s, valid options: %v", cfg.LogFormat, validLogFormats)
	}
	return nil
}

func (cfg Config) validateTsnetSettings() error {
	if cfg.UseTsnet && cfg.TsnetHostname == "" {
		return fmt.Errorf("TSNET_HOSTNAME required when USE_TSNET=true")
	}
	return nil
}

func (cfg Config) validateNetworkSettings() error {
	if cfg.Port == "" {
		return fmt.Errorf("PORT cannot be empty")
	}

	if cfg.ClientMetricsTimeout <= 0 {
		return fmt.Errorf("CLIENT_METRICS_TIMEOUT must be positive")
	}

	if cfg.MaxConcurrentScrapes <= 0 {
		return fmt.Errorf("MAX_CONCURRENT_SCRAPES must be positive")
	}
	return nil
}

func (cfg Config) validateTailnetSettings() error {
	hasOAuth := cfg.OAuthClientID != "" && cfg.OAuthSecret != ""
	hasToken := os.Getenv("OAUTH_TOKEN") != ""
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
