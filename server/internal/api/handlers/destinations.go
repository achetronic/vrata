// Package handlers wires HTTP request/response logic for the destinations resource.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/achetronic/rutoso/internal/api/respond"
	"github.com/achetronic/rutoso/internal/model"
	"github.com/google/uuid"
)

// ListDestinations returns all destinations stored in the database.
//
// @Summary     List destinations
// @Description Returns the full list of destinations.
// @Tags        destinations
// @Produce     json
// @Success     200 {array}   model.Destination
// @Failure     500 {object}  respond.ErrorBody
// @Router      /destinations [get]
func (d *Dependencies) ListDestinations(w http.ResponseWriter, r *http.Request) {
	destinations, err := d.Store.ListDestinations(context.Background())
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error(), d.Logger)
		return
	}
	respond.JSON(w, http.StatusOK, destinations, d.Logger)
}

// CreateDestination creates a new destination and persists it in the database.
//
// @Summary     Create a destination
// @Description Creates a new upstream destination entity.
// @Tags        destinations
// @Accept      json
// @Produce     json
// @Param       destination body      model.Destination true "Destination definition"
// @Success     201         {object}  model.Destination
// @Failure     400         {object}  respond.ErrorBody
// @Failure     500         {object}  respond.ErrorBody
// @Router      /destinations [post]
func (d *Dependencies) CreateDestination(w http.ResponseWriter, r *http.Request) {
	var destination model.Destination
	if err := json.NewDecoder(r.Body).Decode(&destination); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error(), d.Logger)
		return
	}

	if destination.ID == "" {
		destination.ID = uuid.NewString()
	}

	if err := d.Store.SaveDestination(context.Background(), destination); err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error(), d.Logger)
		return
	}

	respond.JSON(w, http.StatusCreated, destination, d.Logger)
}

// GetDestination returns the destination identified by destinationId.
//
// @Summary     Get a destination
// @Description Returns the destination with the given ID.
// @Tags        destinations
// @Produce     json
// @Param       destinationId path     string true "Destination ID"
// @Success     200           {object} model.Destination
// @Failure     404           {object} respond.ErrorBody
// @Failure     500           {object} respond.ErrorBody
// @Router      /destinations/{destinationId} [get]
func (d *Dependencies) GetDestination(w http.ResponseWriter, r *http.Request) {
	destinationID := r.PathValue("destinationId")

	destination, err := d.Store.GetDestination(context.Background(), destinationID)
	if err != nil {
		respond.Error(w, http.StatusNotFound, err.Error(), d.Logger)
		return
	}
	respond.JSON(w, http.StatusOK, destination, d.Logger)
}

// UpdateDestination replaces an existing destination.
//
// @Summary     Update a destination
// @Description Replaces the destination with the given ID.
// @Tags        destinations
// @Accept      json
// @Produce     json
// @Param       destinationId path     string            true "Destination ID"
// @Param       destination   body     model.Destination true "Updated destination definition"
// @Success     200           {object} model.Destination
// @Failure     400           {object} respond.ErrorBody
// @Failure     404           {object} respond.ErrorBody
// @Failure     500           {object} respond.ErrorBody
// @Router      /destinations/{destinationId} [put]
func (d *Dependencies) UpdateDestination(w http.ResponseWriter, r *http.Request) {
	destinationID := r.PathValue("destinationId")

	if _, err := d.Store.GetDestination(context.Background(), destinationID); err != nil {
		respond.Error(w, http.StatusNotFound, err.Error(), d.Logger)
		return
	}

	var destination model.Destination
	if err := json.NewDecoder(r.Body).Decode(&destination); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error(), d.Logger)
		return
	}
	destination.ID = destinationID

	if err := d.Store.SaveDestination(context.Background(), destination); err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error(), d.Logger)
		return
	}

	respond.JSON(w, http.StatusOK, destination, d.Logger)
}

// DeleteDestination removes the destination identified by destinationId.
//
// @Summary     Delete a destination
// @Description Deletes the destination with the given ID.
// @Tags        destinations
// @Produce     json
// @Param       destinationId path     string true "Destination ID"
// @Success     204           "No Content"
// @Failure     404           {object} respond.ErrorBody
// @Failure     500           {object} respond.ErrorBody
// @Router      /destinations/{destinationId} [delete]
func (d *Dependencies) DeleteDestination(w http.ResponseWriter, r *http.Request) {
	destinationID := r.PathValue("destinationId")

	if err := d.Store.DeleteDestination(context.Background(), destinationID); err != nil {
		respond.Error(w, http.StatusNotFound, err.Error(), d.Logger)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
