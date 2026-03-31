// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"net/http"

	"github.com/achetronic/vrata/internal/api/respond"
)

// GetConfigDump returns the complete current configuration: all listeners,
// destinations, routes, groups, and middlewares in a single JSON response.
//
// @Summary     Get config dump
// @Description Returns every stored entity in a single response for debugging.
// @Tags        debug
// @Produce     json
// @Success     200 {object} map[string]interface{}
// @Failure     500 {object} respond.ErrorBody
// @Router      /debug/config [get]
func (d *Dependencies) GetConfigDump(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	listeners, err := d.Store.ListListeners(ctx)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error(), d.Logger)
		return
	}

	destinations, err := d.Store.ListDestinations(ctx)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error(), d.Logger)
		return
	}

	routes, err := d.Store.ListRoutes(ctx)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error(), d.Logger)
		return
	}

	groups, err := d.Store.ListGroups(ctx)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error(), d.Logger)
		return
	}

	middlewares, err := d.Store.ListMiddlewares(ctx)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error(), d.Logger)
		return
	}

	dump := map[string]interface{}{
		"listeners":    listeners,
		"destinations": destinations,
		"routes":       routes,
		"groups":       groups,
		"middlewares":  middlewares,
	}

	respond.JSON(w, http.StatusOK, dump, d.Logger)
}
