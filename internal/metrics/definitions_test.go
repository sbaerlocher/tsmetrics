package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// TestDeadRuntimeMetricsNotExported guards against re-adding runtime gauges
// that were never set, and checks that the Go collector replacements named in
// the CHANGELOG are exported by the default registry.
func TestDeadRuntimeMetricsNotExported(t *testing.T) {
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	names := make(map[string]bool, len(families))
	for _, f := range families {
		names[f.GetName()] = true
	}

	for _, dead := range []string{"tsmetrics_memory_usage_bytes", "tsmetrics_goroutines_total"} {
		if names[dead] {
			t.Errorf("%s is still exported", dead)
		}
	}
	for _, want := range []string{"go_goroutines", "go_memstats_heap_alloc_bytes"} {
		if !names[want] {
			t.Errorf("%s is not exported", want)
		}
	}
}
