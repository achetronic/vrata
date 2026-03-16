package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/achetronic/rutoso/internal/api/respond"
	"github.com/achetronic/rutoso/internal/model"
)

// RoutesHandler handles CRUD operations for routes within a group.
type RoutesHandler struct {
	deps Dependencies
}

// NewRoutesHandler creates a RoutesHandler with the given dependencies.
func NewRoutesHandler(deps Dependencies) *RoutesHandler {
	return &RoutesHandler{deps: deps}
}

// HandleListRoutes handles GET /api/v1/groups/{groupId}/routes.
//
//	@Summary		List routes in a group
//	@Description	Returns all routes that belong to the specified route group.
//	@Tags			routes
//	@Produce		json
//	@Param			groupId	path		string			true	"Group UUID"	example(550e8400-e29b-41d4-a716-446655440000)
//	@Success		200		{array}		model.Route		"List of routes (may be empty)"
//	@Failure		404		{object}	respond.ErrorBody	"Group not found"
//	@Failure		500		{object}	respond.ErrorBody	"Internal error"
//	@Router			/api/v1/groups/{groupId}/routes [get]
func (h *RoutesHandler) HandleListRoutes(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupId")
	routes, err := h.deps.Store.ListRoutes(r.Context(), groupID)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			respond.Error(w, http.StatusNotFound, err.Error(), h.deps.Logger)
			return
		}
		respond.Error(w, http.StatusInternalServerError, err.Error(), h.deps.Logger)
		return
	}
	respond.JSON(w, http.StatusOK, routes, h.deps.Logger)
}

// HandleGetRoute handles GET /api/v1/groups/{groupId}/routes/{routeId}.
//
//	@Summary		Get a route
//	@Description	Returns a single route by its ID within the specified group.
//	@Tags			routes
//	@Produce		json
//	@Param			groupId		path		string			true	"Group UUID"	example(550e8400-e29b-41d4-a716-446655440000)
//	@Param			routeId		path		string			true	"Route UUID"	example(6ba7b810-9dad-11d1-80b4-00c04fd430c8)
//	@Success		200			{object}	model.Route		"Route found"
//	@Failure		404			{object}	respond.ErrorBody	"Group or route not found"
//	@Failure		500			{object}	respond.ErrorBody	"Internal error"
//	@Router			/api/v1/groups/{groupId}/routes/{routeId} [get]
func (h *RoutesHandler) HandleGetRoute(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupId")
	routeID := r.PathValue("routeId")

	route, err := h.deps.Store.GetRoute(r.Context(), groupID, routeID)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			respond.Error(w, http.StatusNotFound, err.Error(), h.deps.Logger)
			return
		}
		respond.Error(w, http.StatusInternalServerError, err.Error(), h.deps.Logger)
		return
	}
	respond.JSON(w, http.StatusOK, route, h.deps.Logger)
}

// HandleCreateRoute handles POST /api/v1/groups/{groupId}/routes.
//
//	@Summary		Create a route
//	@Description	Adds a new route to the specified group.
//	@Description	At least one backend is required. If multiple backends are provided,
//	@Description	their weights must sum to exactly 100 (weighted routing / canary).
//	@Description	Two routes with the same path rule (path/pathPrefix/pathRegex) in the
//	@Description	same group are considered duplicates and will be rejected with 409.
//	@Tags			routes
//	@Accept			json
//	@Produce		json
//	@Param			groupId	path		string			true	"Group UUID"	example(550e8400-e29b-41d4-a716-446655440000)
//	@Param			route	body		model.Route		true	"Route to create"
//	@Success		201		{object}	model.Route		"Route created"
//	@Failure		400		{object}	respond.ErrorBody	"Invalid payload, missing backend, or invalid weights"
//	@Failure		404		{object}	respond.ErrorBody	"Group not found"
//	@Failure		409		{object}	respond.ErrorBody	"A route with the same match rule already exists"
//	@Failure		500		{object}	respond.ErrorBody	"Internal error"
//	@Router			/api/v1/groups/{groupId}/routes [post]
func (h *RoutesHandler) HandleCreateRoute(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupId")

	var route model.Route
	if err := json.NewDecoder(r.Body).Decode(&route); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error(), h.deps.Logger)
		return
	}

	if len(route.Backends) == 0 {
		respond.Error(w, http.StatusBadRequest, "at least one backend is required", h.deps.Logger)
		return
	}
	if len(route.Backends) > 1 {
		if err := validateWeights(route.Backends); err != nil {
			respond.Error(w, http.StatusBadRequest, err.Error(), h.deps.Logger)
			return
		}
	}

	route.ID = uuid.NewString()
	route.GroupID = groupID

	if err := h.deps.Store.CreateRoute(r.Context(), route); err != nil {
		switch {
		case errors.Is(err, model.ErrNotFound):
			respond.Error(w, http.StatusNotFound, err.Error(), h.deps.Logger)
		case errors.Is(err, model.ErrDuplicateRoute):
			respond.Error(w, http.StatusConflict, err.Error(), h.deps.Logger)
		default:
			respond.Error(w, http.StatusInternalServerError, err.Error(), h.deps.Logger)
		}
		return
	}
	respond.JSON(w, http.StatusCreated, route, h.deps.Logger)
}

// HandleUpdateRoute handles PUT /api/v1/groups/{groupId}/routes/{routeId}.
//
//	@Summary		Update a route
//	@Description	Replaces an existing route entirely.
//	@Description	If multiple backends are provided, their weights must sum to 100.
//	@Description	Changing the match rule to one already used by another route in the same
//	@Description	group will be rejected with 409.
//	@Tags			routes
//	@Accept			json
//	@Produce		json
//	@Param			groupId		path		string			true	"Group UUID"	example(550e8400-e29b-41d4-a716-446655440000)
//	@Param			routeId		path		string			true	"Route UUID"	example(6ba7b810-9dad-11d1-80b4-00c04fd430c8)
//	@Param			route		body		model.Route		true	"Updated route"
//	@Success		200			{object}	model.Route		"Route updated"
//	@Failure		400			{object}	respond.ErrorBody	"Invalid payload or invalid weights"
//	@Failure		404			{object}	respond.ErrorBody	"Group or route not found"
//	@Failure		409			{object}	respond.ErrorBody	"Match rule conflicts with another route"
//	@Failure		500			{object}	respond.ErrorBody	"Internal error"
//	@Router			/api/v1/groups/{groupId}/routes/{routeId} [put]
func (h *RoutesHandler) HandleUpdateRoute(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupId")
	routeID := r.PathValue("routeId")

	var route model.Route
	if err := json.NewDecoder(r.Body).Decode(&route); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error(), h.deps.Logger)
		return
	}

	if len(route.Backends) > 1 {
		if err := validateWeights(route.Backends); err != nil {
			respond.Error(w, http.StatusBadRequest, err.Error(), h.deps.Logger)
			return
		}
	}

	route.ID = routeID
	route.GroupID = groupID

	if err := h.deps.Store.UpdateRoute(r.Context(), route); err != nil {
		switch {
		case errors.Is(err, model.ErrNotFound):
			respond.Error(w, http.StatusNotFound, err.Error(), h.deps.Logger)
		case errors.Is(err, model.ErrDuplicateRoute):
			respond.Error(w, http.StatusConflict, err.Error(), h.deps.Logger)
		default:
			respond.Error(w, http.StatusInternalServerError, err.Error(), h.deps.Logger)
		}
		return
	}
	respond.JSON(w, http.StatusOK, route, h.deps.Logger)
}

// HandleDeleteRoute handles DELETE /api/v1/groups/{groupId}/routes/{routeId}.
//
//	@Summary		Delete a route
//	@Description	Removes a route from its group. The xDS snapshot is updated immediately.
//	@Tags			routes
//	@Param			groupId		path	string	true	"Group UUID"	example(550e8400-e29b-41d4-a716-446655440000)
//	@Param			routeId		path	string	true	"Route UUID"	example(6ba7b810-9dad-11d1-80b4-00c04fd430c8)
//	@Success		204		"Route deleted"
//	@Failure		404		{object}	respond.ErrorBody	"Group or route not found"
//	@Failure		500		{object}	respond.ErrorBody	"Internal error"
//	@Router			/api/v1/groups/{groupId}/routes/{routeId} [delete]
func (h *RoutesHandler) HandleDeleteRoute(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupId")
	routeID := r.PathValue("routeId")

	if err := h.deps.Store.DeleteRoute(r.Context(), groupID, routeID); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			respond.Error(w, http.StatusNotFound, err.Error(), h.deps.Logger)
			return
		}
		respond.Error(w, http.StatusInternalServerError, err.Error(), h.deps.Logger)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// validateWeights returns model.ErrInvalidWeight if the backend weights don't sum to 100.
func validateWeights(backends []model.Backend) error {
	var total uint32
	for _, b := range backends {
		total += b.Weight
	}
	if total != 100 {
		return model.ErrInvalidWeight
	}
	return nil
}
