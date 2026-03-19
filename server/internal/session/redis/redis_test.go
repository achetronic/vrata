// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package redis

import (
	"context"
	"os"
	"testing"
	"time"
)

func testAddr() string {
	if addr := os.Getenv("REDIS_ADDRESS"); addr != "" {
		return addr
	}
	return "localhost:6379"
}

func skipWithoutRedis(t *testing.T) *Store {
	t.Helper()
	store, err := New(testAddr(), "", 0)
	if err != nil {
		t.Skipf("Redis not available at %s: %v", testAddr(), err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestStore_SetGet(t *testing.T) {
	store := skipWithoutRedis(t)
	ctx := context.Background()

	if err := store.Set(ctx, "sid-1", "route-1", "dest-A", 60); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := store.Get(ctx, "sid-1", "route-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "dest-A" {
		t.Errorf("expected dest-A, got %q", got)
	}
}

func TestStore_GetMissing(t *testing.T) {
	store := skipWithoutRedis(t)
	ctx := context.Background()

	got, err := store.Get(ctx, "nonexistent-sid", "nonexistent-route")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string for missing key, got %q", got)
	}
}

func TestStore_Overwrite(t *testing.T) {
	store := skipWithoutRedis(t)
	ctx := context.Background()

	_ = store.Set(ctx, "sid-ow", "route-ow", "dest-A", 60)
	_ = store.Set(ctx, "sid-ow", "route-ow", "dest-B", 60)

	got, _ := store.Get(ctx, "sid-ow", "route-ow")
	if got != "dest-B" {
		t.Errorf("expected dest-B after overwrite, got %q", got)
	}
}

func TestStore_TTLExpiry(t *testing.T) {
	store := skipWithoutRedis(t)
	ctx := context.Background()

	_ = store.Set(ctx, "sid-ttl", "route-ttl", "dest-A", 1)
	time.Sleep(1500 * time.Millisecond)

	got, _ := store.Get(ctx, "sid-ttl", "route-ttl")
	if got != "" {
		t.Errorf("expected empty after TTL expiry, got %q", got)
	}
}

func TestStore_RouteIsolation(t *testing.T) {
	store := skipWithoutRedis(t)
	ctx := context.Background()

	_ = store.Set(ctx, "sid-iso", "route-X", "dest-A", 60)
	_ = store.Set(ctx, "sid-iso", "route-Y", "dest-B", 60)

	gotX, _ := store.Get(ctx, "sid-iso", "route-X")
	gotY, _ := store.Get(ctx, "sid-iso", "route-Y")

	if gotX != "dest-A" {
		t.Errorf("route-X: expected dest-A, got %q", gotX)
	}
	if gotY != "dest-B" {
		t.Errorf("route-Y: expected dest-B, got %q", gotY)
	}
}
