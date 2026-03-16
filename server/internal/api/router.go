// Package api wires together all HTTP routes and middleware for the Rutoso REST API.
package api

import (
	"log/slog"
	"net/http"

	"github.com/achetronic/rutoso/internal/api/handlers"
	"github.com/achetronic/rutoso/internal/api/middleware"
	"github.com/achetronic/rutoso/internal/store"
)

// NewRouter creates and returns the root http.Handler for the Rutoso REST API.
// It registers all routes under /api/v1 and applies the logger and recovery
// middleware to every request.
func NewRouter(st store.Store, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	deps := handlers.Dependencies{
		Store:  st,
		Logger: logger,
	}

	groups := handlers.NewGroupsHandler(deps)
	routes := handlers.NewRoutesHandler(deps)

	// Route group endpoints
	mux.HandleFunc("GET /api/v1/route-groups", groups.HandleListGroups)
	mux.HandleFunc("POST /api/v1/route-groups", groups.HandleCreateGroup)
	mux.HandleFunc("GET /api/v1/route-groups/{groupId}", groups.HandleGetGroup)
	mux.HandleFunc("PUT /api/v1/route-groups/{groupId}", groups.HandleUpdateGroup)
	mux.HandleFunc("DELETE /api/v1/route-groups/{groupId}", groups.HandleDeleteGroup)

	// Route endpoints
	mux.HandleFunc("GET /api/v1/route-groups/{groupId}/routes", routes.HandleListRoutes)
	mux.HandleFunc("POST /api/v1/route-groups/{groupId}/routes", routes.HandleCreateRoute)
	mux.HandleFunc("GET /api/v1/route-groups/{groupId}/routes/{routeId}", routes.HandleGetRoute)
	mux.HandleFunc("PUT /api/v1/route-groups/{groupId}/routes/{routeId}", routes.HandleUpdateRoute)
	mux.HandleFunc("DELETE /api/v1/route-groups/{groupId}/routes/{routeId}", routes.HandleDeleteRoute)

	// Chain middleware: recovery wraps logger wraps mux.
	return middleware.Recovery(logger)(middleware.Logger(logger)(mux))
}
