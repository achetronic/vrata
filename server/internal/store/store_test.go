// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package store_test provides a shared test suite that exercises the
// store.Store interface against every implementation (bolt, memory).
// Adding a new implementation only requires adding one line to TestMain.
package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/achetronic/vrata/internal/model"
	"github.com/achetronic/vrata/internal/store"
	boltstore "github.com/achetronic/vrata/internal/store/bolt"
	memstore "github.com/achetronic/vrata/internal/store/memory"
)

type storeFactory func(t *testing.T) store.Store

func factories() map[string]storeFactory {
	return map[string]storeFactory{
		"memory": func(t *testing.T) store.Store {
			return memstore.New()
		},
		"bolt": func(t *testing.T) store.Store {
			dir := t.TempDir()
			s, err := boltstore.New(filepath.Join(dir, "test.db"), nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.Close() })
			return s
		},
	}
}

func runForAll(t *testing.T, name string, fn func(t *testing.T, s store.Store)) {
	for implName, factory := range factories() {
		t.Run(implName+"/"+name, func(t *testing.T) {
			fn(t, factory(t))
		})
	}
}

// ─── Routes ─────────────────────────────────────────────────────────────────

func TestRouteCRUD(t *testing.T) {
	runForAll(t, "CRUD", func(t *testing.T, s store.Store) {
		ctx := context.Background()

		routes, err := s.ListRoutes(ctx)
		assertNoErr(t, err)
		assertEqual(t, 0, len(routes))

		r := model.Route{ID: "r1", Name: "test", Match: model.MatchRule{PathPrefix: "/api"}}
		assertNoErr(t, s.SaveRoute(ctx, r))

		got, err := s.GetRoute(ctx, "r1")
		assertNoErr(t, err)
		assertEqual(t, "test", got.Name)

		routes, err = s.ListRoutes(ctx)
		assertNoErr(t, err)
		assertEqual(t, 1, len(routes))

		r.Name = "updated"
		assertNoErr(t, s.SaveRoute(ctx, r))
		got, _ = s.GetRoute(ctx, "r1")
		assertEqual(t, "updated", got.Name)

		assertNoErr(t, s.DeleteRoute(ctx, "r1"))
		_, err = s.GetRoute(ctx, "r1")
		assertIs(t, err, model.ErrNotFound)

		err = s.DeleteRoute(ctx, "nonexistent")
		assertIs(t, err, model.ErrNotFound)
	})
}

// ─── Groups ─────────────────────────────────────────────────────────────────

func TestGroupCRUD(t *testing.T) {
	runForAll(t, "CRUD", func(t *testing.T, s store.Store) {
		ctx := context.Background()

		groups, _ := s.ListGroups(ctx)
		assertEqual(t, 0, len(groups))

		g := model.RouteGroup{ID: "g1", Name: "grp"}
		assertNoErr(t, s.SaveGroup(ctx, g))

		got, err := s.GetGroup(ctx, "g1")
		assertNoErr(t, err)
		assertEqual(t, "grp", got.Name)

		g.Name = "updated"
		assertNoErr(t, s.SaveGroup(ctx, g))
		got, _ = s.GetGroup(ctx, "g1")
		assertEqual(t, "updated", got.Name)

		assertNoErr(t, s.DeleteGroup(ctx, "g1"))
		_, err = s.GetGroup(ctx, "g1")
		assertIs(t, err, model.ErrNotFound)
	})
}

// ─── Middlewares ─────────────────────────────────────────────────────────────

func TestMiddlewareCRUD(t *testing.T) {
	runForAll(t, "CRUD", func(t *testing.T, s store.Store) {
		ctx := context.Background()

		mws, _ := s.ListMiddlewares(ctx)
		assertEqual(t, 0, len(mws))

		m := model.Middleware{ID: "m1", Name: "cors", Type: model.MiddlewareTypeCORS}
		assertNoErr(t, s.SaveMiddleware(ctx, m))

		got, err := s.GetMiddleware(ctx, "m1")
		assertNoErr(t, err)
		assertEqual(t, "cors", got.Name)

		assertNoErr(t, s.DeleteMiddleware(ctx, "m1"))
		_, err = s.GetMiddleware(ctx, "m1")
		assertIs(t, err, model.ErrNotFound)
	})
}

// ─── Listeners ──────────────────────────────────────────────────────────────

func TestListenerCRUD(t *testing.T) {
	runForAll(t, "CRUD", func(t *testing.T, s store.Store) {
		ctx := context.Background()

		ls, _ := s.ListListeners(ctx)
		assertEqual(t, 0, len(ls))

		l := model.Listener{ID: "l1", Name: "main", Port: 3000}
		assertNoErr(t, s.SaveListener(ctx, l))

		got, _ := s.GetListener(ctx, "l1")
		assertEqual(t, "main", got.Name)
		assertEqual(t, uint32(3000), got.Port)

		assertNoErr(t, s.DeleteListener(ctx, "l1"))
		_, err := s.GetListener(ctx, "l1")
		assertIs(t, err, model.ErrNotFound)
	})
}

// ─── Destinations ───────────────────────────────────────────────────────────

func TestDestinationCRUD(t *testing.T) {
	runForAll(t, "CRUD", func(t *testing.T, s store.Store) {
		ctx := context.Background()

		ds, _ := s.ListDestinations(ctx)
		assertEqual(t, 0, len(ds))

		d := model.Destination{ID: "d1", Name: "up", Host: "10.0.0.1", Port: 80}
		assertNoErr(t, s.SaveDestination(ctx, d))

		got, _ := s.GetDestination(ctx, "d1")
		assertEqual(t, "up", got.Name)

		assertNoErr(t, s.DeleteDestination(ctx, "d1"))
		_, err := s.GetDestination(ctx, "d1")
		assertIs(t, err, model.ErrNotFound)
	})
}

// ─── Snapshots ──────────────────────────────────────────────────────────────

func TestSnapshotCRUD(t *testing.T) {
	runForAll(t, "CRUD", func(t *testing.T, s store.Store) {
		ctx := context.Background()

		summaries, _ := s.ListSnapshots(ctx)
		assertEqual(t, 0, len(summaries))

		vs := model.VersionedSnapshot{
			ID: "s1", Name: "v1",
			CreatedAt: time.Now().UTC(),
			Snapshot:  model.Snapshot{Routes: []model.Route{{ID: "r1", Name: "test"}}},
		}
		assertNoErr(t, s.SaveSnapshot(ctx, vs))

		got, err := s.GetSnapshot(ctx, "s1")
		assertNoErr(t, err)
		assertEqual(t, "v1", got.Name)
		assertEqual(t, 1, len(got.Snapshot.Routes))

		summaries, _ = s.ListSnapshots(ctx)
		assertEqual(t, 1, len(summaries))
		assertEqual(t, false, summaries[0].Active)

		assertNoErr(t, s.DeleteSnapshot(ctx, "s1"))
		_, err = s.GetSnapshot(ctx, "s1")
		assertIs(t, err, model.ErrNotFound)
	})
}

func TestSnapshotActivation(t *testing.T) {
	runForAll(t, "Activate", func(t *testing.T, s store.Store) {
		ctx := context.Background()

		_, err := s.GetActiveSnapshot(ctx)
		assertIs(t, err, model.ErrNoActiveSnapshot)

		vs := model.VersionedSnapshot{ID: "s1", Name: "v1", Snapshot: model.Snapshot{}}
		assertNoErr(t, s.SaveSnapshot(ctx, vs))
		assertNoErr(t, s.ActivateSnapshot(ctx, "s1"))

		active, err := s.GetActiveSnapshot(ctx)
		assertNoErr(t, err)
		assertEqual(t, "s1", active.ID)

		summaries, _ := s.ListSnapshots(ctx)
		for _, sm := range summaries {
			if sm.ID == "s1" {
				assertEqual(t, true, sm.Active)
			}
		}

		err = s.ActivateSnapshot(ctx, "nonexistent")
		assertIs(t, err, model.ErrNotFound)
	})
}

func TestDeleteActiveSnapshotClearsPointer(t *testing.T) {
	runForAll(t, "DeleteActive", func(t *testing.T, s store.Store) {
		ctx := context.Background()

		vs := model.VersionedSnapshot{ID: "s1", Name: "v1", Snapshot: model.Snapshot{}}
		assertNoErr(t, s.SaveSnapshot(ctx, vs))
		assertNoErr(t, s.ActivateSnapshot(ctx, "s1"))
		assertNoErr(t, s.DeleteSnapshot(ctx, "s1"))

		_, err := s.GetActiveSnapshot(ctx)
		assertIs(t, err, model.ErrNoActiveSnapshot)
	})
}

// ─── Subscribe ──────────────────────────────────────────────────────────────

func TestSubscribeReceivesEvents(t *testing.T) {
	runForAll(t, "Subscribe", func(t *testing.T, s store.Store) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		ch, err := s.Subscribe(ctx)
		assertNoErr(t, err)

		assertNoErr(t, s.SaveRoute(ctx, model.Route{ID: "r1", Name: "test"}))

		select {
		case ev := <-ch:
			assertEqual(t, store.ResourceRoute, ev.Resource)
			assertEqual(t, "r1", ev.ID)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for event")
		}
	})
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func assertNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func assertEqual[T comparable](t *testing.T, want, got T) {
	t.Helper()
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func assertIs(t *testing.T, err, target error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error wrapping %v, got nil", target)
	}
	if errors.Is(err, target) {
		return
	}
	if !containsErr(err, target) {
		t.Fatalf("expected error wrapping %v, got: %v", target, err)
	}
}

func containsErr(err, target error) bool {
	for e := err; e != nil; {
		if e.Error() == target.Error() {
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			break
		}
		e = u.Unwrap()
	}
	// Fallback: check if error string contains the target string
	return len(err.Error()) > 0 && len(target.Error()) > 0 &&
		contains(err.Error(), target.Error())
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
