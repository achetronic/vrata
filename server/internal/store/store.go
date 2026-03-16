// Package store defines the persistence interface used by Rutoso to read and
// write routes and route groups, and to subscribe to state change events.
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
)

// StoreEvent is emitted by the store whenever the state changes.
// The gateway layer subscribes to these events to trigger xDS snapshot rebuilds.
type StoreEvent struct {
	// Type indicates whether the resource was created, updated, or deleted.
	Type EventType

	// Resource indicates whether a group or a route changed.
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

	// --- Subscriptions ---

	// Subscribe returns a channel that receives a StoreEvent whenever any resource
	// changes. The channel is closed when ctx is cancelled. Multiple subscribers
	// are supported. Each subscriber receives all events independently.
	Subscribe(ctx context.Context) (<-chan StoreEvent, error)
}
