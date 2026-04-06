// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package workqueue

import (
	"log/slog"
	"os"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestQueue_SingleItems(t *testing.T) {
	q := New(testLogger())
	known := make(map[string]bool)

	q.Observe(RouteRef{Namespace: "default", Name: "route-a"}, nil, known)
	q.Observe(RouteRef{Namespace: "default", Name: "route-b"}, nil, known)

	if q.Len() != 2 {
		t.Fatalf("expected 2 items, got %d", q.Len())
	}

	head := q.Head()
	if head.Kind != KindSingle || head.Single.Name != "route-a" {
		t.Errorf("expected route-a as head, got %+v", head)
	}

	q.Pop()
	head = q.Head()
	if head.Kind != KindSingle || head.Single.Name != "route-b" {
		t.Errorf("expected route-b as head, got %+v", head)
	}
}

func TestQueue_SingleItemDeduplication(t *testing.T) {
	q := New(testLogger())
	known := make(map[string]bool)

	q.Observe(RouteRef{Namespace: "default", Name: "route-a"}, nil, known)
	q.Observe(RouteRef{Namespace: "default", Name: "route-a"}, nil, known)

	if q.Len() != 1 {
		t.Errorf("expected 1 item after dedup, got %d", q.Len())
	}
}

func TestQueue_BatchGroupCreated(t *testing.T) {
	q := New(testLogger())
	known := make(map[string]bool)

	annotations := map[string]string{AnnotationBatch: "release-v1"}
	q.Observe(RouteRef{Namespace: "default", Name: "route-a"}, annotations, known)
	q.Observe(RouteRef{Namespace: "default", Name: "route-b"}, annotations, known)

	if q.Len() != 1 {
		t.Fatalf("expected 1 batch group item, got %d", q.Len())
	}

	head := q.Head()
	if head.Kind != KindBatch {
		t.Fatalf("expected KindBatch, got %d", head.Kind)
	}
	if head.Batch.Name != "release-v1" {
		t.Errorf("expected batch name release-v1, got %q", head.Batch.Name)
	}
	if len(head.Batch.Members) != 2 {
		t.Errorf("expected 2 members, got %d", len(head.Batch.Members))
	}
}

func TestQueue_BatchGroupMemberDeduplication(t *testing.T) {
	q := New(testLogger())
	known := make(map[string]bool)

	annotations := map[string]string{AnnotationBatch: "release-v1"}
	q.Observe(RouteRef{Namespace: "default", Name: "route-a"}, annotations, known)
	q.Observe(RouteRef{Namespace: "default", Name: "route-a"}, annotations, known)

	if len(q.Head().Batch.Members) != 1 {
		t.Errorf("expected 1 member after dedup, got %d", len(q.Head().Batch.Members))
	}
}

func TestQueue_BatchReadyByIdleTimeout(t *testing.T) {
	q := New(testLogger())
	known := make(map[string]bool)

	annotations := map[string]string{AnnotationBatch: "release-v1"}
	q.Observe(RouteRef{Namespace: "default", Name: "route-a"}, annotations, known)

	bg := q.Head().Batch

	// Not ready yet.
	if bg.IsReady(10 * time.Second) {
		t.Error("should not be ready immediately")
	}

	// Force lastSeen to the past.
	bg.lastSeen = time.Now().Add(-15 * time.Second)

	if !bg.IsReady(10 * time.Second) {
		t.Error("should be ready after idle timeout")
	}
}

func TestQueue_BatchReadyByExpectedSize(t *testing.T) {
	q := New(testLogger())
	known := make(map[string]bool)

	annotations := map[string]string{
		AnnotationBatch:     "release-v1",
		AnnotationBatchSize: "2",
	}
	q.Observe(RouteRef{Namespace: "default", Name: "route-a"}, annotations, known)

	bg := q.Head().Batch
	if bg.ExpectedSize != 2 {
		t.Fatalf("expected size 2, got %d", bg.ExpectedSize)
	}

	// Not ready with 1 of 2.
	if bg.IsReady(10 * time.Second) {
		t.Error("should not be ready with 1/2 members")
	}

	q.Observe(RouteRef{Namespace: "default", Name: "route-b"}, annotations, known)

	// Ready with 2 of 2 — no need to wait for idle timeout.
	if !bg.IsReady(10 * time.Second) {
		t.Error("should be ready with 2/2 members")
	}
}

func TestQueue_BatchIncomplete(t *testing.T) {
	q := New(testLogger())
	known := make(map[string]bool)

	annotations := map[string]string{
		AnnotationBatch:     "release-v1",
		AnnotationBatchSize: "5",
	}
	q.Observe(RouteRef{Namespace: "default", Name: "route-a"}, annotations, known)

	bg := q.Head().Batch
	bg.lastSeen = time.Now().Add(-15 * time.Second)

	// Ready by idle timeout but incomplete.
	if !bg.IsReady(10 * time.Second) {
		t.Error("should be ready by idle timeout")
	}
	if !bg.IsIncomplete() {
		t.Error("should be flagged as incomplete (got 1/5)")
	}
}

func TestQueue_BatchAndSingleOrdering(t *testing.T) {
	q := New(testLogger())
	known := make(map[string]bool)

	// Single first.
	q.Observe(RouteRef{Namespace: "default", Name: "single-a"}, nil, known)
	// Then batch.
	q.Observe(RouteRef{Namespace: "default", Name: "batch-a"}, map[string]string{AnnotationBatch: "rel-1"}, known)

	if q.Len() != 2 {
		t.Fatalf("expected 2 items, got %d", q.Len())
	}

	head := q.Head()
	if head.Kind != KindSingle {
		t.Error("single should be head (first observed)")
	}

	q.Pop()
	head = q.Head()
	if head.Kind != KindBatch {
		t.Error("batch should be second")
	}
}

func TestQueue_HasBatch(t *testing.T) {
	q := New(testLogger())
	known := make(map[string]bool)

	if q.HasBatch("rel-1") {
		t.Error("should not have batch before observe")
	}

	q.Observe(RouteRef{Namespace: "default", Name: "r"}, map[string]string{AnnotationBatch: "rel-1"}, known)

	if !q.HasBatch("rel-1") {
		t.Error("should have batch after observe")
	}

	q.Pop()

	if q.HasBatch("rel-1") {
		t.Error("should not have batch after pop")
	}
}

func TestQueue_PopEmpty(t *testing.T) {
	q := New(testLogger())
	if item := q.Pop(); item != nil {
		t.Error("pop on empty queue should return nil")
	}
	if item := q.Head(); item != nil {
		t.Error("head on empty queue should return nil")
	}
}

func TestBatchGroup_IdleTimeoutResetsOnNewMember(t *testing.T) {
	q := New(testLogger())
	known := make(map[string]bool)

	annotations := map[string]string{AnnotationBatch: "rel-1"}
	q.Observe(RouteRef{Namespace: "default", Name: "route-a"}, annotations, known)

	bg := q.Head().Batch
	// Force lastSeen into the past.
	bg.lastSeen = time.Now().Add(-15 * time.Second)

	// Observing a new member should reset lastSeen.
	q.Observe(RouteRef{Namespace: "default", Name: "route-b"}, annotations, known)

	// Should no longer be ready since lastSeen was just reset.
	if bg.IsReady(10 * time.Second) {
		t.Error("idle timeout should have been reset by new member arrival")
	}
}

func TestParseSize_Valid(t *testing.T) {
	l := testLogger()
	if n := parseSize("200", "test", l); n != 200 {
		t.Errorf("expected 200, got %d", n)
	}
	if n := parseSize("0", "test", l); n != 0 {
		t.Errorf("expected 0, got %d", n)
	}
	if n := parseSize("1", "test", l); n != 1 {
		t.Errorf("expected 1, got %d", n)
	}
}

func TestParseSize_Invalid(t *testing.T) {
	l := testLogger()
	if n := parseSize("abc", "test", l); n != 0 {
		t.Errorf("expected 0 for non-numeric, got %d", n)
	}
	if n := parseSize("12x", "test", l); n != 0 {
		t.Errorf("expected 0 for partially numeric, got %d", n)
	}
}

func TestParseSize_Empty(t *testing.T) {
	l := testLogger()
	if n := parseSize("", "test", l); n != 0 {
		t.Errorf("expected 0 for empty string, got %d", n)
	}
}

func TestQueue_InconsistentBatchSize(t *testing.T) {
	q := New(testLogger())
	known := make(map[string]bool)

	q.Observe(RouteRef{Namespace: "default", Name: "route-a"}, map[string]string{
		AnnotationBatch:     "rel-1",
		AnnotationBatchSize: "10",
	}, known)

	// Second member with different batch-size — should use first value.
	q.Observe(RouteRef{Namespace: "default", Name: "route-b"}, map[string]string{
		AnnotationBatch:     "rel-1",
		AnnotationBatchSize: "20",
	}, known)

	bg := q.Head().Batch
	if bg.ExpectedSize != 10 {
		t.Errorf("expected first seen size 10, got %d", bg.ExpectedSize)
	}
}

func TestQueue_BatchSizeFromLaterMember(t *testing.T) {
	q := New(testLogger())
	known := make(map[string]bool)

	// First member without batch-size.
	q.Observe(RouteRef{Namespace: "default", Name: "route-a"}, map[string]string{
		AnnotationBatch: "rel-1",
	}, known)

	// Second member with batch-size — should be picked up.
	q.Observe(RouteRef{Namespace: "default", Name: "route-b"}, map[string]string{
		AnnotationBatch:     "rel-1",
		AnnotationBatchSize: "5",
	}, known)

	bg := q.Head().Batch
	if bg.ExpectedSize != 5 {
		t.Errorf("expected size 5 from second member, got %d", bg.ExpectedSize)
	}
}

func TestQueue_SuperRouteRef(t *testing.T) {
	q := New(testLogger())
	known := make(map[string]bool)

	q.Observe(RouteRef{Namespace: "default", Name: "super-a", Super: true}, nil, known)

	head := q.Head()
	if head.Kind != KindSingle || !head.Single.Super {
		t.Error("expected SuperHTTPRoute flag to be preserved")
	}
}
