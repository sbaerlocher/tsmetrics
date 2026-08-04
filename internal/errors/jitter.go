package errors

import (
	"math/rand/v2"
	"time"
)

// WithJitter adds a random ±25% jitter to d so concurrent clients stop
// synchronising on a recovering upstream. Negative jitter is clamped to zero.
func WithJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	// Half-range: each side of the interval is 25% of d.
	halfRange := int64(d) / 4
	if halfRange == 0 {
		return d
	}
	// rand.Int64N is safe for concurrent use (Go 1.22+) and suitable here —
	// retry pacing does not need crypto-grade randomness and the jittered
	// delay is not security-sensitive.
	//
	// #nosec G404 -- non-crypto RNG is appropriate for retry backoff jitter
	offset := rand.Int64N(2*halfRange+1) - halfRange //nolint:gosec // G404: see comment above
	jittered := int64(d) + offset
	if jittered < 0 {
		return 0
	}
	return time.Duration(jittered)
}
