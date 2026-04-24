package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// retryTestClient builds a Client pointed at srv with the retry helper
// reachable directly. It uses the real DefaultRetryConfig (100ms base), so
// each failing attempt adds a small delay — tests assert behaviour, not exact
// timing, except where explicitly annotated.
func retryTestClient(srv *httptest.Server) *Client {
	return &Client{
		httpClient: srv.Client(),
		baseURL:    srv.URL,
		apiBase:    srv.URL,
	}
}

// statusSequenceHandler returns each status in sequence; extra requests
// after the sequence ends reuse the last entry so callers cannot index OOB.
func statusSequenceHandler(statuses []int, headers ...http.Header) (http.HandlerFunc, *int) {
	var calls int
	return func(w http.ResponseWriter, _ *http.Request) {
		i := calls
		calls++
		if i >= len(statuses) {
			i = len(statuses) - 1
		}
		if i < len(headers) {
			for k, v := range headers[i] {
				for _, vv := range v {
					w.Header().Add(k, vv)
				}
			}
		}
		w.WriteHeader(statuses[i])
		_, _ = fmt.Fprintf(w, "attempt %d\n", calls)
	}, &calls
}

func TestDoWithRetry_Retries5xxThenSucceeds(t *testing.T) {
	handler, calls := statusSequenceHandler([]int{500, 500, 200})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c := retryTestClient(srv)
	resp, err := c.doWithRetry(context.Background(), http.MethodGet, srv.URL+"/x", "test")
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if *calls != 3 {
		t.Errorf("calls = %d, want 3 (two failures + one success)", *calls)
	}
}

func TestDoWithRetry_Retries429ThenSucceeds(t *testing.T) {
	handler, calls := statusSequenceHandler([]int{429, 200})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c := retryTestClient(srv)
	resp, err := c.doWithRetry(context.Background(), http.MethodGet, srv.URL+"/x", "test")
	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if *calls != 2 {
		t.Errorf("calls = %d, want 2", *calls)
	}
}

func TestDoWithRetry_ExhaustedReturnsError(t *testing.T) {
	handler, calls := statusSequenceHandler([]int{500, 500, 500, 500})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c := retryTestClient(srv)
	resp, err := c.doWithRetry(context.Background(), http.MethodGet, srv.URL+"/x", "test")
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected error after exhausting attempts, got nil")
	}
	if !strings.Contains(err.Error(), "after 3 attempts") {
		t.Errorf("error = %v, want reference to attempt count", err)
	}
	if *calls != 3 {
		t.Errorf("calls = %d, want 3 (MaxAttempts from DefaultRetryConfig)", *calls)
	}
}

func TestDoWithRetry_RespectsRetryAfterHeader(t *testing.T) {
	// The server asks for a 250ms wait on the first 429, then returns 200.
	// DefaultRetryConfig's base delay is 100ms — without Retry-After we'd
	// wait ~100ms. With Retry-After honoured we expect to wait ≥250ms.
	// Retry-After is in whole seconds per RFC 9110, so 1s is the smallest
	// value that exercises the path.
	handler, _ := statusSequenceHandler(
		[]int{429, 200},
		http.Header{"Retry-After": []string{"1"}},
		http.Header{},
	)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c := retryTestClient(srv)
	start := time.Now()
	resp, err := c.doWithRetry(context.Background(), http.MethodGet, srv.URL+"/x", "test")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if elapsed < 900*time.Millisecond {
		t.Errorf("elapsed = %v, expected retry to wait ~1s for Retry-After", elapsed)
	}
}

func TestDoWithRetry_ContextCancellationInterrupts(t *testing.T) {
	// Server always returns 500 so we stay in the retry loop.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	c := retryTestClient(srv)
	start := time.Now()
	_, err := c.doWithRetry(ctx, http.MethodGet, srv.URL+"/x", "test")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected context error, got nil")
	}
	// Should exit promptly once the context expires during backoff — well
	// before 300ms (the total budget of 3 attempts with 100ms + 200ms waits).
	if elapsed > 250*time.Millisecond {
		t.Errorf("elapsed = %v, expected prompt exit on ctx cancellation", elapsed)
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"abc", 0},
		{"0", 0},
		{"-1", 0},
		{"5", 5 * time.Second},
		{" 10 ", 10 * time.Second},
	}
	for _, tt := range tests {
		if got := parseRetryAfter(tt.in); got != tt.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}

	// HTTP-date in the past: 0; HTTP-date in the future: > 0.
	future := time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(future); got <= 0 || got > 30*time.Second {
		t.Errorf("parseRetryAfter(future) = %v, want >0 and <=30s", got)
	}
	past := time.Now().Add(-30 * time.Second).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(past); got != 0 {
		t.Errorf("parseRetryAfter(past) = %v, want 0", got)
	}
}

func TestWithJitter_BoundsAndZero(t *testing.T) {
	if got := withJitter(0); got != 0 {
		t.Errorf("withJitter(0) = %v, want 0", got)
	}
	// Single-ns durations have a zero half-range and must round-trip.
	if got := withJitter(time.Nanosecond); got != time.Nanosecond {
		t.Errorf("withJitter(1ns) = %v, want 1ns", got)
	}
	// Bulk test: 1000 samples must all fall within ±25% of 400ms.
	base := 400 * time.Millisecond
	for range 1000 {
		got := withJitter(base)
		if got < 300*time.Millisecond || got > 500*time.Millisecond {
			t.Fatalf("withJitter(%v) = %v out of ±25%% bounds", base, got)
		}
	}
}
