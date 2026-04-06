// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package proxy

import (
	"sync"
	"sync/atomic"
	"time"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState int32

const (
	// CircuitClosed allows all requests through (normal operation).
	CircuitClosed CircuitState = 0

	// CircuitOpen rejects all requests (upstream deemed unhealthy).
	CircuitOpen CircuitState = 1

	// CircuitHalfOpen allows a single probe request to test recovery.
	CircuitHalfOpen CircuitState = 2
)

const defaultFailureThreshold = 5
const defaultOpenDuration = 30 * time.Second

// CircuitBreaker tracks failures per destination and opens the circuit
// when thresholds are exceeded. It enforces four concurrency limits:
//   - maxConnections: total active requests (connections)
//   - maxPendingRequests: requests queued waiting when at maxConnections
//   - maxRequests: active requests in closed state (separate from connections)
//   - maxRetries: concurrent retry attempts across all requests
type CircuitBreaker struct {
	maxConnections     uint32
	maxPendingRequests uint32
	maxRequests        uint32
	maxRetries         uint32
	failureThreshold   int64

	activeConnections atomic.Int64
	activeRequests    atomic.Int64
	activePending     atomic.Int64
	activeRetries     atomic.Int64

	consecutiveFailures atomic.Int64
	state               atomic.Int32
	halfOpenAllowed     atomic.Int32
	lastFailure         time.Time
	mu                  sync.Mutex
	openDuration        time.Duration
}

// NewCircuitBreaker creates a circuit breaker with the given thresholds.
// failureThreshold is the number of consecutive failures to open the circuit (0 = default 5).
// openDuration is how long the circuit stays open ("" or invalid = default 30s).
func NewCircuitBreaker(maxConn, maxPending, maxReq, maxRetry, failThreshold uint32, openDur string) *CircuitBreaker {
	if maxConn == 0 {
		maxConn = 1024
	}
	if maxPending == 0 {
		maxPending = 1024
	}
	if maxReq == 0 {
		maxReq = 1024
	}
	if maxRetry == 0 {
		maxRetry = 3
	}
	ft := int64(defaultFailureThreshold)
	if failThreshold > 0 {
		ft = int64(failThreshold)
	}
	od := defaultOpenDuration
	if openDur != "" {
		if d, err := time.ParseDuration(openDur); err == nil {
			od = d
		}
	}
	return &CircuitBreaker{
		maxConnections:     maxConn,
		maxPendingRequests: maxPending,
		maxRequests:        maxReq,
		maxRetries:         maxRetry,
		failureThreshold:   ft,
		openDuration:       od,
	}
}

// Allow checks if a request should be allowed through.
func (cb *CircuitBreaker) Allow() bool {
	state := CircuitState(cb.state.Load())

	switch state {
	case CircuitOpen:
		cb.mu.Lock()
		elapsed := time.Since(cb.lastFailure)
		cb.mu.Unlock()
		if elapsed > cb.openDuration {
			if cb.state.CompareAndSwap(int32(CircuitOpen), int32(CircuitHalfOpen)) {
				cb.halfOpenAllowed.Store(1)
			}
			return cb.halfOpenAllowed.Add(-1) >= 0
		}
		return false

	case CircuitHalfOpen:
		return cb.halfOpenAllowed.Add(-1) >= 0
	}

	if cb.activeConnections.Load() >= int64(cb.maxConnections) {
		return cb.activePending.Load() < int64(cb.maxPendingRequests)
	}
	if cb.activeRequests.Load() >= int64(cb.maxRequests) {
		return false
	}

	return true
}

// AllowPending checks if a pending (queued) request should be allowed.
// Returns true if the pending queue is not full.
func (cb *CircuitBreaker) AllowPending() bool {
	return cb.activePending.Load() < int64(cb.maxPendingRequests)
}

// OnPending increments the pending request count.
func (cb *CircuitBreaker) OnPending() {
	cb.activePending.Add(1)
}

// OnPendingComplete decrements the pending request count.
func (cb *CircuitBreaker) OnPendingComplete() {
	cb.activePending.Add(-1)
}

// AllowRetry checks if a retry attempt should be allowed based on
// the maxRetries concurrency limit.
func (cb *CircuitBreaker) AllowRetry() bool {
	return cb.activeRetries.Load() < int64(cb.maxRetries)
}

// OnRetry increments the active retry count.
func (cb *CircuitBreaker) OnRetry() {
	cb.activeRetries.Add(1)
}

// OnRetryComplete decrements the active retry count.
func (cb *CircuitBreaker) OnRetryComplete() {
	cb.activeRetries.Add(-1)
}

// RecordSuccess marks a successful request. Closes the circuit if half-open.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.consecutiveFailures.Store(0)
	cb.state.Store(int32(CircuitClosed))
}

// RecordFailure marks a failed request. Opens the circuit after consecutive failures.
func (cb *CircuitBreaker) RecordFailure() {
	failures := cb.consecutiveFailures.Add(1)
	if failures >= cb.failureThreshold {
		cb.state.Store(int32(CircuitOpen))
		cb.mu.Lock()
		cb.lastFailure = time.Now()
		cb.mu.Unlock()
	}
}

// OnRequest increments active request count. Call OnComplete when done.
func (cb *CircuitBreaker) OnRequest() {
	cb.activeRequests.Add(1)
	cb.activeConnections.Add(1)
}

// OnComplete decrements active request count.
func (cb *CircuitBreaker) OnComplete() {
	cb.activeRequests.Add(-1)
	cb.activeConnections.Add(-1)
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	return CircuitState(cb.state.Load())
}

// ActiveRetries returns the current active retry count for metrics.
func (cb *CircuitBreaker) ActiveRetries() int64 {
	return cb.activeRetries.Load()
}

// ActivePending returns the current pending request count for metrics.
func (cb *CircuitBreaker) ActivePending() int64 {
	return cb.activePending.Load()
}
