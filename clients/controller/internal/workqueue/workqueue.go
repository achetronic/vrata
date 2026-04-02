// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package workqueue implements an ordered FIFO work queue for the controller's
// reconcile loop. It distinguishes between individual HTTPRoutes (SingleItems)
// and groups of HTTPRoutes that share a vrata.io/batch annotation (BatchGroups).
//
// The queue is strictly sequential: the head element must complete before the
// next element is processed. While a BatchGroup is Accumulating, the queue is
// blocked — no SingleItem or other BatchGroup is processed.
package workqueue

import (
	"log/slog"
	"time"
)

const (
	// AnnotationBatch is the annotation key that marks an HTTPRoute as part of
	// a named batch group.
	AnnotationBatch = "vrata.io/batch"

	// AnnotationBatchSize is the optional annotation key that tells the controller
	// how many HTTPRoutes to expect in the batch group. When present, the group
	// transitions to Ready as soon as count == size, without waiting for the idle
	// timeout. If the idle timeout expires before all members arrive, the controller
	// logs an error and flushes anyway.
	AnnotationBatchSize = "vrata.io/batch-size"
)

// ItemKind identifies whether a work item is a single route or a batch group.
type ItemKind int

const (
	// KindSingle is an HTTPRoute without a batch annotation.
	KindSingle ItemKind = iota

	// KindBatch is a group of HTTPRoutes sharing the same vrata.io/batch value.
	KindBatch
)

// RouteRef identifies a single HTTPRoute, GRPCRoute, or SuperHTTPRoute by
// namespace and name.
type RouteRef struct {
	Namespace string
	Name      string
	// Super is true for SuperHTTPRoute resources.
	Super bool
	// GRPC is true for GRPCRoute resources.
	GRPC bool
}

// Item is one element in the work queue. It is either a single route or a
// batch group. Use Kind to discriminate.
type Item struct {
	Kind ItemKind

	// Single is set when Kind == KindSingle.
	Single *RouteRef

	// Batch is set when Kind == KindBatch.
	Batch *BatchGroup
}

// BatchGroup holds the state for a named batch of HTTPRoutes.
type BatchGroup struct {
	// Name is the value of the vrata.io/batch annotation.
	Name string

	// ExpectedSize is parsed from vrata.io/batch-size on the first member seen.
	// Zero means unknown (no annotation present).
	ExpectedSize int

	// Members are the HTTPRoutes seen so far in this group.
	Members []RouteRef

	// lastSeen is the time the last member was observed. Used for idle timeout.
	lastSeen time.Time

	// firstSeen is the time the first member was observed.
	firstSeen time.Time

	// ready is true when the group has been marked ready for reconciliation.
	ready bool
}

// IsReady returns true if the batch group is ready to be reconciled.
// A group is ready when:
//   - ExpectedSize > 0 and len(Members) >= ExpectedSize, OR
//   - the idle timeout has elapsed since the last member arrived.
func (bg *BatchGroup) IsReady(idleTimeout time.Duration) bool {
	if bg.ready {
		return true
	}
	if bg.ExpectedSize > 0 && len(bg.Members) >= bg.ExpectedSize {
		return true
	}
	return time.Since(bg.lastSeen) >= idleTimeout
}

// MarkReady forces the group into the ready state (used for failsafe logging).
func (bg *BatchGroup) MarkReady() {
	bg.ready = true
}

// IsIncomplete returns true when the group has a known expected size but has
// not received all members before becoming ready via idle timeout.
func (bg *BatchGroup) IsIncomplete() bool {
	return bg.ExpectedSize > 0 && len(bg.Members) < bg.ExpectedSize
}

// Queue is an ordered FIFO work queue. Items are appended in order of first
// observation and processed strictly head-first.
//
// Queue is not safe for concurrent use — callers must synchronise externally.
type Queue struct {
	items  []*Item
	byKey  map[string]*Item // batch group name → item pointer (for fast lookup)
	logger *slog.Logger
}

// New creates an empty Queue.
func New(logger *slog.Logger) *Queue {
	return &Queue{
		byKey:  make(map[string]*Item),
		logger: logger,
	}
}

// Observe registers the presence of an HTTPRoute in the current informer snapshot.
// If the route has a vrata.io/batch annotation, it is added to the corresponding
// BatchGroup (creating it if this is the first member). Otherwise it is enqueued
// as a SingleItem if not already present.
//
// knownSingles is the set of SingleItem keys (namespace/name) already in the queue,
// used to avoid duplicating single items across ticks.
func (q *Queue) Observe(ref RouteRef, annotations map[string]string, knownSingles map[string]bool) {
	batchName, hasBatch := annotations[AnnotationBatch]

	if !hasBatch {
		key := ref.Namespace + "/" + ref.Name
		if knownSingles[key] {
			return
		}
		knownSingles[key] = true
		q.items = append(q.items, &Item{Kind: KindSingle, Single: &ref})
		return
	}

	existing, ok := q.byKey[batchName]
	if !ok {
		bg := &BatchGroup{
			Name:      batchName,
			lastSeen:  time.Now(),
			firstSeen: time.Now(),
		}
		if sizeStr, ok := annotations[AnnotationBatchSize]; ok {
			bg.ExpectedSize = parseSize(sizeStr, batchName, q.logger)
		}
		item := &Item{Kind: KindBatch, Batch: bg}
		q.items = append(q.items, item)
		q.byKey[batchName] = item
		existing = item
	}

	bg := existing.Batch

	// Warn if batch-size is inconsistent across members.
	if sizeStr, ok := annotations[AnnotationBatchSize]; ok {
		size := parseSize(sizeStr, batchName, q.logger)
		if bg.ExpectedSize == 0 {
			bg.ExpectedSize = size
		} else if size != bg.ExpectedSize {
			q.logger.Warn("workqueue: inconsistent batch-size annotation across members, using first value",
				slog.String("batch", batchName),
				slog.Int("first", bg.ExpectedSize),
				slog.Int("this", size),
			)
		}
	}

	// Add member if not already present.
	for _, m := range bg.Members {
		if m.Namespace == ref.Namespace && m.Name == ref.Name {
			return
		}
	}
	bg.Members = append(bg.Members, ref)
	bg.lastSeen = time.Now()
}

// Head returns the first item in the queue, or nil if empty.
func (q *Queue) Head() *Item {
	if len(q.items) == 0 {
		return nil
	}
	return q.items[0]
}

// Pop removes and returns the first item in the queue.
func (q *Queue) Pop() *Item {
	if len(q.items) == 0 {
		return nil
	}
	item := q.items[0]
	q.items = q.items[1:]
	if item.Kind == KindBatch {
		delete(q.byKey, item.Batch.Name)
	}
	return item
}

// Len returns the number of items in the queue.
func (q *Queue) Len() int {
	return len(q.items)
}

// HasBatch returns true if a batch group with the given name is in the queue.
func (q *Queue) HasBatch(name string) bool {
	_, ok := q.byKey[name]
	return ok
}

// parseSize parses the batch-size annotation value. Returns 0 on error.
func parseSize(s, batchName string, logger *slog.Logger) int {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			logger.Warn("workqueue: invalid batch-size annotation value, ignoring",
				slog.String("batch", batchName),
				slog.String("value", s),
			)
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
