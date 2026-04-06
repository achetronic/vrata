// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package handlers wires HTTP request/response logic for the listeners resource.
package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/achetronic/vrata/internal/api/respond"
	"github.com/achetronic/vrata/internal/model"

	"github.com/google/uuid"
)

// ListListeners returns all listeners stored in the database.
//
// @Summary     List listeners
// @Description Returns the full list of listeners.
// @Tags        listeners
// @Produce     json
// @Success     200 {array}   model.Listener
// @Failure     500 {object}  respond.ErrorBody
// @Router      /listeners [get]
func (d *Dependencies) HandleListListeners(w http.ResponseWriter, r *http.Request) {
	listeners, err := d.Store.ListListeners(r.Context())
	if err != nil {
		storeError(w, err, "listeners", d.Logger)
		return
	}
	respond.JSON(w, http.StatusOK, listeners, d.Logger)
}

// CreateListener creates a new listener and persists it in the database.
//
// @Summary     Create a listener
// @Description Creates a new listener.
// @Tags        listeners
// @Accept      json
// @Produce     json
// @Param       listener body      model.Listener true "Listener definition"
// @Success     201      {object}  model.Listener
// @Failure     400      {object}  respond.ErrorBody
// @Failure     500      {object}  respond.ErrorBody
// @Router      /listeners [post]
func (d *Dependencies) HandleCreateListener(w http.ResponseWriter, r *http.Request) {
	var listener model.Listener
	if err := json.NewDecoder(r.Body).Decode(&listener); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error(), d.Logger)
		return
	}

	if err := validateListener(listener); err != nil {
		respond.Error(w, http.StatusBadRequest, err.Error(), d.Logger)
		return
	}

	if listener.ID == "" {
		listener.ID = uuid.NewString()
	}

	if listener.Address == "" {
		listener.Address = "0.0.0.0"
	}

	if err := d.Store.SaveListener(r.Context(), listener); err != nil {
		storeError(w, err, "listener", d.Logger)
		return
	}

	respond.JSON(w, http.StatusCreated, listener, d.Logger)
}

// GetListener returns the listener identified by listenerId.
//
// @Summary     Get a listener
// @Description Returns the listener with the given ID.
// @Tags        listeners
// @Produce     json
// @Param       listenerId path     string true "Listener ID"
// @Success     200        {object} model.Listener
// @Failure     404        {object} respond.ErrorBody
// @Failure     500        {object} respond.ErrorBody
// @Router      /listeners/{listenerId} [get]
func (d *Dependencies) HandleGetListener(w http.ResponseWriter, r *http.Request) {
	listenerID := r.PathValue("listenerId")

	listener, err := d.Store.GetListener(r.Context(), listenerID)
	if err != nil {
		storeError(w, err, "listener", d.Logger)
		return
	}
	respond.JSON(w, http.StatusOK, listener, d.Logger)
}

// UpdateListener replaces an existing listener.
//
// @Summary     Update a listener
// @Description Replaces the listener with the given ID.
// @Tags        listeners
// @Accept      json
// @Produce     json
// @Param       listenerId path     string         true "Listener ID"
// @Param       listener   body     model.Listener true "Updated listener definition"
// @Success     200        {object} model.Listener
// @Failure     400        {object} respond.ErrorBody
// @Failure     404        {object} respond.ErrorBody
// @Failure     500        {object} respond.ErrorBody
// @Router      /listeners/{listenerId} [put]
func (d *Dependencies) HandleUpdateListener(w http.ResponseWriter, r *http.Request) {
	listenerID := r.PathValue("listenerId")

	if _, err := d.Store.GetListener(r.Context(), listenerID); err != nil {
		storeError(w, err, "listener", d.Logger)
		return
	}

	var listener model.Listener
	if err := json.NewDecoder(r.Body).Decode(&listener); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error(), d.Logger)
		return
	}
	listener.ID = listenerID

	if err := validateListener(listener); err != nil {
		respond.Error(w, http.StatusBadRequest, err.Error(), d.Logger)
		return
	}

	if listener.Address == "" {
		listener.Address = "0.0.0.0"
	}

	if err := d.Store.SaveListener(r.Context(), listener); err != nil {
		storeError(w, err, "listener", d.Logger)
		return
	}

	respond.JSON(w, http.StatusOK, listener, d.Logger)
}

// DeleteListener removes the listener identified by listenerId.
//
// @Summary     Delete a listener
// @Description Deletes the listener with the given ID.
// @Tags        listeners
// @Produce     json
// @Param       listenerId path     string true "Listener ID"
// @Success     204        "No Content"
// @Failure     404        {object} respond.ErrorBody
// @Failure     500        {object} respond.ErrorBody
// @Router      /listeners/{listenerId} [delete]
func (d *Dependencies) HandleDeleteListener(w http.ResponseWriter, r *http.Request) {
	listenerID := r.PathValue("listenerId")

	if err := d.Store.DeleteListener(r.Context(), listenerID); err != nil {
		storeError(w, err, "listener", d.Logger)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// validateListener checks that the listener configuration is valid.
func validateListener(l model.Listener) error {
	if l.Name == "" {
		return fmt.Errorf("name is required")
	}
	if l.Port == 0 {
		return fmt.Errorf("port is required and must be greater than 0")
	}

	if l.TLS != nil && l.TLS.ClientAuth != nil {
		ca := l.TLS.ClientAuth
		switch ca.Mode {
		case "", "none":
		case "optional", "require":
			if ca.CA == "" {
				return fmt.Errorf("clientAuth.ca is required when mode is %q", ca.Mode)
			}
		default:
			return fmt.Errorf("unknown clientAuth.mode %q: must be \"none\", \"optional\", or \"require\"", ca.Mode)
		}
	}
	return nil
}
