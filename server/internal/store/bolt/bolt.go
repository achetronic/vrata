// Package bolt provides a persistent bbolt-backed implementation of store.Store.
// Routes and groups are stored in separate top-level buckets. Each entity is
// serialised as JSON with the entity ID as the key.
package bolt

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/achetronic/rutoso/internal/model"
	"github.com/achetronic/rutoso/internal/store"
	bolt "go.etcd.io/bbolt"
)

const (
	bucketRoutes = "routes"
	bucketGroups = "groups"
)

// Store wraps a bbolt database and exposes CRUD operations for routes and groups.
// It implements store.Store and is safe for concurrent use.
type Store struct {
	db *bolt.DB

	subsMu sync.Mutex
	subs   []chan store.StoreEvent
}

// New opens (or creates) the bbolt database at the given path and initialises
// the required buckets. It returns an error if the database cannot be opened.
func New(path string) (*Store, error) {
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		return nil, fmt.Errorf("opening bolt db: %w", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		for _, name := range []string{bucketRoutes, bucketGroups} {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return fmt.Errorf("creating bucket %q: %w", name, err)
			}
		}
		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialising buckets: %w", err)
	}

	return &Store{db: db}, nil
}

// Close releases the database file handle. Call via defer in main.
func (s *Store) Close() error {
	return s.db.Close()
}

// ────────────────────────────────────────────────────────────────────────────
// Route operations
// ────────────────────────────────────────────────────────────────────────────

// ListRoutes returns all routes stored in the database.
func (s *Store) ListRoutes(_ context.Context) ([]model.Route, error) {
	var routes []model.Route

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketRoutes))
		return b.ForEach(func(_, v []byte) error {
			var r model.Route
			if err := json.Unmarshal(v, &r); err != nil {
				return fmt.Errorf("unmarshalling route: %w", err)
			}
			routes = append(routes, r)
			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("listing routes: %w", err)
	}

	if routes == nil {
		routes = []model.Route{}
	}
	return routes, nil
}

// GetRoute returns the route with the given ID, or model.ErrNotFound if absent.
func (s *Store) GetRoute(_ context.Context, id string) (model.Route, error) {
	var route model.Route

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketRoutes))
		v := b.Get([]byte(id))
		if v == nil {
			return fmt.Errorf("route %q: %w", id, model.ErrNotFound)
		}
		return json.Unmarshal(v, &route)
	})
	if err != nil {
		return model.Route{}, err
	}
	return route, nil
}

// SaveRoute creates or replaces the route with route.ID as key.
func (s *Store) SaveRoute(_ context.Context, route model.Route) error {
	data, err := json.Marshal(route)
	if err != nil {
		return fmt.Errorf("marshalling route: %w", err)
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketRoutes))
		if err := b.Put([]byte(route.ID), data); err != nil {
			return fmt.Errorf("saving route %q: %w", route.ID, err)
		}
		s.publish(store.StoreEvent{Type: store.EventCreated, Resource: store.ResourceRoute, ID: route.ID})
		return nil
	})
}

// DeleteRoute removes the route with the given ID.
func (s *Store) DeleteRoute(_ context.Context, id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketRoutes))
		if b.Get([]byte(id)) == nil {
			return fmt.Errorf("route %q: %w", id, model.ErrNotFound)
		}
		if err := b.Delete([]byte(id)); err != nil {
			return fmt.Errorf("deleting route %q: %w", id, err)
		}
		s.publish(store.StoreEvent{Type: store.EventDeleted, Resource: store.ResourceRoute, ID: id})
		return nil
	})
}

// ────────────────────────────────────────────────────────────────────────────
// Group operations
// ────────────────────────────────────────────────────────────────────────────

// ListGroups returns all groups stored in the database.
func (s *Store) ListGroups(_ context.Context) ([]model.RouteGroup, error) {
	var groups []model.RouteGroup

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketGroups))
		return b.ForEach(func(_, v []byte) error {
			var g model.RouteGroup
			if err := json.Unmarshal(v, &g); err != nil {
				return fmt.Errorf("unmarshalling group: %w", err)
			}
			groups = append(groups, g)
			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("listing groups: %w", err)
	}

	if groups == nil {
		groups = []model.RouteGroup{}
	}
	return groups, nil
}

// GetGroup returns the group with the given ID, or model.ErrNotFound if absent.
func (s *Store) GetGroup(_ context.Context, id string) (model.RouteGroup, error) {
	var group model.RouteGroup

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketGroups))
		v := b.Get([]byte(id))
		if v == nil {
			return fmt.Errorf("group %q: %w", id, model.ErrNotFound)
		}
		return json.Unmarshal(v, &group)
	})
	if err != nil {
		return model.RouteGroup{}, err
	}
	return group, nil
}

// SaveGroup creates or replaces the group with group.ID as key.
func (s *Store) SaveGroup(_ context.Context, group model.RouteGroup) error {
	data, err := json.Marshal(group)
	if err != nil {
		return fmt.Errorf("marshalling group: %w", err)
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketGroups))
		if err := b.Put([]byte(group.ID), data); err != nil {
			return fmt.Errorf("saving group %q: %w", group.ID, err)
		}
		s.publish(store.StoreEvent{Type: store.EventCreated, Resource: store.ResourceGroup, ID: group.ID})
		return nil
	})
}

// DeleteGroup removes the group with the given ID.
func (s *Store) DeleteGroup(_ context.Context, id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketGroups))
		if b.Get([]byte(id)) == nil {
			return fmt.Errorf("group %q: %w", id, model.ErrNotFound)
		}
		if err := b.Delete([]byte(id)); err != nil {
			return fmt.Errorf("deleting group %q: %w", id, err)
		}
		s.publish(store.StoreEvent{Type: store.EventDeleted, Resource: store.ResourceGroup, ID: id})
		return nil
	})
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
// Must NOT be called while holding s.subsMu.
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
