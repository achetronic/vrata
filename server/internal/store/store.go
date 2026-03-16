// Package store defines the persistence interface used by Rutoso to read and
// write routes, route groups, filters, listeners, and destinations, and to
// subscribe to state change events.
package store

import (
	"context"

	"github.com/achetronic/rutoso/internal/model"
)

// EventType classifies what kind of change a StoreEvent represents.
type EventType string

const (
	// EventCreated is emitted when a new resource is created.
	EventCreated EventType = "created"

	// EventUpdated is emitted when an existing resource is updated.
	EventUpdated EventType = "updated"

	// EventDeleted is emitted when a resource is deleted.
	EventDeleted EventType = "deleted"
)

// ResourceType identifies which domain resource a StoreEvent refers to.
type ResourceType string

const (
	// ResourceGroup refers to a RouteGroup resource.
	ResourceGroup ResourceType = "group"

	// ResourceRoute refers to a Route resource.
	ResourceRoute ResourceType = "route"

	// ResourceMiddleware refers to a Filter resource.
	ResourceMiddleware ResourceType = "middleware"

	// ResourceListener refers to a Listener resource.
	ResourceListener ResourceType = "listener"

	// ResourceDestination refers to a Destination resource.
	ResourceDestination ResourceType = "destination"
)

// StoreEvent is emitted by the store whenever the state changes.
// The gateway layer subscribes to these events to trigger xDS snapshot rebuilds.
type StoreEvent struct {
	// Type indicates whether the resource was created, updated, or deleted.
	Type EventType

	// Resource indicates which resource type changed.
	Resource ResourceType

	// ID is the identifier of the affected resource.
	ID string
}

// Store is the persistence interface for all Rutoso state.
// All reads and writes must go through this interface; no component accesses
// storage directly. Implementations must be safe for concurrent use.
type Store interface {
	// --- Routes ---

	// ListRoutes returns all routes. The returned slice is never nil.
	ListRoutes(ctx context.Context) ([]model.Route, error)

	// GetRoute returns the route with the given ID.
	// Returns model.ErrNotFound if no such route exists.
	GetRoute(ctx context.Context, id string) (model.Route, error)

	// SaveRoute creates or replaces the route identified by route.ID.
	SaveRoute(ctx context.Context, r model.Route) error

	// DeleteRoute removes the route with the given ID.
	// Returns model.ErrNotFound if the route does not exist.
	DeleteRoute(ctx context.Context, id string) error

	// --- Route Groups ---

	// ListGroups returns all route groups. The returned slice is never nil.
	ListGroups(ctx context.Context) ([]model.RouteGroup, error)

	// GetGroup returns the group with the given ID.
	// Returns model.ErrNotFound if no such group exists.
	GetGroup(ctx context.Context, id string) (model.RouteGroup, error)

	// SaveGroup creates or replaces the group identified by group.ID.
	SaveGroup(ctx context.Context, g model.RouteGroup) error

	// DeleteGroup removes the group with the given ID.
	// Returns model.ErrNotFound if the group does not exist.
	DeleteGroup(ctx context.Context, id string) error

	// --- Filters ---

	// ListMiddlewares returns all filters. The returned slice is never nil.
	ListMiddlewares(ctx context.Context) ([]model.Middleware, error)

	// GetMiddleware returns the filter with the given ID.
	// Returns model.ErrNotFound if no such filter exists.
	GetMiddleware(ctx context.Context, id string) (model.Middleware, error)

	// SaveMiddleware creates or replaces the filter identified by filter.ID.
	SaveMiddleware(ctx context.Context, f model.Middleware) error

	// DeleteMiddleware removes the filter with the given ID.
	// Returns model.ErrNotFound if the filter does not exist.
	DeleteMiddleware(ctx context.Context, id string) error

	// --- Listeners ---

	// ListListeners returns all listeners. The returned slice is never nil.
	ListListeners(ctx context.Context) ([]model.Listener, error)

	// GetListener returns the listener with the given ID.
	// Returns model.ErrNotFound if no such listener exists.
	GetListener(ctx context.Context, id string) (model.Listener, error)

	// SaveListener creates or replaces the listener identified by listener.ID.
	SaveListener(ctx context.Context, l model.Listener) error

	// DeleteListener removes the listener with the given ID.
	// Returns model.ErrNotFound if the listener does not exist.
	DeleteListener(ctx context.Context, id string) error

	// --- Destinations ---

	// ListDestinations returns all destinations. The returned slice is never nil.
	ListDestinations(ctx context.Context) ([]model.Destination, error)

	// GetDestination returns the destination with the given ID.
	// Returns model.ErrNotFound if no such destination exists.
	GetDestination(ctx context.Context, id string) (model.Destination, error)

	// SaveDestination creates or replaces the destination identified by d.ID.
	SaveDestination(ctx context.Context, d model.Destination) error

	// DeleteDestination removes the destination with the given ID.
	// Returns model.ErrNotFound if the destination does not exist.
	DeleteDestination(ctx context.Context, id string) error

	// --- Subscriptions ---

	// Subscribe returns a channel that receives a StoreEvent whenever any resource
	// changes. The channel is closed when ctx is cancelled. Multiple subscribers
	// are supported. Each subscriber receives all events independently.
	Subscribe(ctx context.Context) (<-chan StoreEvent, error)
}
