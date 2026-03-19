// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package handlers wires HTTP request/response logic for the routes resource.
// Routes are independent first-class entities — they are not nested under groups.
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/achetronic/vrata/internal/api/respond"
	"github.com/achetronic/vrata/internal/model"
	"github.com/google/uuid"
)

// ListRoutes returns all routes stored in the database.
//
// @Summary     List routes
// @Description Returns the full list of routes.
// @Tags        routes
// @Produce     json
// @Success     200 {array}   model.Route
// @Failure     500 {object}  respond.ErrorBody
// @Router      /routes [get]
func (d *Dependencies) ListRoutes(w http.ResponseWriter, r *http.Request) {
	routes, err := d.Store.ListRoutes(r.Context())
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error(), d.Logger)
		return
	}
	respond.JSON(w, http.StatusOK, routes, d.Logger)
}

// CreateRoute creates a new route and persists it in the database.
//
// @Summary     Create a route
// @Description Creates a new independent route.
// @Tags        routes
// @Accept      json
// @Produce     json
// @Param       route body      model.Route true "Route definition"
// @Success     201   {object}  model.Route
// @Failure     400   {object}  respond.ErrorBody
// @Failure     500   {object}  respond.ErrorBody
// @Router      /routes [post]
func (d *Dependencies) CreateRoute(w http.ResponseWriter, r *http.Request) {
	var route model.Route
	if err := json.NewDecoder(r.Body).Decode(&route); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error(), d.Logger)
		return
	}

	if err := validateRouteAction(route); err != nil {
		respond.Error(w, http.StatusBadRequest, err.Error(), d.Logger)
		return
	}

	if route.ID == "" {
		route.ID = uuid.NewString()
	}

	if err := d.Store.SaveRoute(r.Context(), route); err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error(), d.Logger)
		return
	}

	respond.JSON(w, http.StatusCreated, route, d.Logger)
}

// GetRoute returns the route identified by routeId.
//
// @Summary     Get a route
// @Description Returns the route with the given ID.
// @Tags        routes
// @Produce     json
// @Param       routeId path     string true "Route ID"
// @Success     200     {object} model.Route
// @Failure     404     {object} respond.ErrorBody
// @Failure     500     {object} respond.ErrorBody
// @Router      /routes/{routeId} [get]
func (d *Dependencies) GetRoute(w http.ResponseWriter, r *http.Request) {
	routeID := r.PathValue("routeId")

	route, err := d.Store.GetRoute(r.Context(), routeID)
	if err != nil {
		respond.Error(w, http.StatusNotFound, err.Error(), d.Logger)
		return
	}
	respond.JSON(w, http.StatusOK, route, d.Logger)
}

// UpdateRoute replaces an existing route.
//
// @Summary     Update a route
// @Description Replaces the route with the given ID.
// @Tags        routes
// @Accept      json
// @Produce     json
// @Param       routeId path     string      true "Route ID"
// @Param       route   body     model.Route true "Updated route definition"
// @Success     200     {object} model.Route
// @Failure     400     {object} respond.ErrorBody
// @Failure     404     {object} respond.ErrorBody
// @Failure     500     {object} respond.ErrorBody
// @Router      /routes/{routeId} [put]
func (d *Dependencies) UpdateRoute(w http.ResponseWriter, r *http.Request) {
	routeID := r.PathValue("routeId")

	if _, err := d.Store.GetRoute(r.Context(), routeID); err != nil {
		respond.Error(w, http.StatusNotFound, err.Error(), d.Logger)
		return
	}

	var route model.Route
	if err := json.NewDecoder(r.Body).Decode(&route); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error(), d.Logger)
		return
	}
	route.ID = routeID

	if err := validateRouteAction(route); err != nil {
		respond.Error(w, http.StatusBadRequest, err.Error(), d.Logger)
		return
	}

	if err := d.Store.SaveRoute(r.Context(), route); err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error(), d.Logger)
		return
	}

	respond.JSON(w, http.StatusOK, route, d.Logger)
}

// validateRouteAction checks that the route defines exactly one action mode:
// forward, redirect, or directResponse. Returns model.ErrConflictingAction
// when more than one is set, or when none is set.
func validateRouteAction(route model.Route) error {
	set := 0
	if route.Forward != nil {
		set++
	}
	if route.Redirect != nil {
		set++
	}
	if route.DirectResponse != nil {
		set++
	}
	if set != 1 {
		return model.ErrConflictingAction
	}
	return nil
}

// DeleteRoute removes the route identified by routeId.
//
// @Summary     Delete a route
// @Description Deletes the route with the given ID.
// @Tags        routes
// @Produce     json
// @Param       routeId path     string true "Route ID"
// @Success     204     "No Content"
// @Failure     404     {object} respond.ErrorBody
// @Failure     500     {object} respond.ErrorBody
// @Router      /routes/{routeId} [delete]
func (d *Dependencies) DeleteRoute(w http.ResponseWriter, r *http.Request) {
	routeID := r.PathValue("routeId")

	if err := d.Store.DeleteRoute(r.Context(), routeID); err != nil {
		respond.Error(w, http.StatusNotFound, err.Error(), d.Logger)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
