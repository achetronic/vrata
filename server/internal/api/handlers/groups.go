package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/achetronic/rutoso/internal/api/respond"
	"github.com/achetronic/rutoso/internal/model"
)

// GroupsHandler handles CRUD operations for route groups.
type GroupsHandler struct {
	deps Dependencies
}

// NewGroupsHandler creates a GroupsHandler with the given dependencies.
func NewGroupsHandler(deps Dependencies) *GroupsHandler {
	return &GroupsHandler{deps: deps}
}

// HandleListGroups handles GET /api/v1/groups.
//
//	@Summary		List all route groups
//	@Description	Returns the full list of route groups known to this control plane.
//	@Tags			groups
//	@Produce		json
//	@Success		200	{array}		model.RouteGroup	"List of route groups (may be empty)"
//	@Failure		500	{object}	respond.ErrorBody	"Internal error"
//	@Router			/api/v1/groups [get]
func (h *GroupsHandler) HandleListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := h.deps.Store.ListGroups(r.Context())
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error(), h.deps.Logger)
		return
	}
	respond.JSON(w, http.StatusOK, groups, h.deps.Logger)
}

// HandleGetGroup handles GET /api/v1/groups/{groupId}.
//
//	@Summary		Get a route group
//	@Description	Returns a single route group by its ID.
//	@Tags			groups
//	@Produce		json
//	@Param			groupId	path		string				true	"Group UUID"	example(550e8400-e29b-41d4-a716-446655440000)
//	@Success		200		{object}	model.RouteGroup	"Route group found"
//	@Failure		404		{object}	respond.ErrorBody	"Group not found"
//	@Failure		500		{object}	respond.ErrorBody	"Internal error"
//	@Router			/api/v1/groups/{groupId} [get]
func (h *GroupsHandler) HandleGetGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("groupId")
	g, err := h.deps.Store.GetGroup(r.Context(), id)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			respond.Error(w, http.StatusNotFound, err.Error(), h.deps.Logger)
			return
		}
		respond.Error(w, http.StatusInternalServerError, err.Error(), h.deps.Logger)
		return
	}
	respond.JSON(w, http.StatusOK, g, h.deps.Logger)
}

// HandleCreateGroup handles POST /api/v1/groups.
//
//	@Summary		Create a route group
//	@Description	Creates a new route group. The name must be unique across all groups.
//	@Description	The ID is auto-generated and returned in the response.
//	@Tags			groups
//	@Accept			json
//	@Produce		json
//	@Param			group	body		model.RouteGroup	true	"Route group to create"
//	@Success		201		{object}	model.RouteGroup	"Route group created"
//	@Failure		400		{object}	respond.ErrorBody	"Invalid payload or missing required fields"
//	@Failure		409		{object}	respond.ErrorBody	"A group with the same name already exists"
//	@Failure		500		{object}	respond.ErrorBody	"Internal error"
//	@Router			/api/v1/groups [post]
func (h *GroupsHandler) HandleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var g model.RouteGroup
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error(), h.deps.Logger)
		return
	}

	if g.Name == "" {
		respond.Error(w, http.StatusBadRequest, "name is required", h.deps.Logger)
		return
	}

	g.ID = uuid.NewString()

	if err := h.deps.Store.CreateGroup(r.Context(), g); err != nil {
		if errors.Is(err, model.ErrDuplicateGroup) {
			respond.Error(w, http.StatusConflict, err.Error(), h.deps.Logger)
			return
		}
		respond.Error(w, http.StatusInternalServerError, err.Error(), h.deps.Logger)
		return
	}
	respond.JSON(w, http.StatusCreated, g, h.deps.Logger)
}

// HandleUpdateGroup handles PUT /api/v1/groups/{groupId}.
//
//	@Summary		Update a route group
//	@Description	Replaces a route group's metadata (prefix, hostnames, headers, description).
//	@Description	The group's routes are not affected by this operation.
//	@Tags			groups
//	@Accept			json
//	@Produce		json
//	@Param			groupId	path		string				true	"Group UUID"	example(550e8400-e29b-41d4-a716-446655440000)
//	@Param			group	body		model.RouteGroup	true	"Updated route group"
//	@Success		200		{object}	model.RouteGroup	"Route group updated"
//	@Failure		400		{object}	respond.ErrorBody	"Invalid payload"
//	@Failure		404		{object}	respond.ErrorBody	"Group not found"
//	@Failure		500		{object}	respond.ErrorBody	"Internal error"
//	@Router			/api/v1/groups/{groupId} [put]
func (h *GroupsHandler) HandleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("groupId")

	var g model.RouteGroup
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error(), h.deps.Logger)
		return
	}
	g.ID = id

	if err := h.deps.Store.UpdateGroup(r.Context(), g); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			respond.Error(w, http.StatusNotFound, err.Error(), h.deps.Logger)
			return
		}
		respond.Error(w, http.StatusInternalServerError, err.Error(), h.deps.Logger)
		return
	}
	respond.JSON(w, http.StatusOK, g, h.deps.Logger)
}

// HandleDeleteGroup handles DELETE /api/v1/groups/{groupId}.
//
//	@Summary		Delete a route group
//	@Description	Deletes a route group and all of its routes. This action is irreversible.
//	@Description	The xDS snapshot is updated immediately; Envoy will stop routing those rules.
//	@Tags			groups
//	@Param			groupId	path	string	true	"Group UUID"	example(550e8400-e29b-41d4-a716-446655440000)
//	@Success		204		"Group deleted"
//	@Failure		404		{object}	respond.ErrorBody	"Group not found"
//	@Failure		500		{object}	respond.ErrorBody	"Internal error"
//	@Router			/api/v1/groups/{groupId} [delete]
func (h *GroupsHandler) HandleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("groupId")

	if err := h.deps.Store.DeleteGroup(r.Context(), id); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			respond.Error(w, http.StatusNotFound, err.Error(), h.deps.Logger)
			return
		}
		respond.Error(w, http.StatusInternalServerError, err.Error(), h.deps.Logger)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
