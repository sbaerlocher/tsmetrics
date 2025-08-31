package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Enhanced error types for better observability
type ScrapeError struct {
	DeviceID   string
	DeviceName string
	Type       string
	Underlying error
}

func (e ScrapeError) Error() string {
	return fmt.Sprintf("device %s (%s): %s: %v", e.DeviceName, e.DeviceID, e.Type, e.Underlying)
}

// DeviceError provides detailed device-specific error information
type DeviceError struct {
	DeviceID   string
	DeviceName string
	ErrorType  string
	Underlying error
	Retryable  bool
	RetryAfter time.Duration
	Timestamp  time.Time
}

func (e DeviceError) Error() string {
	retryInfo := ""
	if e.Retryable {
		retryInfo = fmt.Sprintf(" (retryable after %v)", e.RetryAfter)
	}
	return fmt.Sprintf("device %s (%s) %s: %v%s", e.DeviceName, e.DeviceID, e.ErrorType, e.Underlying, retryInfo)
}

func (e DeviceError) Unwrap() error {
	return e.Underlying
}

// IsRetryable returns whether this error should be retried
func (e DeviceError) IsRetryable() bool {
	return e.Retryable
}

// ConfigurationError for configuration-related issues
type ConfigurationError struct {
	Field  string
	Value  string
	Reason string
}

func (e ConfigurationError) Error() string {
	return fmt.Sprintf("configuration error in field %s (value: %s): %s", e.Field, e.Value, e.Reason)
}

// APIError for Tailscale API specific errors
type APIError struct {
	Endpoint   string
	StatusCode int
	Message    string
	Underlying error
	Retryable  bool
}

func (e APIError) Error() string {
	return fmt.Sprintf("API error on %s (status %d): %s", e.Endpoint, e.StatusCode, e.Message)
}

func (e APIError) Unwrap() error {
	return e.Underlying
}

func (e APIError) IsRetryable() bool {
	return e.Retryable
}

// RetryableError indicates errors that should be retried
type RetryableError struct {
	Underlying error
	RetryAfter time.Duration
}

func (e RetryableError) Error() string {
	return fmt.Sprintf("retryable error (retry after %v): %v", e.RetryAfter, e.Underlying)
}

// Enhanced APIClient with retry logic and circuit breaker
type EnhancedAPIClient struct {
	*APIClient
	retryConfig         RetryConfig
	circuitBreakerState CircuitBreakerState
}

type RetryConfig struct {
	MaxRetries    int
	BaseDelay     time.Duration
	MaxDelay      time.Duration
	BackoffFactor float64
}

type CircuitBreakerState struct {
	failures    int
	lastFailure time.Time
	state       string // "closed", "open", "half-open"
}

// Enhanced scrapeClient with better error classification
func enhancedScrapeClient(dev Device, client *http.Client) error {
	hostForURL := dev.Host
	if hostForURL == "" {
		hostForURL = dev.Name
	}

	if err := validateHostname(hostForURL); err != nil {
		return ScrapeError{
			DeviceID:   dev.ID,
			DeviceName: dev.Name,
			Type:       "validation_error",
			Underlying: err,
		}
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), client.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("http://%s:5252/metrics", hostForURL), nil)
	if err != nil {
		return ScrapeError{
			DeviceID:   dev.ID,
			DeviceName: dev.Name,
			Type:       "request_creation_error",
			Underlying: err,
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		// Classify network errors for better retry logic
		errorType := "network_error"
		if ctx.Err() == context.DeadlineExceeded {
			errorType = "timeout_error"
		}

		return ScrapeError{
			DeviceID:   dev.ID,
			DeviceName: dev.Name,
			Type:       errorType,
			Underlying: RetryableError{
				Underlying: err,
				RetryAfter: 30 * time.Second,
			},
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errorType := "http_error"
		retryable := false

		switch resp.StatusCode {
		case http.StatusTooManyRequests, http.StatusInternalServerError,
			http.StatusBadGateway, http.StatusServiceUnavailable,
			http.StatusGatewayTimeout:
			retryable = true
		case http.StatusNotFound, http.StatusForbidden:
			errorType = "client_metrics_unavailable"
		}

		baseErr := fmt.Errorf("HTTP %d", resp.StatusCode)
		if retryable {
			baseErr = RetryableError{
				Underlying: baseErr,
				RetryAfter: 60 * time.Second,
			}
		}

		return ScrapeError{
			DeviceID:   dev.ID,
			DeviceName: dev.Name,
			Type:       errorType,
			Underlying: baseErr,
		}
	}

	// Process response body (existing parsing logic)
	// ... existing parsing code ...

	return nil
}

// Enhanced metrics update with better error aggregation
func enhancedUpdateMetrics(target string, cfg Config) error {
	start := time.Now()
	defer func() {
		scrapeDuration.WithLabelValues(target).Observe(time.Since(start).Seconds())
	}()

	devices, err := fetchDevices()
	if err != nil {
		scrapeErrors.WithLabelValues(target, "device_discovery_failed").Inc()
		return fmt.Errorf("device discovery failed: %w", err)
	}

	deviceCount.Set(float64(len(devices)))

	// Update API metrics (existing logic)
	// ... existing API metrics code ...

	// Enhanced client scraping with error classification
	var networkErrors, validationErrors int
	var retryableDevices []Device

	for _, device := range devices {
		if !device.Online || !hasTag(device, "exporter") {
			continue
		}

		client := &http.Client{Timeout: cfg.ClientMetricsTimeout}
		if err := enhancedScrapeClient(device, client); err != nil {
			var scrapeErr ScrapeError
			if errors.As(err, &scrapeErr) {
				scrapeErrors.WithLabelValues(scrapeErr.DeviceName, scrapeErr.Type).Inc()

				switch scrapeErr.Type {
				case "network_error", "timeout_error":
					networkErrors++
					var retryErr RetryableError
					if errors.As(scrapeErr.Underlying, &retryErr) {
						retryableDevices = append(retryableDevices, device)
					}
				case "validation_error":
					validationErrors++
				}
			}
		}
	}

	// Log summary of errors
	if networkErrors > 0 || validationErrors > 0 {
		slog.Info("scraping summary",
			"network_errors", networkErrors,
			"validation_errors", validationErrors,
			"retry_devices", len(retryableDevices))
	}

	return nil
}

// Circuit breaker implementation for API calls
func (c *EnhancedAPIClient) fetchDevicesWithCircuitBreaker() ([]Device, error) {
	now := time.Now()

	// Check circuit breaker state
	switch c.circuitBreakerState.state {
	case "open":
		if now.Sub(c.circuitBreakerState.lastFailure) < 60*time.Second {
			return nil, fmt.Errorf("circuit breaker open: too many recent failures")
		}
		c.circuitBreakerState.state = "half-open"
	}

	devices, err := c.APIClient.fetchDevicesFromAPI()
	if err != nil {
		c.circuitBreakerState.failures++
		c.circuitBreakerState.lastFailure = now

		// Open circuit if too many failures
		if c.circuitBreakerState.failures >= 3 {
			c.circuitBreakerState.state = "open"
			slog.Warn("circuit breaker opened", "failures", c.circuitBreakerState.failures)
		}

		return nil, fmt.Errorf("API call failed (failure %d): %w",
			c.circuitBreakerState.failures, err)
	}

	// Success - reset circuit breaker
	if c.circuitBreakerState.state == "half-open" {
		c.circuitBreakerState.state = "closed"
		c.circuitBreakerState.failures = 0
		slog.Info("circuit breaker closed", "reason", "API calls successful again")
	}

	return devices, nil
}
