package config

import (
	"os"
	"testing"
	"time"
)

func TestConfigLoad(t *testing.T) {
	os.Clearenv()

	t.Setenv("USE_TSNET", "true")
	t.Setenv("TSNET_HOSTNAME", "test-hostname")
	t.Setenv("TSNET_STATE_DIR", "/tmp/test")
	t.Setenv("TS_AUTHKEY", "tskey-test")
	t.Setenv("PORT", "8080")
	t.Setenv("OAUTH_CLIENT_ID", "test-client")
	t.Setenv("OAUTH_CLIENT_SECRET", "test-secret")
	t.Setenv("TAILNET_NAME", "test-tailnet")
	t.Setenv("CLIENT_METRICS_TIMEOUT", "15s")
	t.Setenv("MAX_CONCURRENT_SCRAPES", "20")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_FORMAT", "json")
	t.Setenv("CLIENT_METRICS_PORT", "5353")
	t.Setenv("TSNET_TAGS", "exporter,monitoring")
	t.Setenv("SCRAPE_TAG", "production")

	cfg := Load()

	if !cfg.UseTsnet {
		t.Error("Expected UseTsnet to be true")
	}

	if cfg.TsnetHostname != "test-hostname" {
		t.Errorf("Expected TsnetHostname 'test-hostname', got %s", cfg.TsnetHostname)
	}

	if cfg.Port != "8080" {
		t.Errorf("Expected Port '8080', got %s", cfg.Port)
	}

	if cfg.ClientMetricsTimeout != 15*time.Second {
		t.Errorf("Expected ClientMetricsTimeout 15s, got %v", cfg.ClientMetricsTimeout)
	}

	if cfg.MaxConcurrentScrapes != 20 {
		t.Errorf("Expected MaxConcurrentScrapes 20, got %d", cfg.MaxConcurrentScrapes)
	}

	if cfg.LogLevel != "debug" {
		t.Errorf("Expected LogLevel 'debug', got %s", cfg.LogLevel)
	}

	if cfg.LogFormat != "json" {
		t.Errorf("Expected LogFormat 'json', got %s", cfg.LogFormat)
	}

	if len(cfg.TsnetOwnTags) != 2 || cfg.TsnetOwnTags[0] != "exporter" || cfg.TsnetOwnTags[1] != "monitoring" {
		t.Errorf("Expected TsnetOwnTags ['exporter', 'monitoring'], got %v", cfg.TsnetOwnTags)
	}

	if cfg.TsnetScrapeTag != "production" {
		t.Errorf("Expected TsnetScrapeTag 'production', got %s", cfg.TsnetScrapeTag)
	}
}

func TestConfigLoadDefaults(t *testing.T) {
	os.Clearenv()

	cfg := Load()

	if cfg.UseTsnet {
		t.Error("Expected UseTsnet to be false by default")
	}

	if cfg.Port != "9100" {
		t.Errorf("Expected default Port '9100', got %s", cfg.Port)
	}

	if cfg.ClientMetricsTimeout != 10*time.Second {
		t.Errorf("Expected default ClientMetricsTimeout 10s, got %v", cfg.ClientMetricsTimeout)
	}

	if cfg.MaxConcurrentScrapes != 10 {
		t.Errorf("Expected default MaxConcurrentScrapes 10, got %d", cfg.MaxConcurrentScrapes)
	}

	if cfg.LogLevel != "info" {
		t.Errorf("Expected default LogLevel 'info', got %s", cfg.LogLevel)
	}

	if cfg.LogFormat != "text" {
		t.Errorf("Expected default LogFormat 'text', got %s", cfg.LogFormat)
	}

	if cfg.ClientMetricsPort != "5252" {
		t.Errorf("Expected default ClientMetricsPort '5252', got %s", cfg.ClientMetricsPort)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		envVars map[string]string
		wantErr bool
	}{
		{
			name: "valid minimal config",
			config: Config{
				Port:                 "9100",
				ClientMetricsTimeout: 10 * time.Second,
				MaxConcurrentScrapes: 10,
				LogLevel:             "info",
				LogFormat:            "text",
			},
			wantErr: false,
		},
		{
			name: "invalid log level",
			config: Config{
				Port:                 "9100",
				ClientMetricsTimeout: 10 * time.Second,
				MaxConcurrentScrapes: 10,
				LogLevel:             "invalid",
				LogFormat:            "text",
			},
			wantErr: true,
		},
		{
			name: "invalid log format",
			config: Config{
				Port:                 "9100",
				ClientMetricsTimeout: 10 * time.Second,
				MaxConcurrentScrapes: 10,
				LogLevel:             "info",
				LogFormat:            "invalid",
			},
			wantErr: true,
		},
		{
			name: "empty port",
			config: Config{
				Port:                 "",
				ClientMetricsTimeout: 10 * time.Second,
				MaxConcurrentScrapes: 10,
				LogLevel:             "info",
				LogFormat:            "text",
			},
			wantErr: true,
		},
		{
			name: "zero timeout",
			config: Config{
				Port:                 "9100",
				ClientMetricsTimeout: 0,
				MaxConcurrentScrapes: 10,
				LogLevel:             "info",
				LogFormat:            "text",
			},
			wantErr: true,
		},
		{
			name: "zero concurrent scrapes",
			config: Config{
				Port:                 "9100",
				ClientMetricsTimeout: 10 * time.Second,
				MaxConcurrentScrapes: 0,
				LogLevel:             "info",
				LogFormat:            "text",
			},
			wantErr: true,
		},
		{
			name: "tsnet without hostname",
			config: Config{
				UseTsnet:             true,
				Port:                 "9100",
				ClientMetricsTimeout: 10 * time.Second,
				MaxConcurrentScrapes: 10,
				LogLevel:             "info",
				LogFormat:            "text",
			},
			wantErr: true,
		},
		{
			name: "both oauth credentials and token",
			config: Config{
				OAuthClientID:        "test-client",
				OAuthSecret:          "test-secret",
				Port:                 "9100",
				ClientMetricsTimeout: 10 * time.Second,
				MaxConcurrentScrapes: 10,
				LogLevel:             "info",
				LogFormat:            "text",
			},
			envVars: map[string]string{
				"OAUTH_TOKEN": "test-token",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Clearenv()

			for key, value := range tt.envVars {
				t.Setenv(key, value)
			}

			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSetupTsnetStateDir(t *testing.T) {
	dir := SetupTsnetStateDir("")
	if dir != "/tmp/tsnet-tsmetrics" {
		t.Errorf("Expected default dir '/tmp/tsnet-tsmetrics', got %s", dir)
	}

	customDir := "/tmp/custom-test-dir"
	dir = SetupTsnetStateDir(customDir)
	if dir != customDir {
		t.Errorf("Expected custom dir '%s', got %s", customDir, dir)
	}

	defer func() { _ = os.RemoveAll(customDir) }()
	if _, err := os.Stat(customDir); os.IsNotExist(err) {
		t.Errorf("Expected directory %s to be created", customDir)
	}
}

func TestResolveBindHost_EnvOverrideWins(t *testing.T) {
	t.Setenv("BIND_HOST", "192.0.2.10")
	if got := resolveBindHost(); got != "192.0.2.10" {
		t.Errorf("resolveBindHost() with BIND_HOST set = %q, want %q", got, "192.0.2.10")
	}
}

func TestResolveBindHost_EnvOverrideIsTrimmed(t *testing.T) {
	t.Setenv("BIND_HOST", "   0.0.0.0   ")
	if got := resolveBindHost(); got != "0.0.0.0" {
		t.Errorf("resolveBindHost() with whitespaced BIND_HOST = %q, want %q", got, "0.0.0.0")
	}
}

func TestResolveBindHost_HostFallback(t *testing.T) {
	// Explicitly clear BIND_HOST so the auto-detection path runs. On a host
	// (no /.dockerenv, no KUBERNETES_SERVICE_HOST, no container cgroup/mount
	// markers) the fallback must be loopback — binding 0.0.0.0 on a dev
	// workstation would expose metrics on the LAN.
	t.Setenv("BIND_HOST", "")
	if detectContainer() {
		t.Skip("running inside a container-detected environment; host fallback path not exercised")
	}
	if got := resolveBindHost(); got != "127.0.0.1" {
		t.Errorf("resolveBindHost() on non-container host = %q, want %q", got, "127.0.0.1")
	}
}
