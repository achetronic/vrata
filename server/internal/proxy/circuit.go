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
// when thresholds are exceeded.
type CircuitBreaker struct {
	maxConnections     uint32
	maxPendingRequests uint32
	maxRequests        uint32
	maxRetries         uint32

	activeConnections atomic.Int64
	activeRequests    atomic.Int64

	consecutiveFailures atomic.Int64
	state               atomic.Int32
	halfOpenAllowed     atomic.Int32
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
		openDuration:       defaultOpenDuration,
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
		return false
	}
	if cb.activeRequests.Load() >= int64(cb.maxRequests) {
		return false
	}

	return true
}

// RecordSuccess marks a successful request. Closes the circuit if half-open.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.consecutiveFailures.Store(0)
	cb.state.Store(int32(CircuitClosed))
}

// RecordFailure marks a failed request. Opens the circuit after consecutive failures.
func (cb *CircuitBreaker) RecordFailure() {
	failures := cb.consecutiveFailures.Add(1)
	if failures >= defaultFailureThreshold {
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
