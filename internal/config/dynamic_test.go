package config

import (
	"context"
	"testing"
	"time"
)

func TestNewDynamicConfig(t *testing.T) {
	initialConfig := Config{
		Port:                 "9100",
		ClientMetricsTimeout: 5 * time.Second,
		MaxConcurrentScrapes: 10,
	}

	dc := NewDynamicConfig(initialConfig, 1*time.Minute)

	if dc.GetVersion() != 1 {
		t.Errorf("Expected initial version 1, got %d", dc.GetVersion())
	}

	config := dc.GetConfig()
	if config.Port != "9100" {
		t.Errorf("Expected port 9100, got %s", config.Port)
	}
}

func TestDynamicConfig_AddValidator(t *testing.T) {
	initialConfig := Config{}
	dc := NewDynamicConfig(initialConfig, 1*time.Minute)

	validator := NewBasicConfigValidator()
	dc.AddValidator(validator)

	status := dc.GetStatus()
	if status["validators_count"] != 1 {
		t.Errorf("Expected 1 validator, got %v", status["validators_count"])
	}
}

func TestDynamicConfig_AddChangeHandler(t *testing.T) {
	initialConfig := Config{}
	dc := NewDynamicConfig(initialConfig, 1*time.Minute)

	handler := NewLoggingConfigHandler()
	dc.AddChangeHandler(handler)

	status := dc.GetStatus()
	if status["change_handlers_count"] != 1 {
		t.Errorf("Expected 1 change handler, got %v", status["change_handlers_count"])
	}
}

func TestBasicConfigValidator_ValidateConfig(t *testing.T) {
	validator := NewBasicConfigValidator()

	tests := []struct {
		name      string
		config    Config
		expectErr bool
	}{
		{
			name: "valid config",
			config: Config{
				Port:                 "9100",
				ClientMetricsTimeout: 5 * time.Second,
				MaxConcurrentScrapes: 10,
			},
			expectErr: false,
		},
		{
			name: "zero timeout",
			config: Config{
				Port:                 "9100",
				ClientMetricsTimeout: 0,
				MaxConcurrentScrapes: 10,
			},
			expectErr: true,
		},
		{
			name: "zero concurrent scrapes",
			config: Config{
				Port:                 "9100",
				ClientMetricsTimeout: 5 * time.Second,
				MaxConcurrentScrapes: 0,
			},
			expectErr: true,
		},
		{
			name: "empty port",
			config: Config{
				Port:                 "",
				ClientMetricsTimeout: 5 * time.Second,
				MaxConcurrentScrapes: 10,
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateConfig(tt.config)
			if tt.expectErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Expected no error but got %v", err)
			}
		})
	}
}

func TestLoggingConfigHandler_HandleConfigChange(t *testing.T) {
	handler := NewLoggingConfigHandler()

	oldConfig := Config{LogLevel: "info"}
	newConfig := Config{LogLevel: "debug"}

	err := handler.HandleConfigChange(oldConfig, newConfig)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if handler.Name() != "logging" {
		t.Errorf("Expected handler name 'logging', got %s", handler.Name())
	}

	if handler.Priority() != 1 {
		t.Errorf("Expected priority 1, got %d", handler.Priority())
	}
}

func TestFileConfigSource(t *testing.T) {
	source := NewFileConfigSource("/nonexistent/config.yaml")

	if source.Name() != "file:/nonexistent/config.yaml" {
		t.Errorf("Expected name 'file:/nonexistent/config.yaml', got %s", source.Name())
	}

	// Test LoadConfig (should return default config from Load())
	config, err := source.LoadConfig()
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if config.Port == "" {
		t.Error("Expected default port to be set")
	}

	// Test WatchChanges
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	changeChan, err := source.WatchChanges(ctx)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if changeChan == nil {
		t.Error("Expected non-nil change channel")
	}
}

func TestDynamicConfig_GetStatus(t *testing.T) {
	initialConfig := Config{}
	dc := NewDynamicConfig(initialConfig, 1*time.Minute)

	validator := NewBasicConfigValidator()
	handler := NewLoggingConfigHandler()

	dc.AddValidator(validator)
	dc.AddChangeHandler(handler)

	status := dc.GetStatus()

	expectedFields := []string{
		"version",
		"last_reload",
		"reload_in_progress",
		"reload_interval",
		"validators_count",
		"change_handlers_count",
	}

	for _, field := range expectedFields {
		if _, exists := status[field]; !exists {
			t.Errorf("Expected field %s in status", field)
		}
	}

	if status["version"] != int64(1) {
		t.Errorf("Expected version 1, got %v", status["version"])
	}

	if status["validators_count"] != 1 {
		t.Errorf("Expected 1 validator, got %v", status["validators_count"])
	}

	if status["change_handlers_count"] != 1 {
		t.Errorf("Expected 1 change handler, got %v", status["change_handlers_count"])
	}
}

func TestValidationError(t *testing.T) {
	err := ValidationError{
		Field:   "Port",
		Value:   "",
		Message: "must not be empty",
	}

	expected := "validation error in field Port (value: ): must not be empty"
	if err.Error() != expected {
		t.Errorf("Expected error message %q, got %q", expected, err.Error())
	}
}

func TestDynamicConfig_Stop(t *testing.T) {
	initialConfig := Config{}
	dc := NewDynamicConfig(initialConfig, 1*time.Minute)

	// Start watching with a mock source
	source := NewFileConfigSource("/nonexistent/config.yaml")
	dc.StartWatching(source)

	// Stop should complete without hanging
	done := make(chan struct{})
	go func() {
		dc.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Expected
	case <-time.After(2 * time.Second):
		t.Error("Stop() took too long")
	}
}
