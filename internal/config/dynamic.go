// Package config provides dynamic configuration management capabilities
// including file watching, validation, and real-time configuration updates.
package config

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	configReloadCounter = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "tsmetrics_config_reload_total",
		Help: "Number of configuration reloads",
	}, []string{"status"})

	configReloadDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "tsmetrics_config_reload_duration_seconds",
		Help:    "Time taken to reload configuration",
		Buckets: prometheus.DefBuckets,
	})

	configValidationErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "tsmetrics_config_validation_errors_total",
		Help: "Number of configuration validation errors",
	})

	activeConfigVersion = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "tsmetrics_config_version",
		Help: "Current configuration version",
	})
)

type DynamicConfig struct {
	mutex            sync.RWMutex
	current          Config
	version          int64
	reloadInterval   time.Duration
	validators       []ConfigValidator
	changeHandlers   []ConfigChangeHandler
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	lastReload       time.Time
	reloadInProgress bool
}

type ConfigValidator interface {
	ValidateConfig(config Config) error
	Name() string
}

type ConfigChangeHandler interface {
	HandleConfigChange(oldConfig, newConfig Config) error
	Name() string
	Priority() int
}

type ConfigSource interface {
	LoadConfig() (Config, error)
	WatchChanges(ctx context.Context) (<-chan Config, error)
	Name() string
}

type ValidationError struct {
	Field   string
	Value   interface{}
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("validation error in field %s (value: %v): %s", e.Field, e.Value, e.Message)
}

func NewDynamicConfig(initialConfig Config, reloadInterval time.Duration) *DynamicConfig {
	ctx, cancel := context.WithCancel(context.Background())

	dc := &DynamicConfig{
		current:        initialConfig,
		version:        1,
		reloadInterval: reloadInterval,
		validators:     make([]ConfigValidator, 0),
		changeHandlers: make([]ConfigChangeHandler, 0),
		ctx:            ctx,
		cancel:         cancel,
		lastReload:     time.Now(),
	}

	activeConfigVersion.Set(1)

	return dc
}

func (dc *DynamicConfig) GetConfig() Config {
	dc.mutex.RLock()
	defer dc.mutex.RUnlock()
	return dc.current
}

func (dc *DynamicConfig) GetVersion() int64 {
	dc.mutex.RLock()
	defer dc.mutex.RUnlock()
	return dc.version
}

func (dc *DynamicConfig) AddValidator(validator ConfigValidator) {
	dc.mutex.Lock()
	defer dc.mutex.Unlock()
	dc.validators = append(dc.validators, validator)
}

func (dc *DynamicConfig) AddChangeHandler(handler ConfigChangeHandler) {
	dc.mutex.Lock()
	defer dc.mutex.Unlock()

	dc.changeHandlers = append(dc.changeHandlers, handler)
	dc.sortChangeHandlers()
}

func (dc *DynamicConfig) sortChangeHandlers() {
	for i := 0; i < len(dc.changeHandlers)-1; i++ {
		for j := 0; j < len(dc.changeHandlers)-i-1; j++ {
			if dc.changeHandlers[j].Priority() > dc.changeHandlers[j+1].Priority() {
				dc.changeHandlers[j], dc.changeHandlers[j+1] = dc.changeHandlers[j+1], dc.changeHandlers[j]
			}
		}
	}
}

func (dc *DynamicConfig) StartWatching(source ConfigSource) {
	dc.wg.Add(1)
	go dc.watchLoop(source)
}

func (dc *DynamicConfig) watchLoop(source ConfigSource) {
	defer dc.wg.Done()

	slog.Info("starting config watcher", "source", source.Name(), "interval", dc.reloadInterval)

	ticker := time.NewTicker(dc.reloadInterval)
	defer ticker.Stop()

	changeChan, err := source.WatchChanges(dc.ctx)
	if err != nil {
		slog.Error("failed to watch config changes", "source", source.Name(), "error", err)
		return
	}

	for {
		select {
		case <-dc.ctx.Done():
			return
		case <-ticker.C:
			dc.periodicReload(source)
		case newConfig := <-changeChan:
			dc.handleConfigChange(newConfig, source.Name())
		}
	}
}

func (dc *DynamicConfig) periodicReload(source ConfigSource) {
	if dc.isReloadInProgress() {
		slog.Debug("config reload already in progress, skipping periodic reload")
		return
	}

	newConfig, err := source.LoadConfig()
	if err != nil {
		slog.Error("periodic config reload failed", "source", source.Name(), "error", err)
		configReloadCounter.WithLabelValues("error").Inc()
		return
	}

	dc.handleConfigChange(newConfig, source.Name())
}

func (dc *DynamicConfig) handleConfigChange(newConfig Config, sourceName string) {
	start := time.Now()
	defer func() {
		configReloadDuration.Observe(time.Since(start).Seconds())
	}()

	dc.setReloadInProgress(true)
	defer dc.setReloadInProgress(false)

	slog.Debug("handling config change", "source", sourceName)

	if err := dc.validateConfig(newConfig); err != nil {
		slog.Error("config validation failed", "source", sourceName, "error", err)
		configValidationErrors.Inc()
		configReloadCounter.WithLabelValues("validation_error").Inc()
		return
	}

	oldConfig := dc.GetConfig()

	if dc.configsEqual(oldConfig, newConfig) {
		slog.Debug("config unchanged, skipping reload")
		return
	}

	if err := dc.applyConfigChange(oldConfig, newConfig); err != nil {
		slog.Error("config change application failed", "source", sourceName, "error", err)
		configReloadCounter.WithLabelValues("apply_error").Inc()
		return
	}

	dc.updateConfig(newConfig)

	slog.Info("config successfully reloaded",
		"source", sourceName,
		"version", dc.GetVersion(),
		"duration", time.Since(start))

	configReloadCounter.WithLabelValues("success").Inc()
}

func (dc *DynamicConfig) validateConfig(config Config) error {
	dc.mutex.RLock()
	validators := make([]ConfigValidator, len(dc.validators))
	copy(validators, dc.validators)
	dc.mutex.RUnlock()

	for _, validator := range validators {
		if err := validator.ValidateConfig(config); err != nil {
			return fmt.Errorf("validator %s failed: %w", validator.Name(), err)
		}
	}

	return nil
}

func (dc *DynamicConfig) applyConfigChange(oldConfig, newConfig Config) error {
	dc.mutex.RLock()
	handlers := make([]ConfigChangeHandler, len(dc.changeHandlers))
	copy(handlers, dc.changeHandlers)
	dc.mutex.RUnlock()

	for _, handler := range handlers {
		if err := handler.HandleConfigChange(oldConfig, newConfig); err != nil {
			return fmt.Errorf("handler %s failed: %w", handler.Name(), err)
		}
	}

	return nil
}

func (dc *DynamicConfig) updateConfig(newConfig Config) {
	dc.mutex.Lock()
	defer dc.mutex.Unlock()

	dc.current = newConfig
	dc.version++
	dc.lastReload = time.Now()

	activeConfigVersion.Set(float64(dc.version))
}

func (dc *DynamicConfig) configsEqual(a, b Config) bool {
	aJSON, _ := json.Marshal(a)
	bJSON, _ := json.Marshal(b)
	return string(aJSON) == string(bJSON)
}

func (dc *DynamicConfig) isReloadInProgress() bool {
	dc.mutex.RLock()
	defer dc.mutex.RUnlock()
	return dc.reloadInProgress
}

func (dc *DynamicConfig) setReloadInProgress(inProgress bool) {
	dc.mutex.Lock()
	defer dc.mutex.Unlock()
	dc.reloadInProgress = inProgress
}

func (dc *DynamicConfig) ForceReload(source ConfigSource) error {
	if dc.isReloadInProgress() {
		return fmt.Errorf("reload already in progress")
	}

	newConfig, err := source.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	dc.handleConfigChange(newConfig, source.Name())
	return nil
}

func (dc *DynamicConfig) Stop() {
	dc.cancel()
	dc.wg.Wait()
}

func (dc *DynamicConfig) GetStatus() map[string]interface{} {
	dc.mutex.RLock()
	defer dc.mutex.RUnlock()

	return map[string]interface{}{
		"version":               dc.version,
		"last_reload":           dc.lastReload,
		"reload_in_progress":    dc.reloadInProgress,
		"reload_interval":       dc.reloadInterval,
		"validators_count":      len(dc.validators),
		"change_handlers_count": len(dc.changeHandlers),
	}
}

type FileConfigSource struct {
	filePath string
}

func NewFileConfigSource(filePath string) *FileConfigSource {
	return &FileConfigSource{filePath: filePath}
}

func (fcs *FileConfigSource) Name() string {
	return fmt.Sprintf("file:%s", fcs.filePath)
}

func (fcs *FileConfigSource) LoadConfig() (Config, error) {
	return Load(), nil
}

func (fcs *FileConfigSource) WatchChanges(ctx context.Context) (<-chan Config, error) {
	configChan := make(chan Config, 1)

	go func() {
		defer close(configChan)
		<-ctx.Done()
	}()

	return configChan, nil
}

type BasicConfigValidator struct {
	name string
}

func NewBasicConfigValidator() *BasicConfigValidator {
	return &BasicConfigValidator{name: "basic"}
}

func (bcv *BasicConfigValidator) Name() string {
	return bcv.name
}

func (bcv *BasicConfigValidator) ValidateConfig(config Config) error {
	if config.ClientMetricsTimeout <= 0 {
		return ValidationError{
			Field:   "ClientMetricsTimeout",
			Value:   config.ClientMetricsTimeout,
			Message: "must be positive",
		}
	}

	if config.MaxConcurrentScrapes <= 0 {
		return ValidationError{
			Field:   "MaxConcurrentScrapes",
			Value:   config.MaxConcurrentScrapes,
			Message: "must be positive",
		}
	}

	if config.Port == "" {
		return ValidationError{
			Field:   "Port",
			Value:   config.Port,
			Message: "must not be empty",
		}
	}

	return nil
}

type LoggingConfigHandler struct {
	name     string
	priority int
}

func NewLoggingConfigHandler() *LoggingConfigHandler {
	return &LoggingConfigHandler{
		name:     "logging",
		priority: 1,
	}
}

func (lch *LoggingConfigHandler) Name() string {
	return lch.name
}

func (lch *LoggingConfigHandler) Priority() int {
	return lch.priority
}

func (lch *LoggingConfigHandler) HandleConfigChange(oldConfig, newConfig Config) error {
	if oldConfig.LogLevel != newConfig.LogLevel {
		slog.Info("log level changed",
			"old_level", oldConfig.LogLevel,
			"new_level", newConfig.LogLevel)
	}

	return nil
}
