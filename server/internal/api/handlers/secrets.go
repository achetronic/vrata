// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/achetronic/vrata/internal/api/respond"
	"github.com/achetronic/vrata/internal/model"
	"github.com/google/uuid"
)

// HandleListSecrets returns summary metadata (ID + Name) for all secrets.
// The Value field is never included in the list response.
//
// @Summary     List secrets
// @Description Returns ID and Name for all secrets. Values are omitted.
// @Tags        secrets
// @Produce     json
// @Success     200 {array}   model.SecretSummary
// @Failure     500 {object}  respond.ErrorBody
// @Router      /secrets [get]
func (d *Dependencies) HandleListSecrets(w http.ResponseWriter, r *http.Request) {
	secrets, err := d.Store.ListSecrets(r.Context())
	if err != nil {
		d.Logger.Error("listing secrets", slog.String("error", err.Error()))
		respond.Error(w, http.StatusInternalServerError, "listing secrets", d.Logger)
		return
	}
	respond.JSON(w, http.StatusOK, secrets, d.Logger)
}

// HandleCreateSecret creates a new secret.
//
// @Summary     Create a secret
// @Description Creates a new secret entity. One secret = one value.
// @Tags        secrets
// @Accept      json
// @Produce     json
// @Param       secret body      model.Secret true "Secret definition"
// @Success     201    {object}  model.SecretSummary
// @Failure     400    {object}  respond.ErrorBody
// @Failure     500    {object}  respond.ErrorBody
// @Router      /secrets [post]
func (d *Dependencies) HandleCreateSecret(w http.ResponseWriter, r *http.Request) {
	var sec model.Secret
	if err := json.NewDecoder(r.Body).Decode(&sec); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body", d.Logger)
		return
	}

	if sec.ID == "" {
		sec.ID = uuid.NewString()
	}

	if sec.Name == "" {
		respond.Error(w, http.StatusBadRequest, "name is required", d.Logger)
		return
	}
	if sec.Value == "" {
		respond.Error(w, http.StatusBadRequest, "value is required", d.Logger)
		return
	}

	if err := d.Store.SaveSecret(r.Context(), sec); err != nil {
		d.Logger.Error("saving secret", slog.String("error", err.Error()))
		respond.Error(w, http.StatusInternalServerError, "saving secret", d.Logger)
		return
	}

	d.Logger.Info("secret created",
		slog.String("id", sec.ID),
		slog.String("name", sec.Name),
	)

	respond.JSON(w, http.StatusCreated, model.SecretSummary{ID: sec.ID, Name: sec.Name}, d.Logger)
}

// HandleGetSecret returns the secret with the given ID, including its Value.
//
// @Summary     Get a secret
// @Description Returns the secret with its value. Requires authentication.
// @Tags        secrets
// @Produce     json
// @Param       secretId path     string true "Secret ID"
// @Success     200      {object} model.Secret
// @Failure     404      {object} respond.ErrorBody
// @Failure     500      {object} respond.ErrorBody
// @Router      /secrets/{secretId} [get]
func (d *Dependencies) HandleGetSecret(w http.ResponseWriter, r *http.Request) {
	secretID := r.PathValue("secretId")

	sec, err := d.Store.GetSecret(r.Context(), secretID)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			respond.Error(w, http.StatusNotFound, "secret not found", d.Logger)
			return
		}
		d.Logger.Error("reading secret", slog.String("error", err.Error()))
		respond.Error(w, http.StatusInternalServerError, "reading secret", d.Logger)
		return
	}
	respond.JSON(w, http.StatusOK, sec, d.Logger)
}

// HandleUpdateSecret replaces the secret identified by secretId.
//
// @Summary     Update a secret
// @Description Replaces the secret with the given ID.
// @Tags        secrets
// @Accept      json
// @Produce     json
// @Param       secretId path     string       true "Secret ID"
// @Param       secret   body     model.Secret true "Secret definition"
// @Success     200      {object} model.SecretSummary
// @Failure     400      {object} respond.ErrorBody
// @Failure     500      {object} respond.ErrorBody
// @Router      /secrets/{secretId} [put]
func (d *Dependencies) HandleUpdateSecret(w http.ResponseWriter, r *http.Request) {
	secretID := r.PathValue("secretId")

	var sec model.Secret
	if err := json.NewDecoder(r.Body).Decode(&sec); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body", d.Logger)
		return
	}
	sec.ID = secretID

	if sec.Name == "" {
		respond.Error(w, http.StatusBadRequest, "name is required", d.Logger)
		return
	}
	if sec.Value == "" {
		respond.Error(w, http.StatusBadRequest, "value is required", d.Logger)
		return
	}

	if _, err := d.Store.GetSecret(r.Context(), secretID); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			respond.Error(w, http.StatusNotFound, "secret not found", d.Logger)
			return
		}
		d.Logger.Error("reading secret", slog.String("error", err.Error()))
		respond.Error(w, http.StatusInternalServerError, "reading secret", d.Logger)
		return
	}

	if err := d.Store.SaveSecret(r.Context(), sec); err != nil {
		d.Logger.Error("saving secret", slog.String("error", err.Error()))
		respond.Error(w, http.StatusInternalServerError, "saving secret", d.Logger)
		return
	}

	d.Logger.Info("secret updated",
		slog.String("id", sec.ID),
		slog.String("name", sec.Name),
	)

	respond.JSON(w, http.StatusOK, model.SecretSummary{ID: sec.ID, Name: sec.Name}, d.Logger)
}

// HandleDeleteSecret removes the secret with the given ID.
//
// @Summary     Delete a secret
// @Description Removes the secret. Entities referencing it will fail at next snapshot build.
// @Tags        secrets
// @Param       secretId path string true "Secret ID"
// @Success     204
// @Failure     404 {object} respond.ErrorBody
// @Failure     500 {object} respond.ErrorBody
// @Router      /secrets/{secretId} [delete]
func (d *Dependencies) HandleDeleteSecret(w http.ResponseWriter, r *http.Request) {
	secretID := r.PathValue("secretId")

	if err := d.Store.DeleteSecret(r.Context(), secretID); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			respond.Error(w, http.StatusNotFound, "secret not found", d.Logger)
			return
		}
		d.Logger.Error("deleting secret", slog.String("error", err.Error()))
		respond.Error(w, http.StatusInternalServerError, "deleting secret", d.Logger)
		return
	}

	d.Logger.Info("secret deleted", slog.String("id", secretID))
	w.WriteHeader(http.StatusNoContent)
}
