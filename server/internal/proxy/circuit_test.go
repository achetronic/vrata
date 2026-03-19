package proxy

import "testing"

func TestCircuitBreakerAllowsInitially(t *testing.T) {
	cb := NewCircuitBreaker(10, 10, 10, 3)
	if !cb.Allow() {
		t.Error("should allow initially")
	}
}

func TestCircuitBreakerOpensAfterFailures(t *testing.T) {
	cb := NewCircuitBreaker(10, 10, 10, 3)

	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}

	if cb.Allow() {
		t.Error("should be open after 5 failures")
	}
}

func TestCircuitBreakerRecovery(t *testing.T) {
	cb := NewCircuitBreaker(10, 10, 10, 3)
	cb.openDuration = 0 // instant recovery for test

	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}

	// Should transition to half-open since openDuration is 0.
	if !cb.Allow() {
		t.Error("should allow in half-open state")
	}

	cb.RecordSuccess()

	if !cb.Allow() {
		t.Error("should be closed after success")
	}
}
