// Package errors provides error types and handling utilities for TSMetrics.
package errors

import (
	"errors"
	"fmt"
	"time"
)

// Error constants for common validation errors
var (
	ErrInvalidCollector     = errors.New("invalid collector")
	ErrInvalidInterval      = errors.New("invalid interval")
	ErrInvalidTimeout       = errors.New("invalid timeout")
	ErrInvalidConcurrency   = errors.New("invalid concurrency")
	ErrInvalidLoadThreshold = errors.New("invalid load threshold")
)

// ScrapeError represents an error that occurred during device scraping.
type ScrapeError struct {
	DeviceID   string
	DeviceName string
	Type       string
	Underlying error
}

func (e ScrapeError) Error() string {
	return fmt.Sprintf("device %s (%s): %s: %v", e.DeviceName, e.DeviceID, e.Type, e.Underlying)
}

// DeviceError represents an error related to a specific device.
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

// IsRetryable returns whether the device error is retryable.
func (e DeviceError) IsRetryable() bool {
	return e.Retryable
}

// ConfigurationError represents an error in configuration validation.
type ConfigurationError struct {
	Field  string
	Value  string
	Reason string
}

func (e ConfigurationError) Error() string {
	return fmt.Sprintf("configuration error in field %s (value: %s): %s", e.Field, e.Value, e.Reason)
}

// APIError represents an error that occurred during API communication.
type APIError struct {
	Endpoint   string
	StatusCode int
	Retryable  bool
	Context    map[string]interface{}
	Underlying error
	Timestamp  time.Time
}

func (e APIError) Error() string {
	return fmt.Sprintf("API error on %s (status %d): %v", e.Endpoint, e.StatusCode, e.Underlying)
}

func (e APIError) Unwrap() error {
	return e.Underlying
}

// IsRetryable returns whether the API error is retryable.
func (e APIError) IsRetryable() bool {
	return e.Retryable
}

// NewAPIError creates a new API error with the provided details.
func NewAPIError(endpoint string, statusCode int, err error) *APIError {
	retryable := statusCode >= 500 || statusCode == 429 || statusCode == 408
	return &APIError{
		Endpoint:   endpoint,
		StatusCode: statusCode,
		Retryable:  retryable,
		Context:    make(map[string]interface{}),
		Underlying: err,
		Timestamp:  time.Now(),
	}
}

// RetryConfig configures retry behavior for failed operations.
type RetryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Multiplier  float64
}

// DefaultRetryConfig returns a default retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    10 * time.Second,
		Multiplier:  2.0,
	}
}

// CalculateDelay calculates the delay for the given retry attempt.
func (rc RetryConfig) CalculateDelay(attempt int) time.Duration {
	if attempt <= 0 {
		return rc.BaseDelay
	}

	delay := float64(rc.BaseDelay)
	for i := 0; i < attempt; i++ {
		delay *= rc.Multiplier
	}

	if time.Duration(delay) > rc.MaxDelay {
		return rc.MaxDelay
	}

	return time.Duration(delay)
}

// RetryableError wraps an error to indicate it can be retried.
type RetryableError struct {
	Underlying error
	RetryAfter time.Duration
}

func (e RetryableError) Error() string {
	return fmt.Sprintf("retryable error (retry after %v): %v", e.RetryAfter, e.Underlying)
}
