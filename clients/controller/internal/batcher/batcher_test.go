// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package batcher

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/achetronic/vrata/clients/controller/internal/vrata"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestBatcher_DebounceFlush(t *testing.T) {
	var snapshotCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/snapshots" {
			snapshotCount.Add(1)
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(vrata.Snapshot{ID: "s1", Name: "test"})
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	client := vrata.NewClient(srv.URL)
	b := New(client, 100*time.Millisecond, 1000, true, true, testLogger())

	ctx := context.Background()
	b.Signal(ctx)
	b.Signal(ctx)
	b.Signal(ctx)

	time.Sleep(300 * time.Millisecond)

	if snapshotCount.Load() != 1 {
		t.Errorf("expected 1 snapshot after debounce, got %d", snapshotCount.Load())
	}
	if b.Pending() != 0 {
		t.Errorf("expected 0 pending, got %d", b.Pending())
	}
}

func TestBatcher_MaxBatchForces(t *testing.T) {
	var snapshotCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/snapshots" {
			snapshotCount.Add(1)
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(vrata.Snapshot{ID: "s1", Name: "test"})
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	client := vrata.NewClient(srv.URL)
	b := New(client, 10*time.Second, 3, true, true, testLogger())

	ctx := context.Background()
	b.Signal(ctx)
	b.Signal(ctx)

	if snapshotCount.Load() != 0 {
		t.Error("should not snapshot before max batch")
	}

	b.Signal(ctx)

	time.Sleep(50 * time.Millisecond)
	if snapshotCount.Load() != 1 {
		t.Errorf("expected 1 snapshot at max batch, got %d", snapshotCount.Load())
	}
}

func TestBatcher_FlushForces(t *testing.T) {
	var snapshotCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/snapshots" {
			snapshotCount.Add(1)
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(vrata.Snapshot{ID: "s1", Name: "test"})
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	client := vrata.NewClient(srv.URL)
	b := New(client, 10*time.Second, 1000, true, true, testLogger())

	ctx := context.Background()
	b.Signal(ctx)
	b.Flush(ctx)

	if snapshotCount.Load() != 1 {
		t.Errorf("expected 1 snapshot from flush, got %d", snapshotCount.Load())
	}
}

func TestBatcher_FlushNoOp(t *testing.T) {
	var snapshotCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/snapshots" {
			snapshotCount.Add(1)
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	client := vrata.NewClient(srv.URL)
	b := New(client, 10*time.Second, 1000, true, true, testLogger())

	b.Flush(context.Background())

	if snapshotCount.Load() != 0 {
		t.Error("flush with no pending should be a no-op")
	}
}

func TestBatcher_TotalSignals(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(vrata.Snapshot{ID: "s1", Name: "test"})
	}))
	defer srv.Close()

	client := vrata.NewClient(srv.URL)
	b := New(client, 10*time.Second, 1000, true, true, testLogger())

	ctx := context.Background()
	b.Signal(ctx)
	b.Signal(ctx)
	b.Signal(ctx)

	if b.TotalSignals() != 3 {
		t.Errorf("expected 3 total signals, got %d", b.TotalSignals())
	}
}

func TestBatcher_AutoCreateDisabled(t *testing.T) {
	var snapshotCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/snapshots" {
			snapshotCount.Add(1)
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(vrata.Snapshot{ID: "s1", Name: "test"})
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	client := vrata.NewClient(srv.URL)
	b := New(client, 50*time.Millisecond, 1000, false, false, testLogger())

	ctx := context.Background()
	b.Signal(ctx)
	b.Signal(ctx)

	time.Sleep(200 * time.Millisecond)

	if snapshotCount.Load() != 0 {
		t.Errorf("expected 0 snapshots when autoCreate=false, got %d", snapshotCount.Load())
	}
	if b.Pending() != 0 {
		t.Errorf("expected 0 pending after flush (cleared without snapshot), got %d", b.Pending())
	}
}

func TestBatcher_AutoCreateTrue_AutoActivateFalse(t *testing.T) {
	var createCount atomic.Int32
	var activateCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/snapshots" {
			createCount.Add(1)
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(vrata.Snapshot{ID: "s1", Name: "test"})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/snapshots/s1/activate" {
			activateCount.Add(1)
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	client := vrata.NewClient(srv.URL)
	b := New(client, 50*time.Millisecond, 1000, true, false, testLogger())

	ctx := context.Background()
	b.Signal(ctx)
	b.Signal(ctx)

	time.Sleep(200 * time.Millisecond)

	if createCount.Load() != 1 {
		t.Errorf("expected 1 snapshot created, got %d", createCount.Load())
	}
	if activateCount.Load() != 0 {
		t.Errorf("expected 0 activations when autoActivate=false, got %d", activateCount.Load())
	}
}

func TestBatcher_AutoCreateTrue_AutoActivateTrue(t *testing.T) {
	var createCount atomic.Int32
	var activateCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/snapshots" {
			createCount.Add(1)
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(vrata.Snapshot{ID: "s1", Name: "test"})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/snapshots/s1/activate" {
			activateCount.Add(1)
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	client := vrata.NewClient(srv.URL)
	b := New(client, 50*time.Millisecond, 1000, true, true, testLogger())

	ctx := context.Background()
	b.Signal(ctx)

	time.Sleep(200 * time.Millisecond)

	if createCount.Load() != 1 {
		t.Errorf("expected 1 snapshot created, got %d", createCount.Load())
	}
	if activateCount.Load() != 1 {
		t.Errorf("expected 1 activation, got %d", activateCount.Load())
	}
}
