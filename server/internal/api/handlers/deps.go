// Package handlers implements the HTTP handlers for the Rutoso REST API.
// All handlers share a Dependencies struct injected at construction time.
package handlers

import (
	"log/slog"

	"github.com/achetronic/rutoso/server/internal/store"
)

// Dependencies holds all external collaborators shared by the HTTP handlers.
type Dependencies struct {
	Store  store.Store
	Logger *slog.Logger
}
