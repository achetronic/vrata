// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package handlers wires HTTP request/response logic for the groups resource.
// A group references routes by ID and optionally adds extra matching constraints
// (PathPrefix, Hostnames, Headers) on top of all the referenced routes.
package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/achetronic/vrata/internal/api/respond"
	"github.com/achetronic/vrata/internal/model"

	"github.com/google/uuid"
)

// ListGroups returns all groups stored in the database.
//
// @Summary     List groups
// @Description Returns the full list of route groups.
// @Tags        groups
// @Produce     json
// @Success     200 {array}   model.RouteGroup
// @Failure     500 {object}  respond.ErrorBody
// @Router      /groups [get]
func (d *Dependencies) HandleListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := d.Store.ListGroups(r.Context())
	if err != nil {
		storeError(w, err, "groups", d.Logger)
		return
	}
	respond.JSON(w, http.StatusOK, groups, d.Logger)
}

// CreateGroup creates a new route group.
//
// @Summary     Create a group
// @Description Creates a new group referencing existing routes by ID.
// @Tags        groups
// @Accept      json
// @Produce     json
// @Param       group body      model.RouteGroup true "Group definition"
// @Success     201   {object}  model.RouteGroup
// @Failure     400   {object}  respond.ErrorBody
// @Failure     500   {object}  respond.ErrorBody
// @Router      /groups [post]
func (d *Dependencies) HandleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var group model.RouteGroup
	if err := json.NewDecoder(r.Body).Decode(&group); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error(), d.Logger)
		return
	}

	if group.ID == "" {
		group.ID = uuid.NewString()
	}

	if err := validateGroup(group); err != nil {
		respond.Error(w, http.StatusBadRequest, err.Error(), d.Logger)
		return
	}

	if err := d.Store.SaveGroup(r.Context(), group); err != nil {
		storeError(w, err, "group", d.Logger)
		return
	}

	respond.JSON(w, http.StatusCreated, group, d.Logger)
}

// GetGroup returns the group identified by groupId.
//
// @Summary     Get a group
// @Description Returns the group with the given ID.
// @Tags        groups
// @Produce     json
// @Param       groupId path     string true "Group ID"
// @Success     200     {object} model.RouteGroup
// @Failure     404     {object} respond.ErrorBody
// @Failure     500     {object} respond.ErrorBody
// @Router      /groups/{groupId} [get]
func (d *Dependencies) HandleGetGroup(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupId")

	group, err := d.Store.GetGroup(r.Context(), groupID)
	if err != nil {
		storeError(w, err, "group", d.Logger)
		return
	}
	respond.JSON(w, http.StatusOK, group, d.Logger)
}

// UpdateGroup replaces an existing group.
//
// @Summary     Update a group
// @Description Replaces the group with the given ID.
// @Tags        groups
// @Accept      json
// @Produce     json
// @Param       groupId path     string           true "Group ID"
// @Param       group   body     model.RouteGroup true "Updated group definition"
// @Success     200     {object} model.RouteGroup
// @Failure     400     {object} respond.ErrorBody
// @Failure     404     {object} respond.ErrorBody
// @Failure     500     {object} respond.ErrorBody
// @Router      /groups/{groupId} [put]
func (d *Dependencies) HandleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupId")

	if _, err := d.Store.GetGroup(r.Context(), groupID); err != nil {
		storeError(w, err, "group", d.Logger)
		return
	}

	var group model.RouteGroup
	if err := json.NewDecoder(r.Body).Decode(&group); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error(), d.Logger)
		return
	}
	group.ID = groupID

	if err := validateGroup(group); err != nil {
		respond.Error(w, http.StatusBadRequest, err.Error(), d.Logger)
		return
	}

	if err := d.Store.SaveGroup(r.Context(), group); err != nil {
		storeError(w, err, "group", d.Logger)
		return
	}

	respond.JSON(w, http.StatusOK, group, d.Logger)
}

// DeleteGroup removes the group identified by groupId.
//
// @Summary     Delete a group
// @Description Deletes the group with the given ID.
// @Tags        groups
// @Produce     json
// @Param       groupId path     string true "Group ID"
// @Success     204     "No Content"
// @Failure     404     {object} respond.ErrorBody
// @Failure     500     {object} respond.ErrorBody
// @Router      /groups/{groupId} [delete]
func (d *Dependencies) HandleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupId")

	if err := d.Store.DeleteGroup(r.Context(), groupID); err != nil {
		storeError(w, err, "group", d.Logger)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// validateGroup checks that the group configuration is valid.
func validateGroup(g model.RouteGroup) error {
	if g.Name == "" {
		return errors.New("name is required")
	}
	return nil
}
