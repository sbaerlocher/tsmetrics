// Package types provides core domain types and validation utilities for TSMetrics.
// This package defines fundamental types like DeviceID, DeviceName, MetricName and TagName
// along with their validation logic and error definitions.
package types

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
)

// DeviceID represents a unique identifier for a Tailscale device.
type DeviceID string

// DeviceName represents a human-readable name for a Tailscale device.
type DeviceName string

// MetricName represents a Prometheus metric name.
type MetricName string

// TagName represents a metric tag name.
type TagName string

var (
	// ErrInvalidDeviceID is returned when a device ID is invalid.
	ErrInvalidDeviceID = errors.New("invalid device ID")
	// ErrInvalidDeviceName is returned when a device name is invalid.
	ErrInvalidDeviceName = errors.New("invalid device name")
	// ErrInvalidMetricName is returned when a metric name is invalid.
	ErrInvalidMetricName = errors.New("invalid metric name")
	// ErrInvalidTagName is returned when a tag name is invalid.
	ErrInvalidTagName = errors.New("invalid tag name")
	// ErrHostnameTooLong is returned when a hostname exceeds maximum length.
	ErrHostnameTooLong = errors.New("hostname too long")
	// ErrInvalidHostname is returned when a hostname format is invalid.
	ErrInvalidHostname = errors.New("invalid hostname format")
	// ErrPrivateIP is returned when a private IP address is not allowed.
	ErrPrivateIP = errors.New("private IP addresses not allowed")

	deviceNameRegex = regexp.MustCompile(`^[a-zA-Z0-9\-._]+$`)
	metricNameRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	tagNameRegex    = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_\-]*$`)
)

// NewDeviceID creates a new DeviceID with validation.
func NewDeviceID(id string) (DeviceID, error) {
	if id == "" {
		return "", fmt.Errorf("device ID cannot be empty")
	}
	if len(id) > 64 {
		return "", fmt.Errorf("device ID too long: %d characters", len(id))
	}
	return DeviceID(id), nil
}

// IsValid checks if the DeviceID is valid.
func (d DeviceID) IsValid() bool {
	return len(d) > 0 && len(d) <= 64
}

func (d DeviceID) String() string {
	return string(d)
}

// NewDeviceName creates a new DeviceName with validation.
func NewDeviceName(name string) (DeviceName, error) {
	if name == "" {
		return "", fmt.Errorf("device name cannot be empty")
	}
	if len(name) > 253 {
		return "", fmt.Errorf("device name too long: %d characters", len(name))
	}
	if !deviceNameRegex.MatchString(name) {
		return "", fmt.Errorf("invalid device name format: %s", name)
	}
	return DeviceName(name), nil
}

// Sanitize cleans and normalizes a device name to ensure it's valid for Prometheus metrics.
func (d DeviceName) Sanitize() DeviceName {
	sanitized := strings.ToLower(string(d))
	sanitized = regexp.MustCompile(`[^a-zA-Z0-9\-._]`).ReplaceAllString(sanitized, "-")
	sanitized = regexp.MustCompile(`-+`).ReplaceAllString(sanitized, "-")
	sanitized = strings.Trim(sanitized, "-")

	if len(sanitized) > 253 {
		sanitized = sanitized[:253]
	}

	return DeviceName(sanitized)
}

// IsValid checks if the DeviceName meets validation requirements.
func (d DeviceName) IsValid() bool {
	return len(d) > 0 && len(d) <= 253 && deviceNameRegex.MatchString(string(d))
}

func (d DeviceName) String() string {
	return string(d)
}

// NewMetricName creates a new MetricName with validation.
func NewMetricName(name string) (MetricName, error) {
	if name == "" {
		return "", fmt.Errorf("metric name cannot be empty")
	}
	if !metricNameRegex.MatchString(name) {
		return "", fmt.Errorf("invalid metric name format: %s", name)
	}
	return MetricName(name), nil
}

// IsValid checks if the MetricName meets validation requirements.
func (m MetricName) IsValid() bool {
	return len(m) > 0 && metricNameRegex.MatchString(string(m))
}

func (m MetricName) String() string {
	return string(m)
}

// NewTagName creates a new TagName with validation.
func NewTagName(name string) (TagName, error) {
	if name == "" {
		return "", fmt.Errorf("tag name cannot be empty")
	}
	if !tagNameRegex.MatchString(name) {
		return "", fmt.Errorf("invalid tag name format: %s", name)
	}
	return TagName(name), nil
}

// IsValid checks if the TagName meets validation requirements.
func (t TagName) IsValid() bool {
	return len(t) > 0 && tagNameRegex.MatchString(string(t))
}

func (t TagName) String() string {
	return string(t)
}

// ValidateHostname validates that a hostname is acceptable for use.
func ValidateHostname(hostname string) error {
	if len(hostname) == 0 {
		return fmt.Errorf("hostname cannot be empty")
	}
	if len(hostname) > 253 {
		return fmt.Errorf("hostname too long: %d characters", len(hostname))
	}

	if ip := net.ParseIP(hostname); ip != nil {
		if ip.IsPrivate() || ip.IsLoopback() {
			return fmt.Errorf("private or loopback IP addresses not allowed: %s", hostname)
		}
		return nil
	}

	hostnameRegex := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?)*$`)
	if !hostnameRegex.MatchString(hostname) {
		return fmt.Errorf("invalid hostname format: %s", hostname)
	}

	return nil
}
