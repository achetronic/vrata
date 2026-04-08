// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package bolt provides a persistent bbolt-backed implementation of store.Store.
// Routes and groups are stored in separate top-level buckets. Each entity is
// serialised as JSON with the entity ID as the key.
package bolt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/achetronic/vrata/internal/encrypt"
	"github.com/achetronic/vrata/internal/model"
	"github.com/achetronic/vrata/internal/store"
	bolt "go.etcd.io/bbolt"
)

const (
	bucketRoutes       = "routes"
	bucketGroups       = "groups"
	bucketMiddlewares  = "middlewares"
	bucketListeners    = "listeners"
	bucketDestinations = "destinations"
	bucketSecrets      = "secrets"
	bucketSnapshots    = "snapshots"
	bucketMeta         = "meta"

	metaActiveSnapshot = "active_snapshot_id"
	metaEncrypted      = "encrypted"
)

// Store wraps a bbolt database and exposes CRUD operations for all entities.
// It implements store.Store and is safe for concurrent use.
type Store struct {
	db     *bolt.DB
	cipher *encrypt.Cipher

	subsMu sync.Mutex
	subs   []chan store.StoreEvent
}

var _ store.Store = (*Store)(nil)

// New opens (or creates) the bbolt database at the given path and initialises
// the required buckets. The optional cipher enables at-rest encryption for
// secrets and snapshots. Pass nil for plaintext (dev mode).
// Returns an error if the encryption mode does not match the stored data.
func New(path string, cipher *encrypt.Cipher) (*Store, error) {
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		return nil, fmt.Errorf("opening bolt db: %w", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		for _, name := range []string{bucketRoutes, bucketGroups, bucketMiddlewares, bucketListeners, bucketDestinations, bucketSecrets, bucketSnapshots, bucketMeta} {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return fmt.Errorf("creating bucket %q: %w", name, err)
			}
		}
		return nil
	})
	if err != nil {
		_ = db.Close() // Best-effort cleanup on bucket init failure
		return nil, fmt.Errorf("initialising buckets: %w", err)
	}

	st := &Store{db: db, cipher: cipher}
	if err := st.checkEncryptionMode(); err != nil {
		_ = db.Close() // Best-effort cleanup on encryption check failure
		return nil, err
	}

	return st, nil
}

// Close releases the database file handle. Call via defer in main.
func (s *Store) Close() error {
	return s.db.Close()
}

// checkEncryptionMode verifies that the configured encryption mode matches
// the data in bbolt. If there is a mismatch, it returns an error.
func (s *Store) checkEncryptionMode() error {
	var marker []byte
	if err := s.db.View(func(tx *bolt.Tx) error {
		marker = tx.Bucket([]byte(bucketMeta)).Get([]byte(metaEncrypted))
		return nil
	}); err != nil {
		return fmt.Errorf("reading encryption marker: %w", err)
	}

	dataEncrypted := len(marker) > 0 && string(marker) == "true"

	if s.cipher == nil && dataEncrypted {
		return fmt.Errorf("data is encrypted but no encryption key is configured: set controlPlane.encryption.key")
	}
	if s.cipher != nil && !dataEncrypted {
		hasData := false
		if err := s.db.View(func(tx *bolt.Tx) error {
			for _, name := range []string{bucketSecrets, bucketSnapshots} {
				b := tx.Bucket([]byte(name))
				if b != nil && b.Stats().KeyN > 0 {
					hasData = true
				}
			}
			return nil
		}); err != nil {
			return fmt.Errorf("checking existing data: %w", err)
		}
		if hasData {
			return fmt.Errorf("data is not encrypted but an encryption key is configured: remove controlPlane.encryption.key or start with a fresh database")
		}
		return s.db.Update(func(tx *bolt.Tx) error {
			return tx.Bucket([]byte(bucketMeta)).Put([]byte(metaEncrypted), []byte("true"))
		})
	}
	return nil
}

// encryptValue encrypts data if a cipher is configured. Returns plaintext as-is otherwise.
func (s *Store) encryptValue(data []byte) ([]byte, error) {
	if s.cipher == nil {
		return data, nil
	}
	return s.cipher.Seal(data)
}

// decryptValue decrypts data if a cipher is configured. Returns data as-is otherwise.
func (s *Store) decryptValue(data []byte) ([]byte, error) {
	if s.cipher == nil {
		return data, nil
	}
	return s.cipher.Open(data)
}

// ────────────────────────────────────────────────────────────────────────────
// Route operations
// ────────────────────────────────────────────────────────────────────────────

// ListRoutes returns all routes stored in the database.
func (s *Store) ListRoutes(_ context.Context) ([]model.Route, error) {
	var routes []model.Route

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketRoutes))
		return b.ForEach(func(k, v []byte) error {
			var r model.Route
			if err := json.Unmarshal(v, &r); err != nil {
				slog.Error("store: skipping corrupt route", slog.String("key", string(k)), slog.String("error", err.Error()))
				return nil
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
		if !errors.Is(err, model.ErrNotFound) {
			err = fmt.Errorf("unmarshalling route %q: %w", id, err)
		}
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

	var isUpdate bool
	err = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketRoutes))
		if b.Get([]byte(route.ID)) != nil {
			isUpdate = true
		}
		if err := b.Put([]byte(route.ID), data); err != nil {
			return fmt.Errorf("saving route %q: %w", route.ID, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	evt := store.EventCreated
	if isUpdate {
		evt = store.EventUpdated
	}
	s.publish(store.StoreEvent{Type: evt, Resource: store.ResourceRoute, ID: route.ID})
	return nil
}

// DeleteRoute removes the route with the given ID.
func (s *Store) DeleteRoute(_ context.Context, id string) error {
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketRoutes))
		if b.Get([]byte(id)) == nil {
			return fmt.Errorf("route %q: %w", id, model.ErrNotFound)
		}
		if err := b.Delete([]byte(id)); err != nil {
			return fmt.Errorf("deleting route %q: %w", id, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(store.StoreEvent{Type: store.EventDeleted, Resource: store.ResourceRoute, ID: id})
	return nil
}

// ────────────────────────────────────────────────────────────────────────────
// Group operations
// ────────────────────────────────────────────────────────────────────────────

// ListGroups returns all groups stored in the database.
func (s *Store) ListGroups(_ context.Context) ([]model.RouteGroup, error) {
	var groups []model.RouteGroup

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketGroups))
		return b.ForEach(func(k, v []byte) error {
			var g model.RouteGroup
			if err := json.Unmarshal(v, &g); err != nil {
				slog.Error("store: skipping corrupt group", slog.String("key", string(k)), slog.String("error", err.Error()))
				return nil
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
		if !errors.Is(err, model.ErrNotFound) {
			err = fmt.Errorf("unmarshalling group %q: %w", id, err)
		}
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

	var isUpdate bool
	err = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketGroups))
		if b.Get([]byte(group.ID)) != nil {
			isUpdate = true
		}
		if err := b.Put([]byte(group.ID), data); err != nil {
			return fmt.Errorf("saving group %q: %w", group.ID, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	evt := store.EventCreated
	if isUpdate {
		evt = store.EventUpdated
	}
	s.publish(store.StoreEvent{Type: evt, Resource: store.ResourceGroup, ID: group.ID})
	return nil
}

// DeleteGroup removes the group with the given ID.
func (s *Store) DeleteGroup(_ context.Context, id string) error {
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketGroups))
		if b.Get([]byte(id)) == nil {
			return fmt.Errorf("group %q: %w", id, model.ErrNotFound)
		}
		if err := b.Delete([]byte(id)); err != nil {
			return fmt.Errorf("deleting group %q: %w", id, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(store.StoreEvent{Type: store.EventDeleted, Resource: store.ResourceGroup, ID: id})
	return nil
}

// ────────────────────────────────────────────────────────────────────────────
// Filter operations
// ────────────────────────────────────────────────────────────────────────────

// ListMiddlewares returns all filters stored in the database.
func (s *Store) ListMiddlewares(_ context.Context) ([]model.Middleware, error) {
	var filters []model.Middleware

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketMiddlewares))
		return b.ForEach(func(k, v []byte) error {
			var f model.Middleware
			if err := json.Unmarshal(v, &f); err != nil {
				slog.Error("store: skipping corrupt middleware", slog.String("key", string(k)), slog.String("error", err.Error()))
				return nil
			}
			filters = append(filters, f)
			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("listing filters: %w", err)
	}

	if filters == nil {
		filters = []model.Middleware{}
	}
	return filters, nil
}

// GetMiddleware returns the filter with the given ID, or model.ErrNotFound if absent.
func (s *Store) GetMiddleware(_ context.Context, id string) (model.Middleware, error) {
	var filter model.Middleware

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketMiddlewares))
		v := b.Get([]byte(id))
		if v == nil {
			return fmt.Errorf("filter %q: %w", id, model.ErrNotFound)
		}
		return json.Unmarshal(v, &filter)
	})
	if err != nil {
		if !errors.Is(err, model.ErrNotFound) {
			err = fmt.Errorf("unmarshalling middleware %q: %w", id, err)
		}
		return model.Middleware{}, err
	}
	return filter, nil
}

// SaveMiddleware creates or replaces the filter with filter.ID as key.
func (s *Store) SaveMiddleware(_ context.Context, filter model.Middleware) error {
	data, err := json.Marshal(filter)
	if err != nil {
		return fmt.Errorf("marshalling filter: %w", err)
	}

	var isUpdate bool
	err = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketMiddlewares))
		if b.Get([]byte(filter.ID)) != nil {
			isUpdate = true
		}
		if err := b.Put([]byte(filter.ID), data); err != nil {
			return fmt.Errorf("saving filter %q: %w", filter.ID, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	evt := store.EventCreated
	if isUpdate {
		evt = store.EventUpdated
	}
	s.publish(store.StoreEvent{Type: evt, Resource: store.ResourceMiddleware, ID: filter.ID})
	return nil
}

// DeleteMiddleware removes the filter with the given ID.
func (s *Store) DeleteMiddleware(_ context.Context, id string) error {
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketMiddlewares))
		if b.Get([]byte(id)) == nil {
			return fmt.Errorf("filter %q: %w", id, model.ErrNotFound)
		}
		if err := b.Delete([]byte(id)); err != nil {
			return fmt.Errorf("deleting filter %q: %w", id, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(store.StoreEvent{Type: store.EventDeleted, Resource: store.ResourceMiddleware, ID: id})
	return nil
}

// ────────────────────────────────────────────────────────────────────────────
// Listener operations
// ────────────────────────────────────────────────────────────────────────────

// ListListeners returns all listeners stored in the database.
func (s *Store) ListListeners(_ context.Context) ([]model.Listener, error) {
	var listeners []model.Listener

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketListeners))
		return b.ForEach(func(k, v []byte) error {
			var l model.Listener
			if err := json.Unmarshal(v, &l); err != nil {
				slog.Error("store: skipping corrupt listener", slog.String("key", string(k)), slog.String("error", err.Error()))
				return nil
			}
			listeners = append(listeners, l)
			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("listing listeners: %w", err)
	}

	if listeners == nil {
		listeners = []model.Listener{}
	}
	return listeners, nil
}

// GetListener returns the listener with the given ID, or model.ErrNotFound if absent.
func (s *Store) GetListener(_ context.Context, id string) (model.Listener, error) {
	var listener model.Listener

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketListeners))
		v := b.Get([]byte(id))
		if v == nil {
			return fmt.Errorf("listener %q: %w", id, model.ErrNotFound)
		}
		return json.Unmarshal(v, &listener)
	})
	if err != nil {
		if !errors.Is(err, model.ErrNotFound) {
			err = fmt.Errorf("unmarshalling listener %q: %w", id, err)
		}
		return model.Listener{}, err
	}
	return listener, nil
}

// SaveListener creates or replaces the listener with listener.ID as key.
func (s *Store) SaveListener(_ context.Context, listener model.Listener) error {
	data, err := json.Marshal(listener)
	if err != nil {
		return fmt.Errorf("marshalling listener: %w", err)
	}

	var isUpdate bool
	err = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketListeners))
		if b.Get([]byte(listener.ID)) != nil {
			isUpdate = true
		}
		if err := b.Put([]byte(listener.ID), data); err != nil {
			return fmt.Errorf("saving listener %q: %w", listener.ID, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	evt := store.EventCreated
	if isUpdate {
		evt = store.EventUpdated
	}
	s.publish(store.StoreEvent{Type: evt, Resource: store.ResourceListener, ID: listener.ID})
	return nil
}

// DeleteListener removes the listener with the given ID.
func (s *Store) DeleteListener(_ context.Context, id string) error {
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketListeners))
		if b.Get([]byte(id)) == nil {
			return fmt.Errorf("listener %q: %w", id, model.ErrNotFound)
		}
		if err := b.Delete([]byte(id)); err != nil {
			return fmt.Errorf("deleting listener %q: %w", id, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(store.StoreEvent{Type: store.EventDeleted, Resource: store.ResourceListener, ID: id})
	return nil
}

// ────────────────────────────────────────────────────────────────────────────
// Destination operations
// ────────────────────────────────────────────────────────────────────────────

// ListDestinations returns all destinations stored in the database.
func (s *Store) ListDestinations(_ context.Context) ([]model.Destination, error) {
	var destinations []model.Destination

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketDestinations))
		return b.ForEach(func(k, v []byte) error {
			var d model.Destination
			if err := json.Unmarshal(v, &d); err != nil {
				slog.Error("store: skipping corrupt destination", slog.String("key", string(k)), slog.String("error", err.Error()))
				return nil
			}
			destinations = append(destinations, d)
			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("listing destinations: %w", err)
	}

	if destinations == nil {
		destinations = []model.Destination{}
	}
	return destinations, nil
}

// GetDestination returns the destination with the given ID, or model.ErrNotFound if absent.
func (s *Store) GetDestination(_ context.Context, id string) (model.Destination, error) {
	var destination model.Destination

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketDestinations))
		v := b.Get([]byte(id))
		if v == nil {
			return fmt.Errorf("destination %q: %w", id, model.ErrNotFound)
		}
		return json.Unmarshal(v, &destination)
	})
	if err != nil {
		if !errors.Is(err, model.ErrNotFound) {
			err = fmt.Errorf("unmarshalling destination %q: %w", id, err)
		}
		return model.Destination{}, err
	}
	return destination, nil
}

// SaveDestination creates or replaces the destination with d.ID as key.
func (s *Store) SaveDestination(_ context.Context, d model.Destination) error {
	data, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("marshalling destination: %w", err)
	}

	var isUpdate bool
	err = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketDestinations))
		if b.Get([]byte(d.ID)) != nil {
			isUpdate = true
		}
		if err := b.Put([]byte(d.ID), data); err != nil {
			return fmt.Errorf("saving destination %q: %w", d.ID, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	evt := store.EventCreated
	if isUpdate {
		evt = store.EventUpdated
	}
	s.publish(store.StoreEvent{Type: evt, Resource: store.ResourceDestination, ID: d.ID})
	return nil
}

// DeleteDestination removes the destination with the given ID.
func (s *Store) DeleteDestination(_ context.Context, id string) error {
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketDestinations))
		if b.Get([]byte(id)) == nil {
			return fmt.Errorf("destination %q: %w", id, model.ErrNotFound)
		}
		if err := b.Delete([]byte(id)); err != nil {
			return fmt.Errorf("deleting destination %q: %w", id, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(store.StoreEvent{Type: store.EventDeleted, Resource: store.ResourceDestination, ID: id})
	return nil
}

// ────────────────────────────────────────────────────────────────────────────
// Secret operations
// ────────────────────────────────────────────────────────────────────────────

// ListSecrets returns summary metadata (ID + Name) for all secrets.
func (s *Store) ListSecrets(_ context.Context) ([]model.SecretSummary, error) {
	var summaries []model.SecretSummary
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketSecrets))
		return b.ForEach(func(k, v []byte) error {
			plain, err := s.decryptValue(v)
			if err != nil {
				slog.Error("store: skipping corrupt secret", slog.String("key", string(k)), slog.String("error", err.Error()))
				return nil
			}
			var sec model.Secret
			if err := json.Unmarshal(plain, &sec); err != nil {
				slog.Error("store: skipping corrupt secret", slog.String("key", string(k)), slog.String("error", err.Error()))
				return nil
			}
			summaries = append(summaries, model.SecretSummary{ID: sec.ID, Name: sec.Name})
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if summaries == nil {
		summaries = []model.SecretSummary{}
	}
	return summaries, nil
}

// GetSecret returns the secret with the given ID, including its Value.
func (s *Store) GetSecret(_ context.Context, id string) (model.Secret, error) {
	var sec model.Secret
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketSecrets))
		data := b.Get([]byte(id))
		if data == nil {
			return fmt.Errorf("secret %q: %w", id, model.ErrNotFound)
		}
		plain, err := s.decryptValue(data)
		if err != nil {
			return fmt.Errorf("decrypting secret %q: %w", id, err)
		}
		return json.Unmarshal(plain, &sec)
	})
	if err != nil {
		if !errors.Is(err, model.ErrNotFound) {
			err = fmt.Errorf("unmarshalling secret %q: %w", id, err)
		}
		return model.Secret{}, err
	}
	return sec, nil
}

// SaveSecret creates or replaces the secret identified by s.ID.
func (s *Store) SaveSecret(_ context.Context, sec model.Secret) error {
	data, err := json.Marshal(sec)
	if err != nil {
		return fmt.Errorf("encoding secret: %w", err)
	}
	encrypted, err := s.encryptValue(data)
	if err != nil {
		return fmt.Errorf("encrypting secret: %w", err)
	}
	isUpdate := false
	err = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketSecrets))
		isUpdate = b.Get([]byte(sec.ID)) != nil
		return b.Put([]byte(sec.ID), encrypted)
	})
	if err != nil {
		return err
	}
	evt := store.EventCreated
	if isUpdate {
		evt = store.EventUpdated
	}
	s.publish(store.StoreEvent{Type: evt, Resource: store.ResourceSecret, ID: sec.ID})
	return nil
}

// DeleteSecret removes the secret with the given ID.
func (s *Store) DeleteSecret(_ context.Context, id string) error {
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketSecrets))
		if b.Get([]byte(id)) == nil {
			return fmt.Errorf("secret %q: %w", id, model.ErrNotFound)
		}
		if err := b.Delete([]byte(id)); err != nil {
			return fmt.Errorf("deleting secret %q: %w", id, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(store.StoreEvent{Type: store.EventDeleted, Resource: store.ResourceSecret, ID: id})
	return nil
}

// ────────────────────────────────────────────────────────────────────────────
// Snapshot operations
// ────────────────────────────────────────────────────────────────────────────

// ListSnapshots returns summary metadata for all versioned snapshots.
func (s *Store) ListSnapshots(_ context.Context) ([]model.SnapshotSummary, error) {
	var activeID string
	var summaries []model.SnapshotSummary

	err := s.db.View(func(tx *bolt.Tx) error {
		meta := tx.Bucket([]byte(bucketMeta))
		if v := meta.Get([]byte(metaActiveSnapshot)); v != nil {
			activeID = string(v)
		}

		b := tx.Bucket([]byte(bucketSnapshots))
		return b.ForEach(func(k, v []byte) error {
			plain, err := s.decryptValue(v)
			if err != nil {
				slog.Error("store: skipping corrupt snapshot", slog.String("key", string(k)), slog.String("error", err.Error()))
				return nil
			}
			var vs model.VersionedSnapshot
			if err := json.Unmarshal(plain, &vs); err != nil {
				slog.Error("store: skipping corrupt snapshot", slog.String("key", string(k)), slog.String("error", err.Error()))
				return nil
			}
			summaries = append(summaries, model.SnapshotSummary{
				ID:        vs.ID,
				Name:      vs.Name,
				CreatedAt: vs.CreatedAt,
				Active:    vs.ID == activeID,
			})
			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("listing snapshots: %w", err)
	}

	if summaries == nil {
		summaries = []model.SnapshotSummary{}
	}
	return summaries, nil
}

// GetSnapshot returns the versioned snapshot with the given ID.
func (s *Store) GetSnapshot(_ context.Context, id string) (model.VersionedSnapshot, error) {
	var vs model.VersionedSnapshot

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketSnapshots))
		v := b.Get([]byte(id))
		if v == nil {
			return fmt.Errorf("snapshot %q: %w", id, model.ErrNotFound)
		}
		plain, err := s.decryptValue(v)
		if err != nil {
			return fmt.Errorf("decrypting snapshot %q: %w", id, err)
		}
		return json.Unmarshal(plain, &vs)
	})
	if err != nil {
		if !errors.Is(err, model.ErrNotFound) {
			err = fmt.Errorf("unmarshalling snapshot %q: %w", id, err)
		}
		return model.VersionedSnapshot{}, err
	}
	return vs, nil
}

// SaveSnapshot creates or replaces a versioned snapshot.
func (s *Store) SaveSnapshot(_ context.Context, vs model.VersionedSnapshot) error {
	data, err := json.Marshal(vs)
	if err != nil {
		return fmt.Errorf("marshalling snapshot: %w", err)
	}
	encrypted, err := s.encryptValue(data)
	if err != nil {
		return fmt.Errorf("encrypting snapshot: %w", err)
	}

	var isUpdate bool
	err = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketSnapshots))
		isUpdate = b.Get([]byte(vs.ID)) != nil
		if err := b.Put([]byte(vs.ID), encrypted); err != nil {
			return fmt.Errorf("saving snapshot %q: %w", vs.ID, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	evType := store.EventCreated
	if isUpdate {
		evType = store.EventUpdated
	}
	s.publish(store.StoreEvent{Type: evType, Resource: store.ResourceSnapshot, ID: vs.ID})
	return nil
}

// DeleteSnapshot removes the versioned snapshot with the given ID.
// If the deleted snapshot was the active one, the active pointer is cleared.
func (s *Store) DeleteSnapshot(_ context.Context, id string) error {
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketSnapshots))
		if b.Get([]byte(id)) == nil {
			return fmt.Errorf("snapshot %q: %w", id, model.ErrNotFound)
		}
		if err := b.Delete([]byte(id)); err != nil {
			return fmt.Errorf("deleting snapshot %q: %w", id, err)
		}
		meta := tx.Bucket([]byte(bucketMeta))
		if v := meta.Get([]byte(metaActiveSnapshot)); v != nil && string(v) == id {
			if err := meta.Delete([]byte(metaActiveSnapshot)); err != nil {
				return fmt.Errorf("clearing active snapshot: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(store.StoreEvent{Type: store.EventDeleted, Resource: store.ResourceSnapshot, ID: id})
	return nil
}

// ActivateSnapshot sets the given snapshot ID as the active configuration.
func (s *Store) ActivateSnapshot(_ context.Context, id string) error {
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketSnapshots))
		if b.Get([]byte(id)) == nil {
			return fmt.Errorf("snapshot %q: %w", id, model.ErrNotFound)
		}
		meta := tx.Bucket([]byte(bucketMeta))
		return meta.Put([]byte(metaActiveSnapshot), []byte(id))
	})
	if err != nil {
		return err
	}
	s.publish(store.StoreEvent{Type: store.EventUpdated, Resource: store.ResourceSnapshot, ID: id})
	return nil
}

// GetActiveSnapshot returns the currently active versioned snapshot.
func (s *Store) GetActiveSnapshot(_ context.Context) (model.VersionedSnapshot, error) {
	var vs model.VersionedSnapshot

	err := s.db.View(func(tx *bolt.Tx) error {
		meta := tx.Bucket([]byte(bucketMeta))
		activeID := meta.Get([]byte(metaActiveSnapshot))
		if activeID == nil {
			return model.ErrNoActiveSnapshot
		}
		b := tx.Bucket([]byte(bucketSnapshots))
		v := b.Get(activeID)
		if v == nil {
			return model.ErrNoActiveSnapshot
		}
		plain, err := s.decryptValue(v)
		if err != nil {
			return fmt.Errorf("decrypting active snapshot: %w", err)
		}
		return json.Unmarshal(plain, &vs)
	})
	if err != nil {
		if !errors.Is(err, model.ErrNoActiveSnapshot) {
			err = fmt.Errorf("unmarshalling active snapshot: %w", err)
		}
		return model.VersionedSnapshot{}, err
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
// Must NOT be called while holding s.subsMu.
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

// ────────────────────────────────────────────────────────────────────────────
// Raft FSM support
// ────────────────────────────────────────────────────────────────────────────

// ApplyCommand applies a single replicated command to the store.
// Called by the Raft FSM on every committed log entry.
func (s *Store) ApplyCommand(cmdType string, id string, payload json.RawMessage) error {
	ctx := context.Background()

	switch cmdType {
	case "SaveRoute":
		var v model.Route
		if err := json.Unmarshal(payload, &v); err != nil {
			return fmt.Errorf("ApplyCommand SaveRoute: %w", err)
		}
		return s.SaveRoute(ctx, v)
	case "DeleteRoute":
		return s.DeleteRoute(ctx, id)
	case "SaveGroup":
		var v model.RouteGroup
		if err := json.Unmarshal(payload, &v); err != nil {
			return fmt.Errorf("ApplyCommand SaveGroup: %w", err)
		}
		return s.SaveGroup(ctx, v)
	case "DeleteGroup":
		return s.DeleteGroup(ctx, id)
	case "SaveMiddleware":
		var v model.Middleware
		if err := json.Unmarshal(payload, &v); err != nil {
			return fmt.Errorf("ApplyCommand SaveMiddleware: %w", err)
		}
		return s.SaveMiddleware(ctx, v)
	case "DeleteMiddleware":
		return s.DeleteMiddleware(ctx, id)
	case "SaveListener":
		var v model.Listener
		if err := json.Unmarshal(payload, &v); err != nil {
			return fmt.Errorf("ApplyCommand SaveListener: %w", err)
		}
		return s.SaveListener(ctx, v)
	case "DeleteListener":
		return s.DeleteListener(ctx, id)
	case "SaveDestination":
		var v model.Destination
		if err := json.Unmarshal(payload, &v); err != nil {
			return fmt.Errorf("ApplyCommand SaveDestination: %w", err)
		}
		return s.SaveDestination(ctx, v)
	case "DeleteDestination":
		return s.DeleteDestination(ctx, id)
	case "SaveSecret":
		var v model.Secret
		if err := json.Unmarshal(payload, &v); err != nil {
			return fmt.Errorf("ApplyCommand SaveSecret: %w", err)
		}
		return s.SaveSecret(ctx, v)
	case "DeleteSecret":
		return s.DeleteSecret(ctx, id)
	case "SaveSnapshot":
		var v model.VersionedSnapshot
		if err := json.Unmarshal(payload, &v); err != nil {
			return fmt.Errorf("ApplyCommand SaveSnapshot: %w", err)
		}
		return s.SaveSnapshot(ctx, v)
	case "DeleteSnapshot":
		return s.DeleteSnapshot(ctx, id)
	case "ActivateSnapshot":
		return s.ActivateSnapshot(ctx, id)
	default:
		return fmt.Errorf("unknown command type: %s", cmdType)
	}
}

// Dump serialises the full database as a JSON map of bucket → key → value.
// Used by the Raft FSM to create log compaction snapshots.
func (s *Store) Dump() ([]byte, error) {
	dump := make(map[string]map[string]json.RawMessage)

	err := s.db.View(func(tx *bolt.Tx) error {
		for _, name := range []string{
			bucketRoutes, bucketGroups, bucketMiddlewares, bucketListeners,
			bucketDestinations, bucketSecrets, bucketSnapshots, bucketMeta,
		} {
			b := tx.Bucket([]byte(name))
			if b == nil {
				continue
			}
			entries := make(map[string]json.RawMessage)
			if err := b.ForEach(func(k, v []byte) error {
				key := string(k)
				val := make([]byte, len(v))
				copy(val, v)
				entries[key] = json.RawMessage(val)
				return nil
			}); err != nil {
				return fmt.Errorf("iterating bucket %q: %w", name, err)
			}
			dump[name] = entries
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("dumping bolt db: %w", err)
	}

	return json.Marshal(dump)
}

// Restore replaces the full database with the contents of a Dump.
// Used by the Raft FSM to restore a node from a log compaction snapshot.
func (s *Store) Restore(data []byte) error {
	var dump map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &dump); err != nil {
		return fmt.Errorf("decoding snapshot: %w", err)
	}

	dataBuckets := []string{bucketRoutes, bucketGroups, bucketMiddlewares, bucketListeners, bucketDestinations, bucketSecrets, bucketSnapshots, bucketMeta}

	err := s.db.Update(func(tx *bolt.Tx) error {
		for _, bucketName := range dataBuckets {
			b := tx.Bucket([]byte(bucketName))
			if b == nil {
				var err error
				b, err = tx.CreateBucketIfNotExists([]byte(bucketName))
				if err != nil {
					return err
				}
			}
			// Clear existing data — collect keys first to avoid
			// deleting during ForEach iteration (undefined in bbolt).
			var keysToDelete [][]byte
			if err := b.ForEach(func(k, _ []byte) error {
				keysToDelete = append(keysToDelete, append([]byte(nil), k...))
				return nil
			}); err != nil {
				return fmt.Errorf("collecting keys in bucket %q: %w", bucketName, err)
			}
			for _, k := range keysToDelete {
				if err := b.Delete(k); err != nil {
					return err
				}
			}
			// Write new data (if this bucket is present in the dump).
			entries, ok := dump[bucketName]
			if !ok {
				continue
			}
			for k, v := range entries {
				if err := b.Put([]byte(k), v); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(store.StoreEvent{Type: store.EventUpdated, Resource: store.ResourceSnapshot, ID: "restore"})
	return nil
}
