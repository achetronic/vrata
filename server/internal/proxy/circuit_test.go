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
	p1 := cb.OnRequest()
	p2 := cb.OnRequest()
	if cb.Allow() {
		t.Error("should reject when at maxRequests")
	}
	cb.OnComplete(p2)
	if !cb.Allow() {
		t.Error("should allow after completing a request")
	}
	cb.OnComplete(p1)
}

func TestCircuitBreakerMaxConnections(t *testing.T) {
	cb := NewCircuitBreaker(2, 1024, 1024, 3, 0, "")
	cb.OnRequest()
	cb.OnRequest()
	if !cb.Allow() {
		t.Error("should allow to pending queue when at maxConnections but pending has capacity")
	}
	// Third request enters pending because connections are full.
	p := cb.OnRequest()
	if !p {
		t.Error("third request should be tracked as pending")
	}
	cb.OnComplete(p)

	cb2 := NewCircuitBreaker(1, 1, 1024, 3, 0, "")
	cb2.OnRequest()
	// One pending fills the pending queue.
	p2 := cb2.OnRequest()
	if !p2 {
		t.Error("should be pending when connections full")
	}
	if cb2.Allow() {
		t.Error("should reject when at maxConnections and pending queue full")
	}
	cb2.OnComplete(p2)
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

func TestCircuitBreakerPendingLifecycle(t *testing.T) {
	// maxConn=2, maxPending=2
	cb := NewCircuitBreaker(2, 2, 1024, 3, 0, "")

	// Fill connections.
	p1 := cb.OnRequest()
	if p1 {
		t.Error("first request should not be pending")
	}
	p2 := cb.OnRequest()
	if p2 {
		t.Error("second request should not be pending")
	}

	// Next request overflows to pending.
	p3 := cb.OnRequest()
	if !p3 {
		t.Error("third request should be pending (connections full)")
	}
	if cb.activePending.Load() != 1 {
		t.Errorf("expected 1 pending, got %d", cb.activePending.Load())
	}

	p4 := cb.OnRequest()
	if !p4 {
		t.Error("fourth request should be pending")
	}
	if cb.activePending.Load() != 2 {
		t.Errorf("expected 2 pending, got %d", cb.activePending.Load())
	}

	// Pending queue full — Allow should reject.
	if cb.Allow() {
		t.Error("should reject when connections full and pending queue full")
	}

	// Complete one pending — Allow should pass again.
	cb.OnComplete(p3)
	if !cb.Allow() {
		t.Error("should allow after freeing one pending slot")
	}

	// Complete a connection — new request should be connection, not pending.
	cb.OnComplete(p1)
	p5 := cb.OnRequest()
	if p5 {
		t.Error("should be a connection after freeing a connection slot")
	}

	// Cleanup.
	cb.OnComplete(p2)
	cb.OnComplete(p4)
	cb.OnComplete(p5)

	if cb.activeConnections.Load() != 0 {
		t.Errorf("expected 0 connections after cleanup, got %d", cb.activeConnections.Load())
	}
	if cb.activePending.Load() != 0 {
		t.Errorf("expected 0 pending after cleanup, got %d", cb.activePending.Load())
	}
}

func TestCircuitBreakerPendingCounterIsolation(t *testing.T) {
	// Verify pending and connection counters are truly independent.
	cb := NewCircuitBreaker(1, 1, 1024, 3, 0, "")

	conn := cb.OnRequest()
	if conn {
		t.Error("first request should be a connection")
	}
	pending := cb.OnRequest()
	if !pending {
		t.Error("second request should be pending")
	}

	if cb.activeConnections.Load() != 1 {
		t.Errorf("expected 1 connection, got %d", cb.activeConnections.Load())
	}
	if cb.activePending.Load() != 1 {
		t.Errorf("expected 1 pending, got %d", cb.activePending.Load())
	}

	// Completing pending must not affect connections.
	cb.OnComplete(pending)
	if cb.activeConnections.Load() != 1 {
		t.Errorf("connection counter should not change, got %d", cb.activeConnections.Load())
	}
	if cb.activePending.Load() != 0 {
		t.Errorf("expected 0 pending, got %d", cb.activePending.Load())
	}

	// Completing connection must not affect pending.
	cb.OnComplete(conn)
	if cb.activeConnections.Load() != 0 {
		t.Errorf("expected 0 connections, got %d", cb.activeConnections.Load())
	}
}
