package metrics

import (
	"context"

	"github.com/sbaerlocher/tsmetrics/pkg/device"
)

// MetricCollector interface for collecting metrics from devices
type MetricCollector interface {
	CollectDeviceMetrics(ctx context.Context, device device.Device) error
}
