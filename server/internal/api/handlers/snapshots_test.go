// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/achetronic/vrata/internal/model"
	memstore "github.com/achetronic/vrata/internal/store/memory"
)

func snapshotDeps(t *testing.T) (*Dependencies, *memstore.Store) {
	t.Helper()
	st := memstore.New()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return &Dependencies{Store: st, Logger: logger}, st
}

func seedLiveConfig(t *testing.T, st *memstore.Store) {
	t.Helper()
	ctx := context.Background()
	st.SaveListener(ctx, model.Listener{ID: "l1", Name: "main", Address: "0.0.0.0", Port: 3000})
	st.SaveRoute(ctx, model.Route{
		ID: "r1", Name: "test",
		Match:          model.MatchRule{PathPrefix: "/test"},
		DirectResponse: &model.RouteDirectResponse{Status: 200, Body: "ok"},
	})
	st.SaveDestination(ctx, model.Destination{ID: "d1", Name: "upstream", Host: "10.0.0.1", Port: 80})
}

// ── CreateSnapshot ──────────────────────────────────────────────────────────

func TestCreateSnapshot(t *testing.T) {
	deps, st := snapshotDeps(t)
	seedLiveConfig(t, st)

	body, _ := json.Marshal(SnapshotCreateRequest{Name: "v1.0"})
	req := httptest.NewRequest("POST", "/api/v1/snapshots", bytes.NewReader(body))
	w := httptest.NewRecorder()

	deps.CreateSnapshot(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var vs model.VersionedSnapshot
	if err := json.NewDecoder(w.Body).Decode(&vs); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if vs.Name != "v1.0" {
		t.Errorf("expected name %q, got %q", "v1.0", vs.Name)
	}
	if vs.ID == "" {
		t.Error("expected non-empty ID")
	}
	if len(vs.Snapshot.Listeners) != 1 {
		t.Errorf("expected 1 listener, got %d", len(vs.Snapshot.Listeners))
	}
	if len(vs.Snapshot.Routes) != 1 {
		t.Errorf("expected 1 route, got %d", len(vs.Snapshot.Routes))
	}
	if len(vs.Snapshot.Destinations) != 1 {
		t.Errorf("expected 1 destination, got %d", len(vs.Snapshot.Destinations))
	}
}

func TestCreateSnapshotMissingName(t *testing.T) {
	deps, _ := snapshotDeps(t)

	body, _ := json.Marshal(SnapshotCreateRequest{})
	req := httptest.NewRequest("POST", "/api/v1/snapshots", bytes.NewReader(body))
	w := httptest.NewRecorder()

	deps.CreateSnapshot(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateSnapshotInvalidBody(t *testing.T) {
	deps, _ := snapshotDeps(t)

	req := httptest.NewRequest("POST", "/api/v1/snapshots", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()

	deps.CreateSnapshot(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// ── ListSnapshots ───────────────────────────────────────────────────────────

func TestListSnapshotsEmpty(t *testing.T) {
	deps, _ := snapshotDeps(t)

	req := httptest.NewRequest("GET", "/api/v1/snapshots", nil)
	w := httptest.NewRecorder()

	deps.ListSnapshots(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var summaries []model.SnapshotSummary
	if err := json.NewDecoder(w.Body).Decode(&summaries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("expected 0 snapshots, got %d", len(summaries))
	}
}

func TestListSnapshotsShowsActive(t *testing.T) {
	deps, st := snapshotDeps(t)
	ctx := context.Background()

	st.SaveSnapshot(ctx, model.VersionedSnapshot{
		ID: "s1", Name: "v1",
		Snapshot: model.Snapshot{},
	})
	st.SaveSnapshot(ctx, model.VersionedSnapshot{
		ID: "s2", Name: "v2",
		Snapshot: model.Snapshot{},
	})
	st.ActivateSnapshot(ctx, "s2")

	req := httptest.NewRequest("GET", "/api/v1/snapshots", nil)
	w := httptest.NewRecorder()

	deps.ListSnapshots(w, req)

	var summaries []model.SnapshotSummary
	json.NewDecoder(w.Body).Decode(&summaries)

	if len(summaries) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(summaries))
	}

	activeCount := 0
	for _, s := range summaries {
		if s.Active {
			activeCount++
			if s.ID != "s2" {
				t.Errorf("expected s2 active, got %s", s.ID)
			}
		}
	}
	if activeCount != 1 {
		t.Errorf("expected exactly 1 active, got %d", activeCount)
	}
}

// ── GetSnapshot ─────────────────────────────────────────────────────────────

func TestGetSnapshot(t *testing.T) {
	deps, st := snapshotDeps(t)
	ctx := context.Background()

	st.SaveSnapshot(ctx, model.VersionedSnapshot{
		ID: "s1", Name: "v1",
		Snapshot: model.Snapshot{
			Routes: []model.Route{{ID: "r1", Name: "test"}},
		},
	})

	req := httptest.NewRequest("GET", "/api/v1/snapshots/s1", nil)
	req.SetPathValue("snapshotId", "s1")
	w := httptest.NewRecorder()

	deps.GetSnapshot(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var vs model.VersionedSnapshot
	json.NewDecoder(w.Body).Decode(&vs)
	if vs.ID != "s1" {
		t.Errorf("expected id s1, got %s", vs.ID)
	}
	if len(vs.Snapshot.Routes) != 1 {
		t.Errorf("expected 1 route in snapshot, got %d", len(vs.Snapshot.Routes))
	}
}

func TestGetSnapshotNotFound(t *testing.T) {
	deps, _ := snapshotDeps(t)

	req := httptest.NewRequest("GET", "/api/v1/snapshots/nonexistent", nil)
	req.SetPathValue("snapshotId", "nonexistent")
	w := httptest.NewRecorder()

	deps.GetSnapshot(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// ── DeleteSnapshot ──────────────────────────────────────────────────────────

func TestDeleteSnapshot(t *testing.T) {
	deps, st := snapshotDeps(t)
	ctx := context.Background()

	st.SaveSnapshot(ctx, model.VersionedSnapshot{ID: "s1", Name: "v1", Snapshot: model.Snapshot{}})

	req := httptest.NewRequest("DELETE", "/api/v1/snapshots/s1", nil)
	req.SetPathValue("snapshotId", "s1")
	w := httptest.NewRecorder()

	deps.DeleteSnapshot(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	summaries, _ := st.ListSnapshots(ctx)
	if len(summaries) != 0 {
		t.Errorf("expected 0 snapshots after delete, got %d", len(summaries))
	}
}

func TestDeleteSnapshotNotFound(t *testing.T) {
	deps, _ := snapshotDeps(t)

	req := httptest.NewRequest("DELETE", "/api/v1/snapshots/nonexistent", nil)
	req.SetPathValue("snapshotId", "nonexistent")
	w := httptest.NewRecorder()

	deps.DeleteSnapshot(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDeleteActiveSnapshotClearsPointer(t *testing.T) {
	deps, st := snapshotDeps(t)
	ctx := context.Background()

	st.SaveSnapshot(ctx, model.VersionedSnapshot{ID: "s1", Name: "v1", Snapshot: model.Snapshot{}})
	st.ActivateSnapshot(ctx, "s1")

	req := httptest.NewRequest("DELETE", "/api/v1/snapshots/s1", nil)
	req.SetPathValue("snapshotId", "s1")
	w := httptest.NewRecorder()

	deps.DeleteSnapshot(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	_, err := st.GetActiveSnapshot(ctx)
	if err == nil {
		t.Error("expected error after deleting active snapshot, got nil")
	}
}

// ── ActivateSnapshot ────────────────────────────────────────────────────────

func TestActivateSnapshot(t *testing.T) {
	deps, st := snapshotDeps(t)
	ctx := context.Background()

	st.SaveSnapshot(ctx, model.VersionedSnapshot{ID: "s1", Name: "v1", Snapshot: model.Snapshot{}})

	req := httptest.NewRequest("POST", "/api/v1/snapshots/s1/activate", nil)
	req.SetPathValue("snapshotId", "s1")
	w := httptest.NewRecorder()

	deps.ActivateSnapshot(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var summary model.SnapshotSummary
	json.NewDecoder(w.Body).Decode(&summary)

	if summary.ID != "s1" {
		t.Errorf("expected id s1, got %s", summary.ID)
	}
	if !summary.Active {
		t.Error("expected Active=true in response")
	}

	active, err := st.GetActiveSnapshot(ctx)
	if err != nil {
		t.Fatalf("GetActiveSnapshot: %v", err)
	}
	if active.ID != "s1" {
		t.Errorf("expected active snapshot s1, got %s", active.ID)
	}
}

func TestActivateSnapshotNotFound(t *testing.T) {
	deps, _ := snapshotDeps(t)

	req := httptest.NewRequest("POST", "/api/v1/snapshots/nonexistent/activate", nil)
	req.SetPathValue("snapshotId", "nonexistent")
	w := httptest.NewRecorder()

	deps.ActivateSnapshot(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
