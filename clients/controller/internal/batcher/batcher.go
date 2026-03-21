// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package batcher accumulates reconciler changes and triggers a Vrata
// snapshot when the debounce timer expires or the max batch size is reached.
package batcher

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/achetronic/vrata/clients/controller/internal/vrata"
)

// Batcher accumulates change signals and creates+activates a Vrata snapshot
// when the batch is flushed. The autoCreate and autoActivate flags control
// whether snapshots are created and activated automatically, or left for
// an external process to manage via the Vrata API.
type Batcher struct {
	client       *vrata.Client
	debounce     time.Duration
	maxBatch     int
	autoCreate   bool
	autoActivate bool
	logger       *slog.Logger

	mu      sync.Mutex
	pending int
	timer   *time.Timer
	counter int
}

// New creates a Batcher with the given debounce duration and max batch size.
func New(client *vrata.Client, debounce time.Duration, maxBatch int, autoCreate, autoActivate bool, logger *slog.Logger) *Batcher {
	return &Batcher{
		client:       client,
		debounce:     debounce,
		maxBatch:     maxBatch,
		autoCreate:   autoCreate,
		autoActivate: autoActivate,
		logger:       logger,
	}
}

// Signal records that a change was applied. If the max batch size is reached,
// a snapshot is created immediately. Otherwise, the debounce timer is reset.
func (b *Batcher) Signal(ctx context.Context) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.pending++
	b.counter++

	if b.pending >= b.maxBatch {
		b.flushLocked(ctx)
		return
	}

	if b.timer != nil {
		b.timer.Stop()
	}
	b.timer = time.AfterFunc(b.debounce, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.pending > 0 {
			b.flushLocked(ctx)
		}
	})
}

// Flush forces a snapshot if there are pending changes.
func (b *Batcher) Flush(ctx context.Context) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending > 0 {
		b.flushLocked(ctx)
	}
}

// Pending returns the number of unsnapshotted changes.
func (b *Batcher) Pending() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pending
}

// TotalSignals returns the total number of signals received (for metrics).
func (b *Batcher) TotalSignals() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.counter
}

// flushLocked creates and optionally activates a snapshot. Must be called with mu held.
// When autoCreate is false, pending changes are cleared without creating a snapshot.
// When autoActivate is false, the snapshot is created but not activated.
func (b *Batcher) flushLocked(ctx context.Context) {
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}

	changes := b.pending
	b.pending = 0

	if !b.autoCreate {
		b.logger.Info("batcher: snapshot creation disabled, clearing pending changes",
			slog.Int("changes", changes),
		)
		return
	}

	name := fmt.Sprintf("vrata-controller-%d", time.Now().UnixMilli())
	snap, err := b.client.CreateSnapshot(ctx, name)
	if err != nil {
		b.logger.Error("batcher: failed to create snapshot",
			slog.String("error", err.Error()),
			slog.Int("pending", changes),
		)
		return
	}

	if !b.autoActivate {
		b.logger.Info("batcher: snapshot created (auto-activate disabled, awaiting manual activation)",
			slog.String("id", snap.ID),
			slog.String("name", name),
			slog.Int("changes", changes),
		)
		return
	}

	if err := b.client.ActivateSnapshot(ctx, snap.ID); err != nil {
		b.logger.Error("batcher: failed to activate snapshot",
			slog.String("id", snap.ID),
			slog.String("error", err.Error()),
		)
		return
	}

	b.logger.Info("batcher: snapshot activated",
		slog.String("id", snap.ID),
		slog.String("name", name),
		slog.Int("changes", changes),
	)
}
