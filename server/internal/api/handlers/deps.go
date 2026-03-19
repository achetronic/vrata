// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package handlers implements the HTTP handlers for the Vrata REST API.
// All handlers share a Dependencies struct injected at construction time.
package handlers

import (
	"log/slog"

	"github.com/achetronic/vrata/internal/store"
)

// RaftApplier is the interface the Raft apply handler needs from the Raft node.
// It is satisfied by *raft.Node and allows the handler package to remain
// decoupled from the raft package.
type RaftApplier interface {
	// ApplyRaw applies a raw JSON-encoded command through the Raft log.
	// Only called when this node is the leader.
	ApplyRaw(data []byte) error
}

// Dependencies holds all external collaborators shared by the HTTP handlers.
type Dependencies struct {
	Store  store.Store
	Logger *slog.Logger
	// Raft is optional. When set, the internal Raft apply endpoint is active.
	Raft RaftApplier
}
