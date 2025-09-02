package types

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
)

type DeviceID string
type DeviceName string
type MetricName string
type TagName string

var (
	ErrInvalidDeviceID   = errors.New("invalid device ID")
	ErrInvalidDeviceName = errors.New("invalid device name")
	ErrInvalidMetricName = errors.New("invalid metric name")
	ErrInvalidTagName    = errors.New("invalid tag name")
	ErrHostnameTooLong   = errors.New("hostname too long")
	ErrInvalidHostname   = errors.New("invalid hostname format")
	ErrPrivateIP         = errors.New("private IP addresses not allowed")

	deviceNameRegex = regexp.MustCompile(`^[a-zA-Z0-9\-._]+$`)
	metricNameRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	tagNameRegex    = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_\-]*$`)
)

func NewDeviceID(id string) (DeviceID, error) {
	if id == "" {
		return "", fmt.Errorf("device ID cannot be empty")
	}
	if len(id) > 64 {
		return "", fmt.Errorf("device ID too long: %d characters", len(id))
	}
	return DeviceID(id), nil
}

func (d DeviceID) IsValid() bool {
	return len(d) > 0 && len(d) <= 64
}

func (d DeviceID) String() string {
	return string(d)
}

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

func (d DeviceName) IsValid() bool {
	return len(d) > 0 && len(d) <= 253 && deviceNameRegex.MatchString(string(d))
}

func (d DeviceName) String() string {
	return string(d)
}

func NewMetricName(name string) (MetricName, error) {
	if name == "" {
		return "", fmt.Errorf("metric name cannot be empty")
	}
	if !metricNameRegex.MatchString(name) {
		return "", fmt.Errorf("invalid metric name format: %s", name)
	}
	return MetricName(name), nil
}

func (m MetricName) IsValid() bool {
	return len(m) > 0 && metricNameRegex.MatchString(string(m))
}

func (m MetricName) String() string {
	return string(m)
}

func NewTagName(name string) (TagName, error) {
	if name == "" {
		return "", fmt.Errorf("tag name cannot be empty")
	}
	if !tagNameRegex.MatchString(name) {
		return "", fmt.Errorf("invalid tag name format: %s", name)
	}
	return TagName(name), nil
}

func (t TagName) IsValid() bool {
	return len(t) > 0 && tagNameRegex.MatchString(string(t))
}

func (t TagName) String() string {
	return string(t)
}

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
