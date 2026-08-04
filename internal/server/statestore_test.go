package server

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"tailscale.com/ipn"
	"tailscale.com/types/logger"

	tserrors "github.com/sbaerlocher/tsmetrics/internal/errors"
)

// discardLogf drops tsnet log output during tests.
func discardLogf(string, ...any) {}

// stubStore is a minimal ipn.StateStore standing in for the kube store. The
// retry wrapper never touches the store's contents, so the methods are inert.
type stubStore struct{}

func (stubStore) ReadState(ipn.StateKey) ([]byte, error) { return nil, ipn.ErrStateNotExist }

func (stubStore) WriteState(ipn.StateKey, []byte) error { return nil }

// failingFactory returns an error for the first failures calls, then succeeds.
// It records how many times it was invoked.
func failingFactory(failures int, calls *int) storeFactory {
	return func(logger.Logf, string) (ipn.StateStore, error) {
		*calls++
		if *calls <= failures {
			return nil, errors.New("connection refused")
		}
		return stubStore{}, nil
	}
}

func TestNewStateStoreWithRetry_SucceedsFirstAttempt(t *testing.T) {
	calls := 0
	store, err := newStateStoreWithRetry(context.Background(), discardLogf, "kube:test", failingFactory(0, &calls))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store == nil {
		t.Fatal("expected a state store, got nil")
	}
	if calls != 1 {
		t.Errorf("factory called %d times, want 1", calls)
	}
}

func TestNewStateStoreWithRetry_SucceedsSecondAttempt(t *testing.T) {
	calls := 0
	store, err := newStateStoreWithRetry(context.Background(), discardLogf, "kube:test", failingFactory(1, &calls))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store == nil {
		t.Fatal("expected a state store, got nil")
	}
	if calls != 2 {
		t.Errorf("factory called %d times, want 2", calls)
	}
}

func TestNewStateStoreWithRetry_ExhaustsAttempts(t *testing.T) {
	// A fast config keeps the test quick; the real startup budget is asserted
	// in TestStartupRetryConfig.
	fastCfg := tserrors.RetryConfig{
		MaxAttempts: 4,
		BaseDelay:   time.Millisecond,
		MaxDelay:    2 * time.Millisecond,
		Multiplier:  2.0,
	}

	calls := 0
	_, err := newStateStoreWithRetryConfig(context.Background(), discardLogf, "kube:test",
		failingFactory(fastCfg.MaxAttempts, &calls), fastCfg)
	if err == nil {
		t.Fatal("expected an error when every attempt fails")
	}
	if calls != fastCfg.MaxAttempts {
		t.Errorf("factory called %d times, want exactly %d", calls, fastCfg.MaxAttempts)
	}
	// The underlying failure must stay inspectable by the caller.
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error %q does not wrap the underlying failure", err)
	}
}

func TestNewStateStoreWithRetry_StopsDuringBackoff(t *testing.T) {
	// Long delays guarantee the cancel lands while the loop waits between
	// attempts rather than before the first one.
	slowCfg := tserrors.RetryConfig{
		MaxAttempts: 5,
		BaseDelay:   10 * time.Second,
		MaxDelay:    10 * time.Second,
		Multiplier:  2.0,
	}

	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	factory := func(logger.Logf, string) (ipn.StateStore, error) {
		calls++
		cancel() // fail, then cancel while the wrapper waits to retry
		return nil, errors.New("connection refused")
	}

	start := time.Now()
	_, err := newStateStoreWithRetryConfig(ctx, discardLogf, "kube:test", factory, slowCfg)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error when the context is cancelled mid-backoff")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want it to wrap context.Canceled", err)
	}
	if calls != 1 {
		t.Errorf("factory called %d times, want 1", calls)
	}
	// Must abort the wait rather than sleep out the full backoff.
	if elapsed > 5*time.Second {
		t.Errorf("took %v, want an immediate abort on cancel", elapsed)
	}
}

func TestNewStateStoreWithRetry_StopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := 0
	_, err := newStateStoreWithRetry(ctx, discardLogf, "kube:test", failingFactory(99, &calls))
	if err == nil {
		t.Fatal("expected an error when the context is cancelled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want it to wrap context.Canceled", err)
	}
	// An already-cancelled context must short-circuit before the factory runs.
	if calls != 0 {
		t.Errorf("factory called %d times, want 0", calls)
	}
}
