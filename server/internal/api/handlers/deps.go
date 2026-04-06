// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package handlers implements the HTTP handlers for the Vrata REST API.
// All handlers share a Dependencies struct injected at construction time.
package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/achetronic/vrata/internal/api/respond"
	"github.com/achetronic/vrata/internal/model"
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

// storeError classifies a store error and writes the appropriate HTTP response.
// ErrNotFound → 404 with a safe message; anything else → 500 with a generic
// operation description. Raw Go errors are never exposed to the client.
func storeError(w http.ResponseWriter, err error, resource string, logger *slog.Logger) {
	if errors.Is(err, model.ErrNotFound) {
		respond.Error(w, http.StatusNotFound, resource+" not found", logger)
		return
	}
	logger.Error("store error", slog.String("resource", resource), slog.String("error", err.Error()))
	respond.Error(w, http.StatusInternalServerError, "internal error while accessing "+resource, logger)
}
