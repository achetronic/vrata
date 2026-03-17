package proxy

import (
	"sync"
	"sync/atomic"
	"time"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState int32

const (
	CircuitClosed   CircuitState = 0 // normal operation
	CircuitOpen     CircuitState = 1 // failing, reject requests
	CircuitHalfOpen CircuitState = 2 // testing recovery
)

// CircuitBreaker tracks failures per destination and opens the circuit
// when thresholds are exceeded.
type CircuitBreaker struct {
	maxConnections     uint32
	maxPendingRequests uint32
	maxRequests        uint32
	maxRetries         uint32

	activeConnections atomic.Int64
	pendingRequests   atomic.Int64
	activeRequests    atomic.Int64
	activeRetries     atomic.Int64

	consecutiveFailures atomic.Int64
	state               atomic.Int32
	lastFailure         time.Time
	mu                  sync.Mutex
	openDuration        time.Duration
}

// NewCircuitBreaker creates a circuit breaker with the given thresholds.
func NewCircuitBreaker(maxConn, maxPending, maxReq, maxRetry uint32) *CircuitBreaker {
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
	return &CircuitBreaker{
		maxConnections:     maxConn,
		maxPendingRequests: maxPending,
		maxRequests:        maxReq,
		maxRetries:         maxRetry,
		openDuration:       30 * time.Second,
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
			cb.state.CompareAndSwap(int32(CircuitOpen), int32(CircuitHalfOpen))
			return true
		}
		return false

	case CircuitHalfOpen:
		// Allow one request through to test.
		return true
	}

	// Closed — check thresholds.
	if cb.activeConnections.Load() >= int64(cb.maxConnections) {
		return false
	}
	if cb.activeRequests.Load() >= int64(cb.maxRequests) {
		return false
	}

	return true
}

// RecordSuccess marks a successful request.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.consecutiveFailures.Store(0)
	cb.state.Store(int32(CircuitClosed))
}

// RecordFailure marks a failed request.
func (cb *CircuitBreaker) RecordFailure() {
	failures := cb.consecutiveFailures.Add(1)
	if failures >= 5 {
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
