package server

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// TestCacheMetricsNotExported guards against re-adding device cache metrics
// that the exporter never fed: importing the server package used to register
// them, so /metrics reported constant zeros.
func TestCacheMetricsNotExported(t *testing.T) {
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, f := range families {
		name := f.GetName()
		if strings.HasPrefix(name, "tsmetrics_cache_") || name == "tsmetrics_api_calls_saved_total" {
			t.Errorf("%s is still exported", name)
		}
	}
}
