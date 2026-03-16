// Package handlers wires HTTP request/response logic for the filters resource.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/achetronic/rutoso/internal/api/respond"
	"github.com/achetronic/rutoso/internal/model"
	"github.com/google/uuid"
)

// ListFilters returns all filters stored in the database.
//
// @Summary     List filters
// @Description Returns the full list of filters.
// @Tags        filters
// @Produce     json
// @Success     200 {array}   model.Filter
// @Failure     500 {object}  respond.ErrorBody
// @Router      /filters [get]
func (d *Dependencies) ListFilters(w http.ResponseWriter, r *http.Request) {
	filters, err := d.Store.ListFilters(context.Background())
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error(), d.Logger)
		return
	}
	respond.JSON(w, http.StatusOK, filters, d.Logger)
}

// CreateFilter creates a new filter and persists it in the database.
//
// @Summary     Create a filter
// @Description Creates a new HTTP filter entity.
// @Tags        filters
// @Accept      json
// @Produce     json
// @Param       filter body      model.Filter true "Filter definition"
// @Success     201    {object}  model.Filter
// @Failure     400    {object}  respond.ErrorBody
// @Failure     500    {object}  respond.ErrorBody
// @Router      /filters [post]
func (d *Dependencies) CreateFilter(w http.ResponseWriter, r *http.Request) {
	var filter model.Filter
	if err := json.NewDecoder(r.Body).Decode(&filter); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error(), d.Logger)
		return
	}

	if filter.ID == "" {
		filter.ID = uuid.NewString()
	}

	if err := d.Store.SaveFilter(context.Background(), filter); err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error(), d.Logger)
		return
	}

	respond.JSON(w, http.StatusCreated, filter, d.Logger)
}

// GetFilter returns the filter identified by filterId.
//
// @Summary     Get a filter
// @Description Returns the filter with the given ID.
// @Tags        filters
// @Produce     json
// @Param       filterId path     string true "Filter ID"
// @Success     200      {object} model.Filter
// @Failure     404      {object} respond.ErrorBody
// @Failure     500      {object} respond.ErrorBody
// @Router      /filters/{filterId} [get]
func (d *Dependencies) GetFilter(w http.ResponseWriter, r *http.Request) {
	filterID := r.PathValue("filterId")

	filter, err := d.Store.GetFilter(context.Background(), filterID)
	if err != nil {
		respond.Error(w, http.StatusNotFound, err.Error(), d.Logger)
		return
	}
	respond.JSON(w, http.StatusOK, filter, d.Logger)
}

// UpdateFilter replaces an existing filter.
//
// @Summary     Update a filter
// @Description Replaces the filter with the given ID.
// @Tags        filters
// @Accept      json
// @Produce     json
// @Param       filterId path     string       true "Filter ID"
// @Param       filter   body     model.Filter true "Updated filter definition"
// @Success     200      {object} model.Filter
// @Failure     400      {object} respond.ErrorBody
// @Failure     404      {object} respond.ErrorBody
// @Failure     500      {object} respond.ErrorBody
// @Router      /filters/{filterId} [put]
func (d *Dependencies) UpdateFilter(w http.ResponseWriter, r *http.Request) {
	filterID := r.PathValue("filterId")

	if _, err := d.Store.GetFilter(context.Background(), filterID); err != nil {
		respond.Error(w, http.StatusNotFound, err.Error(), d.Logger)
		return
	}

	var filter model.Filter
	if err := json.NewDecoder(r.Body).Decode(&filter); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error(), d.Logger)
		return
	}
	filter.ID = filterID

	if err := d.Store.SaveFilter(context.Background(), filter); err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error(), d.Logger)
		return
	}

	respond.JSON(w, http.StatusOK, filter, d.Logger)
}

// DeleteFilter removes the filter identified by filterId.
//
// @Summary     Delete a filter
// @Description Deletes the filter with the given ID.
// @Tags        filters
// @Produce     json
// @Param       filterId path     string true "Filter ID"
// @Success     204      "No Content"
// @Failure     404      {object} respond.ErrorBody
// @Failure     500      {object} respond.ErrorBody
// @Router      /filters/{filterId} [delete]
func (d *Dependencies) DeleteFilter(w http.ResponseWriter, r *http.Request) {
	filterID := r.PathValue("filterId")

	if err := d.Store.DeleteFilter(context.Background(), filterID); err != nil {
		respond.Error(w, http.StatusNotFound, err.Error(), d.Logger)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
