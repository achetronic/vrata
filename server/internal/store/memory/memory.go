// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package memory provides an in-memory implementation of store.Store.
// All data is stored in maps protected by a read-write mutex, so it is safe
// for concurrent use. Events are broadcast to all active subscribers via
// buffered channels. This implementation is suitable for testing and
// single-node ephemeral deployments.
package memory

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/achetronic/vrata/internal/model"
	"github.com/achetronic/vrata/internal/store"
)

// Store is an in-memory, thread-safe implementation of store.Store.
type Store struct {
	mu           sync.RWMutex
	routes       map[string]model.Route
	groups       map[string]model.RouteGroup
	middlewares  map[string]model.Middleware
	listeners    map[string]model.Listener
	destinations map[string]model.Destination
	secrets      map[string]model.Secret
	snapshots    map[string]model.VersionedSnapshot
	activeSnap   string

	subsMu sync.Mutex
	subs   []chan store.StoreEvent
}

var _ store.Store = (*Store)(nil)

// New creates an empty in-memory Store.
func New() *Store {
	return &Store{
		routes:       make(map[string]model.Route),
		groups:       make(map[string]model.RouteGroup),
		middlewares:  make(map[string]model.Middleware),
		listeners:    make(map[string]model.Listener),
		destinations: make(map[string]model.Destination),
		secrets:      make(map[string]model.Secret),
		snapshots:    make(map[string]model.VersionedSnapshot),
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

	evt := store.EventUpdated
	if _, ok := s.routes[route.ID]; !ok {
		evt = store.EventCreated
	}
	s.routes[route.ID] = route
	s.publish(store.StoreEvent{Type: evt, Resource: store.ResourceRoute, ID: route.ID})
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

	evt := store.EventUpdated
	if _, ok := s.groups[group.ID]; !ok {
		evt = store.EventCreated
	}
	s.groups[group.ID] = group
	s.publish(store.StoreEvent{Type: evt, Resource: store.ResourceGroup, ID: group.ID})
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
// Filter operations
// ────────────────────────────────────────────────────────────────────────────

// ListMiddlewares returns all filters in insertion-independent order.
func (s *Store) ListMiddlewares(_ context.Context) ([]model.Middleware, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.Middleware, 0, len(s.middlewares))
	for _, f := range s.middlewares {
		out = append(out, f)
	}
	return out, nil
}

// GetMiddleware returns the filter with the given ID, or model.ErrNotFound.
func (s *Store) GetMiddleware(_ context.Context, id string) (model.Middleware, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	f, ok := s.middlewares[id]
	if !ok {
		return model.Middleware{}, fmt.Errorf("middleware %q: %w", id, model.ErrNotFound)
	}
	return f, nil
}

// SaveMiddleware creates or replaces the filter identified by filter.ID.
func (s *Store) SaveMiddleware(_ context.Context, f model.Middleware) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	evt := store.EventUpdated
	if _, ok := s.middlewares[f.ID]; !ok {
		evt = store.EventCreated
	}
	s.middlewares[f.ID] = f
	s.publish(store.StoreEvent{Type: evt, Resource: store.ResourceMiddleware, ID: f.ID})
	return nil
}

// DeleteMiddleware removes the filter with the given ID.
func (s *Store) DeleteMiddleware(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.middlewares[id]; !ok {
		return fmt.Errorf("middleware %q: %w", id, model.ErrNotFound)
	}
	delete(s.middlewares, id)
	s.publish(store.StoreEvent{Type: store.EventDeleted, Resource: store.ResourceMiddleware, ID: id})
	return nil
}

// ────────────────────────────────────────────────────────────────────────────
// Listener operations
// ────────────────────────────────────────────────────────────────────────────

// ListListeners returns all listeners in insertion-independent order.
func (s *Store) ListListeners(_ context.Context) ([]model.Listener, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.Listener, 0, len(s.listeners))
	for _, l := range s.listeners {
		out = append(out, l)
	}
	return out, nil
}

// GetListener returns the listener with the given ID, or model.ErrNotFound.
func (s *Store) GetListener(_ context.Context, id string) (model.Listener, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	l, ok := s.listeners[id]
	if !ok {
		return model.Listener{}, fmt.Errorf("listener %q: %w", id, model.ErrNotFound)
	}
	return l, nil
}

// SaveListener creates or replaces the listener identified by listener.ID.
func (s *Store) SaveListener(_ context.Context, l model.Listener) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	evt := store.EventUpdated
	if _, ok := s.listeners[l.ID]; !ok {
		evt = store.EventCreated
	}
	s.listeners[l.ID] = l
	s.publish(store.StoreEvent{Type: evt, Resource: store.ResourceListener, ID: l.ID})
	return nil
}

// DeleteListener removes the listener with the given ID.
func (s *Store) DeleteListener(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.listeners[id]; !ok {
		return fmt.Errorf("listener %q: %w", id, model.ErrNotFound)
	}
	delete(s.listeners, id)
	s.publish(store.StoreEvent{Type: store.EventDeleted, Resource: store.ResourceListener, ID: id})
	return nil
}

// ────────────────────────────────────────────────────────────────────────────
// Destination operations
// ────────────────────────────────────────────────────────────────────────────

// ListDestinations returns all destinations in insertion-independent order.
func (s *Store) ListDestinations(_ context.Context) ([]model.Destination, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.Destination, 0, len(s.destinations))
	for _, d := range s.destinations {
		out = append(out, d)
	}
	return out, nil
}

// GetDestination returns the destination with the given ID, or model.ErrNotFound.
func (s *Store) GetDestination(_ context.Context, id string) (model.Destination, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	d, ok := s.destinations[id]
	if !ok {
		return model.Destination{}, fmt.Errorf("destination %q: %w", id, model.ErrNotFound)
	}
	return d, nil
}

// SaveDestination creates or replaces the destination identified by d.ID.
func (s *Store) SaveDestination(_ context.Context, d model.Destination) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	evt := store.EventUpdated
	if _, ok := s.destinations[d.ID]; !ok {
		evt = store.EventCreated
	}
	s.destinations[d.ID] = d
	s.publish(store.StoreEvent{Type: evt, Resource: store.ResourceDestination, ID: d.ID})
	return nil
}

// DeleteDestination removes the destination with the given ID.
func (s *Store) DeleteDestination(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.destinations[id]; !ok {
		return fmt.Errorf("destination %q: %w", id, model.ErrNotFound)
	}
	delete(s.destinations, id)
	s.publish(store.StoreEvent{Type: store.EventDeleted, Resource: store.ResourceDestination, ID: id})
	return nil
}

// ────────────────────────────────────────────────────────────────────────────
// Secret operations
// ────────────────────────────────────────────────────────────────────────────

// ListSecrets returns summary metadata (ID + Name) for all secrets.
func (s *Store) ListSecrets(_ context.Context) ([]model.SecretSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.SecretSummary, 0, len(s.secrets))
	for _, sec := range s.secrets {
		out = append(out, model.SecretSummary{ID: sec.ID, Name: sec.Name})
	}
	return out, nil
}

// GetSecret returns the secret with the given ID, including its Value.
func (s *Store) GetSecret(_ context.Context, id string) (model.Secret, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sec, ok := s.secrets[id]
	if !ok {
		return model.Secret{}, fmt.Errorf("secret %q: %w", id, model.ErrNotFound)
	}
	return sec, nil
}

// SaveSecret creates or replaces the secret identified by s.ID.
func (s *Store) SaveSecret(_ context.Context, sec model.Secret) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	evt := store.EventUpdated
	if _, ok := s.secrets[sec.ID]; !ok {
		evt = store.EventCreated
	}
	s.secrets[sec.ID] = sec
	s.publish(store.StoreEvent{Type: evt, Resource: store.ResourceSecret, ID: sec.ID})
	return nil
}

// DeleteSecret removes the secret with the given ID.
func (s *Store) DeleteSecret(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.secrets[id]; !ok {
		return fmt.Errorf("secret %q: %w", id, model.ErrNotFound)
	}
	delete(s.secrets, id)
	s.publish(store.StoreEvent{Type: store.EventDeleted, Resource: store.ResourceSecret, ID: id})
	return nil
}

// ────────────────────────────────────────────────────────────────────────────
// Snapshot operations
// ────────────────────────────────────────────────────────────────────────────

// ListSnapshots returns summary metadata for all versioned snapshots.
func (s *Store) ListSnapshots(_ context.Context) ([]model.SnapshotSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.SnapshotSummary, 0, len(s.snapshots))
	for _, vs := range s.snapshots {
		out = append(out, model.SnapshotSummary{
			ID:        vs.ID,
			Name:      vs.Name,
			CreatedAt: vs.CreatedAt,
			Active:    vs.ID == s.activeSnap,
		})
	}
	return out, nil
}

// GetSnapshot returns the versioned snapshot with the given ID.
func (s *Store) GetSnapshot(_ context.Context, id string) (model.VersionedSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	vs, ok := s.snapshots[id]
	if !ok {
		return model.VersionedSnapshot{}, fmt.Errorf("snapshot %q: %w", id, model.ErrNotFound)
	}
	return vs, nil
}

// SaveSnapshot creates or replaces a versioned snapshot.
func (s *Store) SaveSnapshot(_ context.Context, vs model.VersionedSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	evt := store.EventUpdated
	if _, ok := s.snapshots[vs.ID]; !ok {
		evt = store.EventCreated
	}
	s.snapshots[vs.ID] = vs
	s.publish(store.StoreEvent{Type: evt, Resource: store.ResourceSnapshot, ID: vs.ID})
	return nil
}

// DeleteSnapshot removes the versioned snapshot with the given ID.
func (s *Store) DeleteSnapshot(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.snapshots[id]; !ok {
		return fmt.Errorf("snapshot %q: %w", id, model.ErrNotFound)
	}
	delete(s.snapshots, id)
	if s.activeSnap == id {
		s.activeSnap = ""
	}
	s.publish(store.StoreEvent{Type: store.EventDeleted, Resource: store.ResourceSnapshot, ID: id})
	return nil
}

// ActivateSnapshot sets the given snapshot ID as the active configuration.
func (s *Store) ActivateSnapshot(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.snapshots[id]; !ok {
		return fmt.Errorf("snapshot %q: %w", id, model.ErrNotFound)
	}
	s.activeSnap = id
	s.publish(store.StoreEvent{Type: store.EventUpdated, Resource: store.ResourceSnapshot, ID: id})
	return nil
}

// GetActiveSnapshot returns the currently active versioned snapshot.
func (s *Store) GetActiveSnapshot(_ context.Context) (model.VersionedSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.activeSnap == "" {
		return model.VersionedSnapshot{}, model.ErrNoActiveSnapshot
	}
	vs, ok := s.snapshots[s.activeSnap]
	if !ok {
		return model.VersionedSnapshot{}, model.ErrNoActiveSnapshot
	}
	return vs, nil
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
// May be called while holding s.mu; acquires s.subsMu internally.
// Dropped events (slow subscriber) are silently discarded.
func (s *Store) publish(ev store.StoreEvent) {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()

	for _, ch := range s.subs {
		select {
		case ch <- ev:
		default:
			slog.Debug("store: subscriber too slow, dropping event",
				slog.String("type", string(ev.Type)),
				slog.String("resource", string(ev.Resource)),
			)
		}
	}
}
