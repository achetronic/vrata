// Package bolt provides a persistent implementation of the store.Store interface
// backed by bbolt (an embedded key/value database). All data is stored in a single
// file, making backups trivial. The file is safe for concurrent reads and single
// writes within the same process.
//
// Two bbolt buckets are used:
//
//	"groups" — keyed by group ID, value is JSON-encoded model.RouteGroup
//	"routes" — keyed by route ID, value is JSON-encoded model.Route
//
// Events are broadcast to subscribers in the same way as the memory store.
package bolt

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	bolt "go.etcd.io/bbolt"

	"github.com/achetronic/rutoso/internal/model"
	"github.com/achetronic/rutoso/internal/store"
)

var (
	bucketGroups = []byte("groups")
	bucketRoutes = []byte("routes")
)

// Store is a bbolt-backed, persistent implementation of store.Store.
type Store struct {
	db *bolt.DB

	subsMu sync.Mutex
	subs   []chan store.StoreEvent
}

// New opens (or creates) the bbolt database at the given path and returns a Store.
// The caller must call Close when the store is no longer needed.
func New(path string) (*Store, error) {
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		return nil, fmt.Errorf("bolt: open %q: %w", path, err)
	}

	// Ensure both buckets exist.
	if err := db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(bucketGroups); err != nil {
			return err
		}
		_, err := tx.CreateBucketIfNotExists(bucketRoutes)
		return err
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("bolt: create buckets: %w", err)
	}

	return &Store{db: db}, nil
}

// Close releases the bbolt file lock and flushes pending writes.
func (s *Store) Close() error {
	return s.db.Close()
}

// --- Route Groups ---

// ListGroups returns all route groups stored in the database.
func (s *Store) ListGroups(_ context.Context) ([]model.RouteGroup, error) {
	var out []model.RouteGroup
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketGroups)
		return b.ForEach(func(_, v []byte) error {
			var g model.RouteGroup
			if err := json.Unmarshal(v, &g); err != nil {
				return err
			}
			out = append(out, g)
			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("bolt: list groups: %w", err)
	}
	if out == nil {
		out = []model.RouteGroup{}
	}
	return out, nil
}

// GetGroup returns the group with the given ID or model.ErrNotFound.
func (s *Store) GetGroup(_ context.Context, id string) (model.RouteGroup, error) {
	var g model.RouteGroup
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketGroups).Get([]byte(id))
		if v == nil {
			return fmt.Errorf("group %q: %w", id, model.ErrNotFound)
		}
		return json.Unmarshal(v, &g)
	})
	return g, err
}

// CreateGroup persists a new route group.
// Returns model.ErrDuplicateGroup if a group with the same name already exists.
func (s *Store) CreateGroup(_ context.Context, g model.RouteGroup) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketGroups)

		// Check name uniqueness.
		if err := b.ForEach(func(_, v []byte) error {
			var existing model.RouteGroup
			if err := json.Unmarshal(v, &existing); err != nil {
				return err
			}
			if existing.Name == g.Name {
				return fmt.Errorf("group name %q: %w", g.Name, model.ErrDuplicateGroup)
			}
			return nil
		}); err != nil {
			return err
		}

		data, err := json.Marshal(g)
		if err != nil {
			return err
		}
		if err := b.Put([]byte(g.ID), data); err != nil {
			return err
		}
		s.publish(store.StoreEvent{Type: store.EventCreated, Resource: store.ResourceGroup, GroupID: g.ID})
		return nil
	})
}

// UpdateGroup replaces an existing group's metadata (not its routes).
// Returns model.ErrNotFound if the group does not exist.
func (s *Store) UpdateGroup(_ context.Context, g model.RouteGroup) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketGroups)
		existing := b.Get([]byte(g.ID))
		if existing == nil {
			return fmt.Errorf("group %q: %w", g.ID, model.ErrNotFound)
		}

		// Preserve embedded routes field.
		var prev model.RouteGroup
		if err := json.Unmarshal(existing, &prev); err != nil {
			return err
		}
		g.Routes = prev.Routes

		data, err := json.Marshal(g)
		if err != nil {
			return err
		}
		if err := b.Put([]byte(g.ID), data); err != nil {
			return err
		}
		s.publish(store.StoreEvent{Type: store.EventUpdated, Resource: store.ResourceGroup, GroupID: g.ID})
		return nil
	})
}

// DeleteGroup removes a group and all its routes.
// Returns model.ErrNotFound if the group does not exist.
func (s *Store) DeleteGroup(_ context.Context, id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		gb := tx.Bucket(bucketGroups)
		if gb.Get([]byte(id)) == nil {
			return fmt.Errorf("group %q: %w", id, model.ErrNotFound)
		}

		// Remove all routes belonging to this group.
		rb := tx.Bucket(bucketRoutes)
		var routeKeys [][]byte
		if err := rb.ForEach(func(k, v []byte) error {
			var r model.Route
			if err := json.Unmarshal(v, &r); err != nil {
				return err
			}
			if r.GroupID == id {
				routeKeys = append(routeKeys, append([]byte{}, k...))
			}
			return nil
		}); err != nil {
			return err
		}
		for _, k := range routeKeys {
			if err := rb.Delete(k); err != nil {
				return err
			}
		}

		if err := gb.Delete([]byte(id)); err != nil {
			return err
		}
		s.publish(store.StoreEvent{Type: store.EventDeleted, Resource: store.ResourceGroup, GroupID: id})
		return nil
	})
}

// --- Routes ---

// ListRoutes returns all routes for the given group.
// Returns model.ErrNotFound if the group does not exist.
func (s *Store) ListRoutes(_ context.Context, groupID string) ([]model.Route, error) {
	var out []model.Route
	err := s.db.View(func(tx *bolt.Tx) error {
		if tx.Bucket(bucketGroups).Get([]byte(groupID)) == nil {
			return fmt.Errorf("group %q: %w", groupID, model.ErrNotFound)
		}
		return tx.Bucket(bucketRoutes).ForEach(func(_, v []byte) error {
			var r model.Route
			if err := json.Unmarshal(v, &r); err != nil {
				return err
			}
			if r.GroupID == groupID {
				out = append(out, r)
			}
			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("bolt: list routes: %w", err)
	}
	if out == nil {
		out = []model.Route{}
	}
	return out, nil
}

// GetRoute returns a single route by group and route ID.
// Returns model.ErrNotFound if either the group or the route does not exist.
func (s *Store) GetRoute(_ context.Context, groupID, routeID string) (model.Route, error) {
	var r model.Route
	err := s.db.View(func(tx *bolt.Tx) error {
		if tx.Bucket(bucketGroups).Get([]byte(groupID)) == nil {
			return fmt.Errorf("group %q: %w", groupID, model.ErrNotFound)
		}
		v := tx.Bucket(bucketRoutes).Get([]byte(routeID))
		if v == nil {
			return fmt.Errorf("route %q in group %q: %w", routeID, groupID, model.ErrNotFound)
		}
		if err := json.Unmarshal(v, &r); err != nil {
			return err
		}
		if r.GroupID != groupID {
			return fmt.Errorf("route %q in group %q: %w", routeID, groupID, model.ErrNotFound)
		}
		return nil
	})
	return r, err
}

// CreateRoute adds a new route to a group.
// Returns model.ErrNotFound if the group does not exist.
// Returns model.ErrDuplicateRoute if a route with the same MatchRule exists in the group.
func (s *Store) CreateRoute(_ context.Context, r model.Route) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		gb := tx.Bucket(bucketGroups)
		rb := tx.Bucket(bucketRoutes)

		if gb.Get([]byte(r.GroupID)) == nil {
			return fmt.Errorf("group %q: %w", r.GroupID, model.ErrNotFound)
		}
		if err := checkDuplicate(rb, r, ""); err != nil {
			return err
		}

		data, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if err := rb.Put([]byte(r.ID), data); err != nil {
			return err
		}
		s.publish(store.StoreEvent{Type: store.EventCreated, Resource: store.ResourceRoute, GroupID: r.GroupID, RouteID: r.ID})
		return nil
	})
}

// UpdateRoute replaces an existing route.
// Returns model.ErrNotFound if the group or route does not exist.
// Returns model.ErrDuplicateRoute if the updated MatchRule conflicts with another route.
func (s *Store) UpdateRoute(_ context.Context, r model.Route) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		gb := tx.Bucket(bucketGroups)
		rb := tx.Bucket(bucketRoutes)

		if gb.Get([]byte(r.GroupID)) == nil {
			return fmt.Errorf("group %q: %w", r.GroupID, model.ErrNotFound)
		}
		if rb.Get([]byte(r.ID)) == nil {
			return fmt.Errorf("route %q: %w", r.ID, model.ErrNotFound)
		}
		if err := checkDuplicate(rb, r, r.ID); err != nil {
			return err
		}

		data, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if err := rb.Put([]byte(r.ID), data); err != nil {
			return err
		}
		s.publish(store.StoreEvent{Type: store.EventUpdated, Resource: store.ResourceRoute, GroupID: r.GroupID, RouteID: r.ID})
		return nil
	})
}

// DeleteRoute removes a route from its group.
// Returns model.ErrNotFound if the group or route does not exist.
func (s *Store) DeleteRoute(_ context.Context, groupID, routeID string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		gb := tx.Bucket(bucketGroups)
		rb := tx.Bucket(bucketRoutes)

		if gb.Get([]byte(groupID)) == nil {
			return fmt.Errorf("group %q: %w", groupID, model.ErrNotFound)
		}
		v := rb.Get([]byte(routeID))
		if v == nil {
			return fmt.Errorf("route %q in group %q: %w", routeID, groupID, model.ErrNotFound)
		}
		var r model.Route
		if err := json.Unmarshal(v, &r); err != nil {
			return err
		}
		if r.GroupID != groupID {
			return fmt.Errorf("route %q in group %q: %w", routeID, groupID, model.ErrNotFound)
		}
		if err := rb.Delete([]byte(routeID)); err != nil {
			return err
		}
		s.publish(store.StoreEvent{Type: store.EventDeleted, Resource: store.ResourceRoute, GroupID: groupID, RouteID: routeID})
		return nil
	})
}

// --- Subscriptions ---

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

// --- Helpers ---

// publish sends an event to all current subscribers in a non-blocking manner.
func (s *Store) publish(ev store.StoreEvent) {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	for _, ch := range s.subs {
		select {
		case ch <- ev:
		default:
			// Subscriber too slow; drop rather than block.
		}
	}
}

// checkDuplicate returns model.ErrDuplicateRoute if any route in the same group
// shares the same MatchRule as r. Pass skipID to exclude the route being updated.
func checkDuplicate(rb *bolt.Bucket, r model.Route, skipID string) error {
	return rb.ForEach(func(k, v []byte) error {
		if string(k) == skipID {
			return nil
		}
		var existing model.Route
		if err := json.Unmarshal(v, &existing); err != nil {
			return err
		}
		if existing.GroupID != r.GroupID {
			return nil
		}
		if matchRulesEqual(existing.Match, r.Match) {
			return fmt.Errorf("route with same match rule: %w", model.ErrDuplicateRoute)
		}
		return nil
	})
}

// matchRulesEqual returns true when two MatchRules have the same path specifier.
func matchRulesEqual(a, b model.MatchRule) bool {
	return a.Path == b.Path &&
		a.PathPrefix == b.PathPrefix &&
		a.PathRegex == b.PathRegex
}
