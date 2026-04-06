// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"

	"github.com/achetronic/vrata/internal/model"
)

// retryTransport wraps an http.RoundTripper with retry logic.
type retryTransport struct {
	inner          http.RoundTripper
	retry          *model.RouteRetry
	onRetry        func(req *http.Request, attempt int)
	circuitBreaker *CircuitBreaker
}

// newRetryTransport wraps a transport with retry behaviour.
// The optional circuitBreaker enforces the maxRetries concurrency limit.
func newRetryTransport(inner http.RoundTripper, retry *model.RouteRetry, onRetry func(*http.Request, int), cb *CircuitBreaker) http.RoundTripper {
	if retry == nil || retry.Attempts == 0 {
		return inner
	}
	return &retryTransport{inner: inner, retry: retry, onRetry: onRetry, circuitBreaker: cb}
}

// Unwrap returns the underlying transport for safe type unwrapping.
func (rt *retryTransport) Unwrap() http.RoundTripper {
	return rt.inner
}

func (rt *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Buffer the body so we can replay it on retries.
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body.Close()
	}

	maxAttempts := int(rt.retry.Attempts) + 1 // original + retries
	var lastErr error

	baseBackoff := 100 * time.Millisecond
	maxBackoff := 1 * time.Second
	if rt.retry.Backoff != nil {
		if rt.retry.Backoff.Base != "" {
			if d, err := time.ParseDuration(rt.retry.Backoff.Base); err == nil {
				baseBackoff = d
			}
		}
		if rt.retry.Backoff.Max != "" {
			if d, err := time.ParseDuration(rt.retry.Backoff.Max); err == nil {
				maxBackoff = d
			}
		}
	}

	perAttemptTimeout := time.Duration(0)
	if rt.retry.PerAttemptTimeout != "" {
		if d, err := time.ParseDuration(rt.retry.PerAttemptTimeout); err == nil {
			perAttemptTimeout = d
		}
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			if rt.circuitBreaker != nil {
				if !rt.circuitBreaker.AllowRetry() {
					return nil, lastErr
				}
				rt.circuitBreaker.OnRetry()
			}
			if rt.onRetry != nil {
				rt.onRetry(req, attempt)
			}
		}
		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		req.Header.Set("X-Request-Attempt-Count", fmt.Sprintf("%d", attempt+1))

		attemptReq := req
		if perAttemptTimeout > 0 {
			ctx, cancel := context.WithTimeout(req.Context(), perAttemptTimeout)
			attemptReq = req.WithContext(ctx)
			resp, err := rt.inner.RoundTrip(attemptReq)
			cancel()
			if attempt > 0 && rt.circuitBreaker != nil {
				rt.circuitBreaker.OnRetryComplete()
			}
			if err != nil {
				lastErr = err
				if attempt < maxAttempts-1 {
					if !sleepWithContext(req.Context(), calcBackoff(baseBackoff, maxBackoff, attempt)) {
						return nil, lastErr
					}
					continue
				}
				return nil, lastErr
			}
			if shouldRetry(resp.StatusCode, rt.retry) && attempt < maxAttempts-1 {
				resp.Body.Close()
				if !sleepWithContext(req.Context(), calcBackoff(baseBackoff, maxBackoff, attempt)) {
					return nil, lastErr
				}
				continue
			}
			return resp, nil
		}

		resp, err := rt.inner.RoundTrip(attemptReq)
		if attempt > 0 && rt.circuitBreaker != nil {
			rt.circuitBreaker.OnRetryComplete()
		}
		if err != nil {
			lastErr = err
			if attempt < maxAttempts-1 {
				if !sleepWithContext(req.Context(), calcBackoff(baseBackoff, maxBackoff, attempt)) {
					return nil, lastErr
				}
				continue
			}
			return nil, lastErr
		}

		if shouldRetry(resp.StatusCode, rt.retry) && attempt < maxAttempts-1 {
			resp.Body.Close()
			if !sleepWithContext(req.Context(), calcBackoff(baseBackoff, maxBackoff, attempt)) {
				return nil, lastErr
			}
			continue
		}

		return resp, nil
	}

	return nil, lastErr
}

func shouldRetry(status int, retry *model.RouteRetry) bool {
	if len(retry.On) == 0 {
		// Default: retry on server errors and connection failures.
		return status >= 500
	}

	for _, cond := range retry.On {
		switch cond {
		case model.RetryOnServerError:
			if status >= 500 {
				return true
			}
		case model.RetryOnGatewayError:
			if status == 502 || status == 503 || status == 504 {
				return true
			}
		case model.RetryOnRetriableCodes:
			for _, code := range retry.RetriableCodes {
				if uint32(status) == code {
					return true
				}
			}
		case model.RetryOnConnectionFailure:
			// Connection failures are handled by the transport error path,
			// not by status code. This case is a no-op here.
		}
	}

	return false
}

func calcBackoff(base, max time.Duration, attempt int) time.Duration {
	backoff := base * time.Duration(1<<uint(attempt))
	if backoff > max {
		backoff = max
	}
	// Add jitter: 50-100% of the calculated backoff.
	jitter := time.Duration(rand.Int63n(int64(backoff / 2)))
	return backoff/2 + jitter
}

// sleepWithContext waits for the given duration or until the context is done.
// Returns true if the sleep completed, false if the context was cancelled.
func sleepWithContext(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
