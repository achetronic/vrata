// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/achetronic/vrata/internal/api/respond"
	"github.com/achetronic/vrata/internal/model"
	"github.com/achetronic/vrata/internal/resolve"
	"github.com/google/uuid"
)

// ListSnapshots returns summary metadata for all versioned snapshots.
//
// @Summary     List snapshots
// @Description Returns summary metadata (id, name, createdAt, active) for all versioned snapshots.
// @Tags        snapshots
// @Produce     json
// @Success     200 {array}   model.SnapshotSummary
// @Failure     500 {object}  respond.ErrorBody
// @Router      /snapshots [get]
func (d *Dependencies) HandleListSnapshots(w http.ResponseWriter, r *http.Request) {
	summaries, err := d.Store.ListSnapshots(r.Context())
	if err != nil {
		storeError(w, err, "snapshots", d.Logger)
		return
	}
	respond.JSON(w, http.StatusOK, summaries, d.Logger)
}

// CreateSnapshot captures the current live configuration as an immutable
// versioned snapshot.
//
// @Summary     Create a snapshot
// @Description Captures the current live configuration (listeners, routes, groups, destinations, middlewares) as an immutable versioned snapshot.
// @Tags        snapshots
// @Accept      json
// @Produce     json
// @Param       body body     SnapshotCreateRequest true "Snapshot metadata"
// @Success     201  {object} model.VersionedSnapshot
// @Failure     400  {object} respond.ErrorBody
// @Failure     500  {object} respond.ErrorBody
// @Router      /snapshots [post]
func (d *Dependencies) HandleCreateSnapshot(w http.ResponseWriter, r *http.Request) {
	var req SnapshotCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error(), d.Logger)
		return
	}

	if req.Name == "" {
		respond.Error(w, http.StatusBadRequest, "name is required", d.Logger)
		return
	}

	ctx := r.Context()

	snap, err := buildSnapshot(ctx, d)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "building snapshot: "+err.Error(), d.Logger)
		return
	}

	resolvedSnap, err := resolveSecrets(ctx, d, snap)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "resolving secrets: "+err.Error(), d.Logger)
		return
	}

	vs := model.VersionedSnapshot{
		ID:        uuid.NewString(),
		Name:      req.Name,
		CreatedAt: time.Now().UTC(),
		Snapshot:  *resolvedSnap,
	}

	if err := d.Store.SaveSnapshot(ctx, vs); err != nil {
		storeError(w, err, "snapshot", d.Logger)
		return
	}

	respond.JSON(w, http.StatusCreated, vs, d.Logger)
}

// GetSnapshot returns the versioned snapshot with the given ID.
//
// @Summary     Get a snapshot
// @Description Returns the full versioned snapshot with the given ID, including all configuration entities.
// @Tags        snapshots
// @Produce     json
// @Param       snapshotId path     string true "Snapshot ID"
// @Success     200        {object} model.VersionedSnapshot
// @Failure     404        {object} respond.ErrorBody
// @Failure     500        {object} respond.ErrorBody
// @Router      /snapshots/{snapshotId} [get]
func (d *Dependencies) HandleGetSnapshot(w http.ResponseWriter, r *http.Request) {
	snapshotID := r.PathValue("snapshotId")

	vs, err := d.Store.GetSnapshot(r.Context(), snapshotID)
	if err != nil {
		storeError(w, err, "snapshot", d.Logger)
		return
	}
	respond.JSON(w, http.StatusOK, vs, d.Logger)
}

// DeleteSnapshot removes the versioned snapshot with the given ID.
// If the deleted snapshot was the active one, the active pointer is cleared
// and proxies will stop receiving configuration until a new snapshot is activated.
//
// @Summary     Delete a snapshot
// @Description Deletes the versioned snapshot with the given ID. If it was the active snapshot, the active pointer is cleared.
// @Tags        snapshots
// @Produce     json
// @Param       snapshotId path     string true "Snapshot ID"
// @Success     204        "No Content"
// @Failure     404        {object} respond.ErrorBody
// @Failure     500        {object} respond.ErrorBody
// @Router      /snapshots/{snapshotId} [delete]
func (d *Dependencies) HandleDeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	snapshotID := r.PathValue("snapshotId")

	if err := d.Store.DeleteSnapshot(r.Context(), snapshotID); err != nil {
		storeError(w, err, "snapshot", d.Logger)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ActivateSnapshot marks the given snapshot as the active configuration.
// The SSE stream will immediately start serving this snapshot to all
// connected proxies.
//
// @Summary     Activate a snapshot
// @Description Sets the given snapshot as the active configuration served to proxies via the SSE stream.
// @Tags        snapshots
// @Produce     json
// @Param       snapshotId path     string true "Snapshot ID"
// @Success     200        {object} model.SnapshotSummary
// @Failure     404        {object} respond.ErrorBody
// @Failure     500        {object} respond.ErrorBody
// @Router      /snapshots/{snapshotId}/activate [post]
func (d *Dependencies) HandleActivateSnapshot(w http.ResponseWriter, r *http.Request) {
	snapshotID := r.PathValue("snapshotId")
	ctx := r.Context()

	if err := d.Store.ActivateSnapshot(ctx, snapshotID); err != nil {
		storeError(w, err, "snapshot", d.Logger)
		return
	}

	vs, err := d.Store.GetSnapshot(ctx, snapshotID)
	if err != nil {
		storeError(w, err, "snapshot", d.Logger)
		return
	}

	respond.JSON(w, http.StatusOK, model.SnapshotSummary{
		ID:        vs.ID,
		Name:      vs.Name,
		CreatedAt: vs.CreatedAt,
		Active:    true,
	}, d.Logger)
}

// SnapshotCreateRequest is the request body for POST /snapshots.
type SnapshotCreateRequest struct {
	// Name is a human-readable label for the snapshot (e.g. "v1.0", "pre-deploy").
	Name string `json:"name" example:"v1.0"`
}

// resolveSecrets serializes the snapshot to JSON, resolves all
// {{secret:...}} patterns, and deserializes back. Returns the
// original snapshot unchanged if no patterns are found.
func resolveSecrets(ctx context.Context, d *Dependencies, snap *model.Snapshot) (*model.Snapshot, error) {
	data, err := json.Marshal(snap)
	if err != nil {
		return nil, fmt.Errorf("encoding snapshot: %w", err)
	}

	resolved, err := resolve.Secrets(ctx, d.Store, data)
	if err != nil {
		return nil, err
	}

	var out model.Snapshot
	if err := json.Unmarshal(resolved, &out); err != nil {
		return nil, fmt.Errorf("decoding resolved snapshot: %w", err)
	}
	return &out, nil
}
