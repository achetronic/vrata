// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"log/slog"
	"net/http"

	"github.com/achetronic/vrata/internal/api/respond"
)

// HandleGetConfigDump returns the complete current configuration: all listeners,
// destinations, routes, groups, and middlewares in a single JSON response.
// If any entity type fails to load, the dump includes partial results
// with the errors reported in an "errors" field.
//
// @Summary     Get config dump
// @Description Returns every stored entity in a single response for debugging.
// @Tags        debug
// @Produce     json
// @Success     200 {object} map[string]interface{}
// @Failure     500 {object} respond.ErrorBody
// @Router      /debug/config [get]
func (d *Dependencies) HandleGetConfigDump(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dump := map[string]interface{}{}
	var errs []string

	listeners, err := d.Store.ListListeners(ctx)
	if err != nil {
		d.Logger.Error("config dump: listing listeners", slog.String("error", err.Error()))
		errs = append(errs, "listeners: "+err.Error())
	} else {
		dump["listeners"] = listeners
	}

	destinations, err := d.Store.ListDestinations(ctx)
	if err != nil {
		d.Logger.Error("config dump: listing destinations", slog.String("error", err.Error()))
		errs = append(errs, "destinations: "+err.Error())
	} else {
		dump["destinations"] = destinations
	}

	routes, err := d.Store.ListRoutes(ctx)
	if err != nil {
		d.Logger.Error("config dump: listing routes", slog.String("error", err.Error()))
		errs = append(errs, "routes: "+err.Error())
	} else {
		dump["routes"] = routes
	}

	groups, err := d.Store.ListGroups(ctx)
	if err != nil {
		d.Logger.Error("config dump: listing groups", slog.String("error", err.Error()))
		errs = append(errs, "groups: "+err.Error())
	} else {
		dump["groups"] = groups
	}

	middlewares, err := d.Store.ListMiddlewares(ctx)
	if err != nil {
		d.Logger.Error("config dump: listing middlewares", slog.String("error", err.Error()))
		errs = append(errs, "middlewares: "+err.Error())
	} else {
		dump["middlewares"] = middlewares
	}

	if len(errs) > 0 {
		dump["errors"] = errs
	}

	respond.JSON(w, http.StatusOK, dump, d.Logger)
}
