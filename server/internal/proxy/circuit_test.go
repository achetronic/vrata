// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

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

func TestCircuitBreakerMaxRequests(t *testing.T) {
	cb := NewCircuitBreaker(1024, 1024, 2, 3, 0, "")
	cb.OnRequest()
	cb.OnRequest()
	if cb.Allow() {
		t.Error("should reject when at maxRequests")
	}
	cb.OnComplete()
	if !cb.Allow() {
		t.Error("should allow after completing a request")
	}
}

func TestCircuitBreakerMaxConnections(t *testing.T) {
	cb := NewCircuitBreaker(2, 1024, 1024, 3, 0, "")
	cb.OnRequest()
	cb.OnRequest()
	if !cb.Allow() {
		t.Error("should allow to pending queue when at maxConnections but pending has capacity")
	}

	cb2 := NewCircuitBreaker(1, 1, 1024, 3, 0, "")
	cb2.OnRequest()
	cb2.activePending.Store(1)
	if cb2.Allow() {
		t.Error("should reject when at maxConnections and pending queue full")
	}
}

func TestCircuitBreakerAllowRetry(t *testing.T) {
	cb := NewCircuitBreaker(10, 10, 10, 2, 0, "")
	if !cb.AllowRetry() {
		t.Error("should allow retry initially")
	}
	cb.OnRetry()
	cb.OnRetry()
	if cb.AllowRetry() {
		t.Error("should reject retry when at maxRetries=2")
	}
	cb.OnRetryComplete()
	if !cb.AllowRetry() {
		t.Error("should allow retry after one completes")
	}
}
