// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package handlers wires HTTP request/response logic for the middlewares resource.
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/achetronic/vrata/internal/api/respond"
	"github.com/achetronic/vrata/internal/model"
	"github.com/google/uuid"
)

// ListMiddlewares returns all middlewares stored in the database.
//
// @Summary     List middlewares
// @Description Returns the full list of middlewares.
// @Tags        middlewares
// @Produce     json
// @Success     200 {array}   model.Middleware
// @Failure     500 {object}  respond.ErrorBody
// @Router      /middlewares [get]
func (d *Dependencies) ListMiddlewares(w http.ResponseWriter, r *http.Request) {
	middlewares, err := d.Store.ListMiddlewares(r.Context())
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error(), d.Logger)
		return
	}
	respond.JSON(w, http.StatusOK, middlewares, d.Logger)
}

// CreateMiddleware creates a new middleware and persists it in the database.
//
// @Summary     Create a middleware
// @Description Creates a new middleware.
// @Tags        middlewares
// @Accept      json
// @Produce     json
// @Param       middleware body model.Middleware true "Middleware definition"
// @Success     201    {object}  model.Middleware
// @Failure     400    {object}  respond.ErrorBody
// @Failure     500    {object}  respond.ErrorBody
// @Router      /middlewares [post]
func (d *Dependencies) CreateMiddleware(w http.ResponseWriter, r *http.Request) {
	var mw model.Middleware
	if err := json.NewDecoder(r.Body).Decode(&mw); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error(), d.Logger)
		return
	}

	if mw.ID == "" {
		mw.ID = uuid.NewString()
	}

	if err := d.Store.SaveMiddleware(r.Context(), mw); err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error(), d.Logger)
		return
	}

	respond.JSON(w, http.StatusCreated, mw, d.Logger)
}

// GetMiddleware returns the middleware identified by middlewareId.
//
// @Summary     Get a middleware
// @Description Returns the middleware with the given ID.
// @Tags        middlewares
// @Produce     json
// @Param       middlewareId path     string true "Filter ID"
// @Success     200      {object} model.Middleware
// @Failure     404      {object} respond.ErrorBody
// @Failure     500      {object} respond.ErrorBody
// @Router      /middlewares/{middlewareId} [get]
func (d *Dependencies) GetMiddleware(w http.ResponseWriter, r *http.Request) {
	middlewareID := r.PathValue("middlewareId")

	mw, err := d.Store.GetMiddleware(r.Context(), middlewareID)
	if err != nil {
		respond.Error(w, http.StatusNotFound, err.Error(), d.Logger)
		return
	}
	respond.JSON(w, http.StatusOK, mw, d.Logger)
}

// UpdateMiddleware replaces an existing middleware.
//
// @Summary     Update a middleware
// @Description Replaces the middleware with the given ID.
// @Tags        middlewares
// @Accept      json
// @Produce     json
// @Param       middlewareId path     string       true "Filter ID"
// @Param       middleware body model.Middleware true "Updated middleware definition"
// @Success     200      {object} model.Middleware
// @Failure     400      {object} respond.ErrorBody
// @Failure     404      {object} respond.ErrorBody
// @Failure     500      {object} respond.ErrorBody
// @Router      /middlewares/{middlewareId} [put]
func (d *Dependencies) UpdateMiddleware(w http.ResponseWriter, r *http.Request) {
	middlewareID := r.PathValue("middlewareId")

	if _, err := d.Store.GetMiddleware(r.Context(), middlewareID); err != nil {
		respond.Error(w, http.StatusNotFound, err.Error(), d.Logger)
		return
	}

	var mw model.Middleware
	if err := json.NewDecoder(r.Body).Decode(&mw); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error(), d.Logger)
		return
	}
	mw.ID = middlewareID

	if err := d.Store.SaveMiddleware(r.Context(), mw); err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error(), d.Logger)
		return
	}

	respond.JSON(w, http.StatusOK, mw, d.Logger)
}

// DeleteMiddleware removes the middleware identified by middlewareId.
//
// @Summary     Delete a middleware
// @Description Deletes the middleware with the given ID.
// @Tags        middlewares
// @Produce     json
// @Param       middlewareId path     string true "Filter ID"
// @Success     204      "No Content"
// @Failure     404      {object} respond.ErrorBody
// @Failure     500      {object} respond.ErrorBody
// @Router      /middlewares/{middlewareId} [delete]
func (d *Dependencies) DeleteMiddleware(w http.ResponseWriter, r *http.Request) {
	middlewareID := r.PathValue("middlewareId")

	if err := d.Store.DeleteMiddleware(r.Context(), middlewareID); err != nil {
		respond.Error(w, http.StatusNotFound, err.Error(), d.Logger)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
