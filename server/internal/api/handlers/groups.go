package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/achetronic/rutoso/server/internal/api/respond"
	"github.com/achetronic/rutoso/server/internal/model"
)

// GroupsHandler handles CRUD operations for route groups.
type GroupsHandler struct {
	deps Dependencies
}

// NewGroupsHandler creates a GroupsHandler with the given dependencies.
func NewGroupsHandler(deps Dependencies) *GroupsHandler {
	return &GroupsHandler{deps: deps}
}

// HandleListGroups handles GET /api/v1/route-groups.
//
//	@Summary		List route groups
//	@Tags			route-groups
//	@Produce		json
//	@Success		200	{array}		model.RouteGroup
//	@Failure		500	{object}	respond.errorBody
//	@Router			/api/v1/route-groups [get]
func (h *GroupsHandler) HandleListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := h.deps.Store.ListGroups(r.Context())
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error(), h.deps.Logger)
		return
	}
	respond.JSON(w, http.StatusOK, groups, h.deps.Logger)
}

// HandleGetGroup handles GET /api/v1/route-groups/{groupId}.
//
//	@Summary		Get a route group
//	@Tags			route-groups
//	@Produce		json
//	@Param			groupId	path		string	true	"Group ID"
//	@Success		200		{object}	model.RouteGroup
//	@Failure		404		{object}	respond.errorBody
//	@Failure		500		{object}	respond.errorBody
//	@Router			/api/v1/route-groups/{groupId} [get]
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

// HandleCreateGroup handles POST /api/v1/route-groups.
//
//	@Summary		Create a route group
//	@Tags			route-groups
//	@Accept			json
//	@Produce		json
//	@Param			group	body		model.RouteGroup	true	"Route group to create"
//	@Success		201		{object}	model.RouteGroup
//	@Failure		400		{object}	respond.errorBody
//	@Failure		409		{object}	respond.errorBody
//	@Failure		500		{object}	respond.errorBody
//	@Router			/api/v1/route-groups [post]
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

// HandleUpdateGroup handles PUT /api/v1/route-groups/{groupId}.
//
//	@Summary		Update a route group
//	@Tags			route-groups
//	@Accept			json
//	@Produce		json
//	@Param			groupId	path		string				true	"Group ID"
//	@Param			group	body		model.RouteGroup	true	"Updated route group"
//	@Success		200		{object}	model.RouteGroup
//	@Failure		400		{object}	respond.errorBody
//	@Failure		404		{object}	respond.errorBody
//	@Failure		500		{object}	respond.errorBody
//	@Router			/api/v1/route-groups/{groupId} [put]
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

// HandleDeleteGroup handles DELETE /api/v1/route-groups/{groupId}.
//
//	@Summary		Delete a route group
//	@Tags			route-groups
//	@Param			groupId	path	string	true	"Group ID"
//	@Success		204
//	@Failure		404	{object}	respond.errorBody
//	@Failure		500	{object}	respond.errorBody
//	@Router			/api/v1/route-groups/{groupId} [delete]
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
