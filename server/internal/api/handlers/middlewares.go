// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package handlers wires HTTP request/response logic for the middlewares resource.
package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/achetronic/vrata/internal/api/respond"
	"github.com/achetronic/vrata/internal/model"
	"github.com/achetronic/vrata/internal/proxy/celeval"

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
func (d *Dependencies) HandleListMiddlewares(w http.ResponseWriter, r *http.Request) {
	middlewares, err := d.Store.ListMiddlewares(r.Context())
	if err != nil {
		storeError(w, err, "middlewares", d.Logger)
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
func (d *Dependencies) HandleCreateMiddleware(w http.ResponseWriter, r *http.Request) {
	var mw model.Middleware
	if err := json.NewDecoder(r.Body).Decode(&mw); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error(), d.Logger)
		return
	}

	if err := validateMiddleware(mw); err != nil {
		respond.Error(w, http.StatusBadRequest, err.Error(), d.Logger)
		return
	}

	if mw.ID == "" {
		mw.ID = uuid.NewString()
	}

	if err := d.Store.SaveMiddleware(r.Context(), mw); err != nil {
		storeError(w, err, "middleware", d.Logger)
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
func (d *Dependencies) HandleGetMiddleware(w http.ResponseWriter, r *http.Request) {
	middlewareID := r.PathValue("middlewareId")

	mw, err := d.Store.GetMiddleware(r.Context(), middlewareID)
	if err != nil {
		storeError(w, err, "middleware", d.Logger)
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
func (d *Dependencies) HandleUpdateMiddleware(w http.ResponseWriter, r *http.Request) {
	middlewareID := r.PathValue("middlewareId")

	if _, err := d.Store.GetMiddleware(r.Context(), middlewareID); err != nil {
		storeError(w, err, "middleware", d.Logger)
		return
	}

	var mw model.Middleware
	if err := json.NewDecoder(r.Body).Decode(&mw); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error(), d.Logger)
		return
	}
	mw.ID = middlewareID

	if err := validateMiddleware(mw); err != nil {
		respond.Error(w, http.StatusBadRequest, err.Error(), d.Logger)
		return
	}

	if err := d.Store.SaveMiddleware(r.Context(), mw); err != nil {
		storeError(w, err, "middleware", d.Logger)
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
func (d *Dependencies) HandleDeleteMiddleware(w http.ResponseWriter, r *http.Request) {
	middlewareID := r.PathValue("middlewareId")

	if err := d.Store.DeleteMiddleware(r.Context(), middlewareID); err != nil {
		storeError(w, err, "middleware", d.Logger)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// validateMiddleware checks that the middleware configuration is valid.
func validateMiddleware(mw model.Middleware) error {
	if mw.Name == "" {
		return fmt.Errorf("name is required")
	}

	switch mw.Type {
	case model.MiddlewareTypeInlineAuthz:
		if mw.InlineAuthz == nil {
			return fmt.Errorf("inlineAuthz config is required when type is %q", mw.Type)
		}
		cfg := mw.InlineAuthz
		if len(cfg.Rules) == 0 {
			return fmt.Errorf("inlineAuthz.rules must not be empty")
		}
		if cfg.DefaultAction != "" && cfg.DefaultAction != "allow" && cfg.DefaultAction != "deny" {
			return fmt.Errorf("inlineAuthz.defaultAction must be \"allow\" or \"deny\", got %q", cfg.DefaultAction)
		}
		for i, rule := range cfg.Rules {
			if rule.Action != "allow" && rule.Action != "deny" {
				return fmt.Errorf("inlineAuthz.rules[%d].action must be \"allow\" or \"deny\", got %q", i, rule.Action)
			}
			if rule.CEL == "" {
				return fmt.Errorf("inlineAuthz.rules[%d].cel must not be empty", i)
			}
			if _, err := celeval.Compile(rule.CEL); err != nil {
				return fmt.Errorf("inlineAuthz.rules[%d].cel: %w", i, err)
			}
		}

	case model.MiddlewareTypeJWT:
		if mw.JWT == nil {
			return fmt.Errorf("jwt config is required when type is %q", mw.Type)
		}
		if mw.JWT.Issuer == "" {
			return fmt.Errorf("jwt.issuer is required")
		}
		if mw.JWT.JWKsPath != "" && mw.JWT.JWKsDestinationID == "" {
			return fmt.Errorf("jwt.jwksDestinationId is required when jwksPath is set")
		}

	case model.MiddlewareTypeExtAuthz:
		if mw.ExtAuthz == nil {
			return fmt.Errorf("extAuthz config is required when type is %q", mw.Type)
		}
		if mw.ExtAuthz.DestinationID == "" {
			return fmt.Errorf("extAuthz.destinationId is required")
		}

	case model.MiddlewareTypeExtProc:
		if mw.ExtProc == nil {
			return fmt.Errorf("extProc config is required when type is %q", mw.Type)
		}
		if mw.ExtProc.DestinationID == "" {
			return fmt.Errorf("extProc.destinationId is required")
		}
	}

	return nil
}
