package config

import (
	"os"
	"testing"
	"time"
)

func TestConfigLoad(t *testing.T) {
	os.Clearenv()

	os.Setenv("USE_TSNET", "true")
	os.Setenv("TSNET_HOSTNAME", "test-hostname")
	os.Setenv("TSNET_STATE_DIR", "/tmp/test")
	os.Setenv("TS_AUTHKEY", "tskey-test")
	os.Setenv("PORT", "8080")
	os.Setenv("OAUTH_CLIENT_ID", "test-client")
	os.Setenv("OAUTH_CLIENT_SECRET", "test-secret")
	os.Setenv("TAILNET_NAME", "test-tailnet")
	os.Setenv("CLIENT_METRICS_TIMEOUT", "15s")
	os.Setenv("MAX_CONCURRENT_SCRAPES", "20")
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("LOG_FORMAT", "json")
	os.Setenv("CLIENT_METRICS_PORT", "5353")
	os.Setenv("TSNET_TAGS", "exporter,monitoring")
	os.Setenv("SCRAPE_TAG", "production")

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
				os.Setenv(key, value)
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

	defer os.RemoveAll(customDir)
	if _, err := os.Stat(customDir); os.IsNotExist(err) {
		t.Errorf("Expected directory %s to be created", customDir)
	}
}
