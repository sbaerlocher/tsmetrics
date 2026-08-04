package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"tailscale.com/ipn"
	"tailscale.com/types/logger"

	tserrors "github.com/sbaerlocher/tsmetrics/internal/errors"
)

// storeFactory creates a state store for the given path. In production this is
// tailscale.com/ipn/store.New; tests inject a stub.
type storeFactory func(logger.Logf, string) (ipn.StateStore, error)

// newStateStoreWithRetry creates the state store, retrying transient failures
// with exponential backoff. A Kubernetes API server that is briefly unreachable
// while it restarts would otherwise kill the process on startup, and the pod
// could not recover on its own once the API server came back.
//
// After the attempts are exhausted the original behaviour returns: the error is
// propagated, the process exits and Kubernetes restarts the pod. A permanently
// unreachable API server therefore stays visible as a CrashLoopBackOff instead
// of silently hanging.
//
// Every failure is treated as transient. Permanent ones — missing RBAC rights
// on the Secret, or running outside a cluster — therefore burn the full budget
// before the process exits. That costs roughly 90s per restart and is accepted
// deliberately: the failures are indistinguishable from a transient outage at
// this layer without parsing driver-specific error strings, and the retry only
// runs once per process start.
func newStateStoreWithRetry(ctx context.Context, logf logger.Logf, path string, factory storeFactory) (ipn.StateStore, error) {
	return newStateStoreWithRetryConfig(ctx, logf, path, factory, tserrors.StartupRetryConfig())
}

// newStateStoreWithRetryConfig is newStateStoreWithRetry with an injectable
// retry configuration so tests can exercise the loop without waiting out the
// full startup budget.
func newStateStoreWithRetryConfig(ctx context.Context, logf logger.Logf, path string, factory storeFactory, retryCfg tserrors.RetryConfig) (ipn.StateStore, error) {
	var lastErr error
	for attempt := range retryCfg.MaxAttempts {
		// Checked before every attempt, including the first: a context that is
		// already cancelled must not reach the factory at all.
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("state store creation cancelled before attempt %d: %w", attempt+1, err)
		}

		if attempt > 0 {
			delay := tserrors.WithJitter(retryCfg.CalculateDelay(attempt - 1))
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("state store creation cancelled while waiting to retry after %d attempts: %w", attempt, ctx.Err())
			case <-time.After(delay):
			}
		}

		stateStore, err := factory(logf, path)
		if err == nil {
			if attempt > 0 {
				slog.Info("state store created after retry", "attempts", attempt+1)
			}
			return stateStore, nil
		}

		lastErr = err
		slog.Warn("state store creation failed",
			"attempt", attempt+1,
			"max_attempts", retryCfg.MaxAttempts,
			"will_retry", attempt < retryCfg.MaxAttempts-1,
			"error", err)
	}

	if lastErr == nil {
		// Only reachable with a non-positive MaxAttempts, where the loop never
		// ran; without this the %w below would render as %!w(<nil>).
		return nil, fmt.Errorf("failed to create state store %q: no attempts made (MaxAttempts=%d)", path, retryCfg.MaxAttempts)
	}
	return nil, fmt.Errorf("failed to create state store %q after %d attempts: %w", path, retryCfg.MaxAttempts, lastErr)
}
