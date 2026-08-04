package errors

import (
	"testing"
	"time"
)

func TestWithJitter_BoundsAndZero(t *testing.T) {
	if got := WithJitter(0); got != 0 {
		t.Errorf("WithJitter(0) = %v, want 0", got)
	}
	// Single-ns durations have a zero half-range and must round-trip.
	if got := WithJitter(time.Nanosecond); got != time.Nanosecond {
		t.Errorf("WithJitter(1ns) = %v, want 1ns", got)
	}
	// Bulk test: 1000 samples must all fall within ±25% of 400ms.
	base := 400 * time.Millisecond
	for range 1000 {
		got := WithJitter(base)
		if got < 300*time.Millisecond || got > 500*time.Millisecond {
			t.Fatalf("WithJitter(%v) = %v out of ±25%% bounds", base, got)
		}
	}
}
