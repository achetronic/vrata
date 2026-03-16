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

// HandleListRoutes handles GET /api/v1/route-groups/{groupId}/routes.
//
//	@Summary		List routes in a group
//	@Tags			routes
//	@Produce		json
//	@Param			groupId	path		string	true	"Group ID"
//	@Success		200		{array}		model.Route
//	@Failure		404		{object}	respond.errorBody
//	@Failure		500		{object}	respond.errorBody
//	@Router			/api/v1/route-groups/{groupId}/routes [get]
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

// HandleGetRoute handles GET /api/v1/route-groups/{groupId}/routes/{routeId}.
//
//	@Summary		Get a route
//	@Tags			routes
//	@Produce		json
//	@Param			groupId		path		string	true	"Group ID"
//	@Param			routeId		path		string	true	"Route ID"
//	@Success		200			{object}	model.Route
//	@Failure		404			{object}	respond.errorBody
//	@Failure		500			{object}	respond.errorBody
//	@Router			/api/v1/route-groups/{groupId}/routes/{routeId} [get]
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

// HandleCreateRoute handles POST /api/v1/route-groups/{groupId}/routes.
//
//	@Summary		Create a route
//	@Tags			routes
//	@Accept			json
//	@Produce		json
//	@Param			groupId	path		string		true	"Group ID"
//	@Param			route	body		model.Route	true	"Route to create"
//	@Success		201		{object}	model.Route
//	@Failure		400		{object}	respond.errorBody
//	@Failure		404		{object}	respond.errorBody
//	@Failure		409		{object}	respond.errorBody
//	@Failure		500		{object}	respond.errorBody
//	@Router			/api/v1/route-groups/{groupId}/routes [post]
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

// HandleUpdateRoute handles PUT /api/v1/route-groups/{groupId}/routes/{routeId}.
//
//	@Summary		Update a route
//	@Tags			routes
//	@Accept			json
//	@Produce		json
//	@Param			groupId		path		string		true	"Group ID"
//	@Param			routeId		path		string		true	"Route ID"
//	@Param			route		body		model.Route	true	"Updated route"
//	@Success		200			{object}	model.Route
//	@Failure		400			{object}	respond.errorBody
//	@Failure		404			{object}	respond.errorBody
//	@Failure		409			{object}	respond.errorBody
//	@Failure		500			{object}	respond.errorBody
//	@Router			/api/v1/route-groups/{groupId}/routes/{routeId} [put]
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

// HandleDeleteRoute handles DELETE /api/v1/route-groups/{groupId}/routes/{routeId}.
//
//	@Summary		Delete a route
//	@Tags			routes
//	@Param			groupId	path	string	true	"Group ID"
//	@Param			routeId	path	string	true	"Route ID"
//	@Success		204
//	@Failure		404	{object}	respond.errorBody
//	@Failure		500	{object}	respond.errorBody
//	@Router			/api/v1/route-groups/{groupId}/routes/{routeId} [delete]
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
