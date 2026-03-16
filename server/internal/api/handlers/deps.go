// Package handlers implements the HTTP handlers for the Rutoso REST API.
// All handlers share a Dependencies struct injected at construction time.
package handlers

import (
	"log/slog"

	"github.com/achetronic/rutoso/internal/store"
	"github.com/achetronic/rutoso/internal/xds"
)

// Dependencies holds all external collaborators shared by the HTTP handlers.
type Dependencies struct {
	Store     store.Store
	XDSServer *xds.Server
	Logger    *slog.Logger
}
