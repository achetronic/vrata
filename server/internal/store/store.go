// Package store defines the persistence interface used by Rutoso to read and
// write route groups and routes, and to subscribe to state change events.
package store

import (
	"context"

	"github.com/achetronic/rutoso/server/internal/model"
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

	// GroupID is the ID of the affected group (always set).
	GroupID string

	// RouteID is the ID of the affected route. Empty when Resource is ResourceGroup.
	RouteID string
}

// Store is the persistence interface for all Rutoso state.
// All reads and writes must go through this interface; no component accesses
// storage directly. Implementations must be safe for concurrent use.
type Store interface {
	// --- Route Groups ---

	// ListGroups returns all route groups. The returned slice is never nil.
	ListGroups(ctx context.Context) ([]model.RouteGroup, error)

	// GetGroup returns the group with the given ID.
	// Returns model.ErrNotFound if no such group exists.
	GetGroup(ctx context.Context, id string) (model.RouteGroup, error)

	// CreateGroup persists a new route group.
	// Returns model.ErrDuplicateGroup if a group with the same name already exists.
	CreateGroup(ctx context.Context, g model.RouteGroup) error

	// UpdateGroup replaces an existing group's attributes (not its routes).
	// Returns model.ErrNotFound if the group does not exist.
	UpdateGroup(ctx context.Context, g model.RouteGroup) error

	// DeleteGroup removes a group and all its routes.
	// Returns model.ErrNotFound if the group does not exist.
	DeleteGroup(ctx context.Context, id string) error

	// --- Routes ---

	// ListRoutes returns all routes belonging to the given group.
	// Returns model.ErrNotFound if the group does not exist.
	ListRoutes(ctx context.Context, groupID string) ([]model.Route, error)

	// GetRoute returns a single route by group and route ID.
	// Returns model.ErrNotFound if either the group or the route does not exist.
	GetRoute(ctx context.Context, groupID, routeID string) (model.Route, error)

	// CreateRoute adds a new route to the given group.
	// Returns model.ErrNotFound if the group does not exist.
	// Returns model.ErrDuplicateRoute if a route with the same MatchRule already exists.
	CreateRoute(ctx context.Context, r model.Route) error

	// UpdateRoute replaces an existing route.
	// Returns model.ErrNotFound if the group or route does not exist.
	// Returns model.ErrDuplicateRoute if the updated MatchRule conflicts with another route.
	UpdateRoute(ctx context.Context, r model.Route) error

	// DeleteRoute removes a route from its group.
	// Returns model.ErrNotFound if the group or route does not exist.
	DeleteRoute(ctx context.Context, groupID, routeID string) error

	// --- Subscriptions ---

	// Subscribe returns a channel that receives a StoreEvent whenever any resource
	// changes. The channel is closed when ctx is cancelled. Multiple subscribers
	// are supported. Each subscriber receives all events independently.
	Subscribe(ctx context.Context) (<-chan StoreEvent, error)
}
