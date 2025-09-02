package errors

import (
	"fmt"
	"time"
)

type ScrapeError struct {
	DeviceID   string
	DeviceName string
	Type       string
	Underlying error
}

func (e ScrapeError) Error() string {
	return fmt.Sprintf("device %s (%s): %s: %v", e.DeviceName, e.DeviceID, e.Type, e.Underlying)
}

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

func (e DeviceError) IsRetryable() bool {
	return e.Retryable
}

type ConfigurationError struct {
	Field  string
	Value  string
	Reason string
}

func (e ConfigurationError) Error() string {
	return fmt.Sprintf("configuration error in field %s (value: %s): %s", e.Field, e.Value, e.Reason)
}

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

type RetryableError struct {
	Underlying error
	RetryAfter time.Duration
}

func (e RetryableError) Error() string {
	return fmt.Sprintf("retryable error (retry after %v): %v", e.RetryAfter, e.Underlying)
}
