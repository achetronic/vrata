package proxy

import "testing"

func TestCircuitBreakerAllowsInitially(t *testing.T) {
	cb := NewCircuitBreaker(10, 10, 10, 3, 0, "")
	if !cb.Allow() {
		t.Error("should allow initially")
	}
}

func TestCircuitBreakerOpensAfterFailures(t *testing.T) {
	cb := NewCircuitBreaker(10, 10, 10, 3, 0, "")

	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}

	if cb.Allow() {
		t.Error("should be open after 5 failures (default threshold)")
	}
}

func TestCircuitBreakerRecovery(t *testing.T) {
	cb := NewCircuitBreaker(10, 10, 10, 3, 0, "0s")

	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}

	if !cb.Allow() {
		t.Error("should allow in half-open state (openDuration=0)")
	}

	cb.RecordSuccess()

	if !cb.Allow() {
		t.Error("should be closed after success")
	}
}

func TestCircuitBreakerCustomThreshold(t *testing.T) {
	cb := NewCircuitBreaker(10, 10, 10, 3, 3, "")

	for i := 0; i < 2; i++ {
		cb.RecordFailure()
	}
	if !cb.Allow() {
		t.Error("should still be closed after 2 failures (threshold=3)")
	}

	cb.RecordFailure()
	if cb.Allow() {
		t.Error("should be open after 3 failures (threshold=3)")
	}
}

func TestCircuitBreakerCustomOpenDuration(t *testing.T) {
	cb := NewCircuitBreaker(10, 10, 10, 3, 0, "0s")

	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}

	if !cb.Allow() {
		t.Error("should transition to half-open immediately (openDuration=0s)")
	}
}
