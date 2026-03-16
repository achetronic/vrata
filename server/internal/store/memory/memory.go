// Package memory provides an in-memory implementation of store.Store.
// All data is stored in maps protected by a read-write mutex, so it is safe
// for concurrent use. Events are broadcast to all active subscribers via
// buffered channels. This implementation is suitable for testing and
// single-node ephemeral deployments.
package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/achetronic/rutoso/internal/model"
	"github.com/achetronic/rutoso/internal/store"
)

// Store is an in-memory, thread-safe implementation of store.Store.
type Store struct {
	mu     sync.RWMutex
	routes map[string]model.Route      // keyed by route ID
	groups map[string]model.RouteGroup // keyed by group ID

	subsMu sync.Mutex
	subs   []chan store.StoreEvent
}

// New creates an empty in-memory Store.
func New() *Store {
	return &Store{
		routes: make(map[string]model.Route),
		groups: make(map[string]model.RouteGroup),
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Route operations
// ────────────────────────────────────────────────────────────────────────────

// ListRoutes returns all routes in insertion-independent order.
func (s *Store) ListRoutes(_ context.Context) ([]model.Route, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.Route, 0, len(s.routes))
	for _, r := range s.routes {
		out = append(out, r)
	}
	return out, nil
}

// GetRoute returns the route with the given ID, or model.ErrNotFound.
func (s *Store) GetRoute(_ context.Context, id string) (model.Route, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	r, ok := s.routes[id]
	if !ok {
		return model.Route{}, fmt.Errorf("route %q: %w", id, model.ErrNotFound)
	}
	return r, nil
}

// SaveRoute creates or replaces the route identified by route.ID.
func (s *Store) SaveRoute(_ context.Context, route model.Route) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.routes[route.ID] = route
	s.publish(store.StoreEvent{Type: store.EventCreated, Resource: store.ResourceRoute, ID: route.ID})
	return nil
}

// DeleteRoute removes the route with the given ID.
func (s *Store) DeleteRoute(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.routes[id]; !ok {
		return fmt.Errorf("route %q: %w", id, model.ErrNotFound)
	}
	delete(s.routes, id)
	s.publish(store.StoreEvent{Type: store.EventDeleted, Resource: store.ResourceRoute, ID: id})
	return nil
}

// ────────────────────────────────────────────────────────────────────────────
// Group operations
// ────────────────────────────────────────────────────────────────────────────

// ListGroups returns all groups in insertion-independent order.
func (s *Store) ListGroups(_ context.Context) ([]model.RouteGroup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.RouteGroup, 0, len(s.groups))
	for _, g := range s.groups {
		out = append(out, g)
	}
	return out, nil
}

// GetGroup returns the group with the given ID, or model.ErrNotFound.
func (s *Store) GetGroup(_ context.Context, id string) (model.RouteGroup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	g, ok := s.groups[id]
	if !ok {
		return model.RouteGroup{}, fmt.Errorf("group %q: %w", id, model.ErrNotFound)
	}
	return g, nil
}

// SaveGroup creates or replaces the group identified by group.ID.
func (s *Store) SaveGroup(_ context.Context, group model.RouteGroup) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.groups[group.ID] = group
	s.publish(store.StoreEvent{Type: store.EventCreated, Resource: store.ResourceGroup, ID: group.ID})
	return nil
}

// DeleteGroup removes the group with the given ID.
func (s *Store) DeleteGroup(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.groups[id]; !ok {
		return fmt.Errorf("group %q: %w", id, model.ErrNotFound)
	}
	delete(s.groups, id)
	s.publish(store.StoreEvent{Type: store.EventDeleted, Resource: store.ResourceGroup, ID: id})
	return nil
}

// ────────────────────────────────────────────────────────────────────────────
// Subscriptions
// ────────────────────────────────────────────────────────────────────────────

// Subscribe returns a channel that receives StoreEvents until ctx is cancelled.
func (s *Store) Subscribe(ctx context.Context) (<-chan store.StoreEvent, error) {
	ch := make(chan store.StoreEvent, 32)

	s.subsMu.Lock()
	s.subs = append(s.subs, ch)
	s.subsMu.Unlock()

	go func() {
		<-ctx.Done()
		s.subsMu.Lock()
		for i, sub := range s.subs {
			if sub == ch {
				s.subs = append(s.subs[:i], s.subs[i+1:]...)
				break
			}
		}
		s.subsMu.Unlock()
		close(ch)
	}()

	return ch, nil
}

// publish sends an event to all current subscribers in a non-blocking manner.
// Must be called while holding s.mu (write lock).
func (s *Store) publish(ev store.StoreEvent) {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()

	for _, ch := range s.subs {
		select {
		case ch <- ev:
		default:
			// Subscriber too slow; drop the event rather than blocking.
		}
	}
}
