// Package memory provides an in-memory implementation of the store.Store interface.
// It stores all data in maps protected by a read-write mutex, so it is safe for
// concurrent use. Events are broadcast to all active subscribers via buffered channels.
// This implementation is suitable for development and single-node deployments.
// A persistent backend can be swapped in later by implementing store.Store.
package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/achetronic/rutoso/server/internal/model"
	"github.com/achetronic/rutoso/server/internal/store"
)

// Store is an in-memory, thread-safe implementation of store.Store.
type Store struct {
	mu     sync.RWMutex
	groups map[string]model.RouteGroup // keyed by group ID
	routes map[string]model.Route      // keyed by route ID

	subsMu sync.Mutex
	subs   []chan store.StoreEvent
}

// New creates an empty in-memory Store.
func New() *Store {
	return &Store{
		groups: make(map[string]model.RouteGroup),
		routes: make(map[string]model.Route),
	}
}

// --- Route Groups ---

// ListGroups returns all route groups in insertion-independent order.
func (s *Store) ListGroups(_ context.Context) ([]model.RouteGroup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.RouteGroup, 0, len(s.groups))
	for _, g := range s.groups {
		out = append(out, g)
	}
	return out, nil
}

// GetGroup returns the group with the given ID or model.ErrNotFound.
func (s *Store) GetGroup(_ context.Context, id string) (model.RouteGroup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	g, ok := s.groups[id]
	if !ok {
		return model.RouteGroup{}, fmt.Errorf("group %q: %w", id, model.ErrNotFound)
	}
	return g, nil
}

// CreateGroup persists a new route group. Returns model.ErrDuplicateGroup if
// a group with the same name already exists.
func (s *Store) CreateGroup(_ context.Context, g model.RouteGroup) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, existing := range s.groups {
		if existing.Name == g.Name {
			return fmt.Errorf("group name %q: %w", g.Name, model.ErrDuplicateGroup)
		}
	}

	s.groups[g.ID] = g
	s.publish(store.StoreEvent{Type: store.EventCreated, Resource: store.ResourceGroup, GroupID: g.ID})
	return nil
}

// UpdateGroup replaces an existing group's metadata (not its routes).
// Returns model.ErrNotFound if the group does not exist.
func (s *Store) UpdateGroup(_ context.Context, g model.RouteGroup) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.groups[g.ID]; !ok {
		return fmt.Errorf("group %q: %w", g.ID, model.ErrNotFound)
	}

	// Preserve the embedded routes; only update top-level metadata.
	existing := s.groups[g.ID]
	g.Routes = existing.Routes
	s.groups[g.ID] = g

	s.publish(store.StoreEvent{Type: store.EventUpdated, Resource: store.ResourceGroup, GroupID: g.ID})
	return nil
}

// DeleteGroup removes a group and all its routes.
// Returns model.ErrNotFound if the group does not exist.
func (s *Store) DeleteGroup(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.groups[id]; !ok {
		return fmt.Errorf("group %q: %w", id, model.ErrNotFound)
	}

	// Remove all routes belonging to this group.
	for routeID, r := range s.routes {
		if r.GroupID == id {
			delete(s.routes, routeID)
		}
	}
	delete(s.groups, id)

	s.publish(store.StoreEvent{Type: store.EventDeleted, Resource: store.ResourceGroup, GroupID: id})
	return nil
}

// --- Routes ---

// ListRoutes returns all routes for the given group.
// Returns model.ErrNotFound if the group does not exist.
func (s *Store) ListRoutes(_ context.Context, groupID string) ([]model.Route, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.groups[groupID]; !ok {
		return nil, fmt.Errorf("group %q: %w", groupID, model.ErrNotFound)
	}

	var out []model.Route
	for _, r := range s.routes {
		if r.GroupID == groupID {
			out = append(out, r)
		}
	}
	return out, nil
}

// GetRoute returns a single route by group and route ID.
// Returns model.ErrNotFound if either the group or the route does not exist.
func (s *Store) GetRoute(_ context.Context, groupID, routeID string) (model.Route, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.groups[groupID]; !ok {
		return model.Route{}, fmt.Errorf("group %q: %w", groupID, model.ErrNotFound)
	}

	r, ok := s.routes[routeID]
	if !ok || r.GroupID != groupID {
		return model.Route{}, fmt.Errorf("route %q in group %q: %w", routeID, groupID, model.ErrNotFound)
	}
	return r, nil
}

// CreateRoute adds a new route to a group.
// Returns model.ErrNotFound if the group does not exist.
// Returns model.ErrDuplicateRoute if a route with the same MatchRule exists.
func (s *Store) CreateRoute(_ context.Context, r model.Route) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.groups[r.GroupID]; !ok {
		return fmt.Errorf("group %q: %w", r.GroupID, model.ErrNotFound)
	}

	if err := s.checkDuplicate(r, ""); err != nil {
		return err
	}

	s.routes[r.ID] = r
	s.publish(store.StoreEvent{Type: store.EventCreated, Resource: store.ResourceRoute, GroupID: r.GroupID, RouteID: r.ID})
	return nil
}

// UpdateRoute replaces an existing route.
// Returns model.ErrNotFound if the group or route does not exist.
// Returns model.ErrDuplicateRoute if the updated MatchRule conflicts with another route.
func (s *Store) UpdateRoute(_ context.Context, r model.Route) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.groups[r.GroupID]; !ok {
		return fmt.Errorf("group %q: %w", r.GroupID, model.ErrNotFound)
	}
	if _, ok := s.routes[r.ID]; !ok {
		return fmt.Errorf("route %q: %w", r.ID, model.ErrNotFound)
	}

	if err := s.checkDuplicate(r, r.ID); err != nil {
		return err
	}

	s.routes[r.ID] = r
	s.publish(store.StoreEvent{Type: store.EventUpdated, Resource: store.ResourceRoute, GroupID: r.GroupID, RouteID: r.ID})
	return nil
}

// DeleteRoute removes a route from its group.
// Returns model.ErrNotFound if the group or route does not exist.
func (s *Store) DeleteRoute(_ context.Context, groupID, routeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.groups[groupID]; !ok {
		return fmt.Errorf("group %q: %w", groupID, model.ErrNotFound)
	}

	r, ok := s.routes[routeID]
	if !ok || r.GroupID != groupID {
		return fmt.Errorf("route %q in group %q: %w", routeID, groupID, model.ErrNotFound)
	}

	delete(s.routes, routeID)
	s.publish(store.StoreEvent{Type: store.EventDeleted, Resource: store.ResourceRoute, GroupID: groupID, RouteID: routeID})
	return nil
}

// --- Subscriptions ---

// Subscribe returns a channel that receives StoreEvents until ctx is cancelled.
// The channel has a small buffer; slow consumers may miss events.
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

// --- Helpers ---

// publish sends an event to all current subscribers in a non-blocking manner.
// Must be called while holding s.mu (write lock).
func (s *Store) publish(ev store.StoreEvent) {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()

	for _, ch := range s.subs {
		select {
		case ch <- ev:
		default:
			// Subscriber is too slow; drop the event rather than blocking.
		}
	}
}

// checkDuplicate returns model.ErrDuplicateRoute if any existing route in the same
// group has the same MatchRule as r. Pass skipID to exclude the route being updated.
// Must be called while holding s.mu (write lock).
func (s *Store) checkDuplicate(r model.Route, skipID string) error {
	for id, existing := range s.routes {
		if id == skipID || existing.GroupID != r.GroupID {
			continue
		}
		if matchRulesEqual(existing.Match, r.Match) {
			return fmt.Errorf("route %q: %w", r.ID, model.ErrDuplicateRoute)
		}
	}
	return nil
}

// matchRulesEqual returns true if two MatchRules have the same path specifier.
// Header and hostname differences are intentionally ignored for duplicate detection:
// two routes sharing the same path would be ambiguous regardless of other matchers.
func matchRulesEqual(a, b model.MatchRule) bool {
	return a.Path == b.Path &&
		a.PathPrefix == b.PathPrefix &&
		a.PathRegex == b.PathRegex
}
