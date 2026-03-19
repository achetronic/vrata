package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/achetronic/vrata/internal/model"
	memstore "github.com/achetronic/vrata/internal/store/memory"
)

func TestSyncSnapshotSendsActiveSnapshot(t *testing.T) {
	st := memstore.New()
	ctx := context.Background()

	st.SaveListener(ctx, model.Listener{ID: "l1", Name: "listener-1", Port: 8080})
	st.SaveRoute(ctx, model.Route{
		ID:   "r1",
		Name: "route-1",
		Match: model.MatchRule{PathPrefix: "/api"},
		DirectResponse: &model.RouteDirectResponse{Status: 200, Body: "ok"},
	})

	st.SaveSnapshot(ctx, model.VersionedSnapshot{
		ID:   "snap-1",
		Name: "v1",
		Snapshot: model.Snapshot{
			Listeners: []model.Listener{{ID: "l1", Name: "listener-1", Port: 8080}},
			Routes: []model.Route{{
				ID:   "r1",
				Name: "route-1",
				Match: model.MatchRule{PathPrefix: "/api"},
				DirectResponse: &model.RouteDirectResponse{Status: 200, Body: "ok"},
			}},
			Groups:       []model.RouteGroup{},
			Destinations: []model.Destination{},
			Middlewares:  []model.Middleware{},
		},
	})
	st.ActivateSnapshot(ctx, "snap-1")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	deps := &Dependencies{Store: st, Logger: logger}

	reqCtx, reqCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer reqCancel()

	req := httptest.NewRequest("GET", "/api/v1/sync/snapshot", nil).WithContext(reqCtx)
	w := newFlushRecorder()

	done := make(chan struct{})
	go func() {
		deps.SyncSnapshot(w, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		reqCancel()
		t.Fatal("handler did not return")
	}

	body := w.Body.String()
	if !strings.Contains(body, "event: snapshot") {
		t.Fatalf("expected SSE event, got:\n%s", body)
	}

	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			var vs model.VersionedSnapshot
			if err := json.Unmarshal([]byte(data), &vs); err != nil {
				t.Fatalf("invalid snapshot JSON: %v", err)
			}
			if len(vs.Snapshot.Listeners) != 1 {
				t.Errorf("expected 1 listener, got %d", len(vs.Snapshot.Listeners))
			}
			if len(vs.Snapshot.Routes) != 1 {
				t.Errorf("expected 1 route, got %d", len(vs.Snapshot.Routes))
			}
			return
		}
	}
	t.Fatal("no data line found in SSE stream")
}

func TestSyncSnapshotSendsOnSnapshotActivate(t *testing.T) {
	st := memstore.New()
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	deps := &Dependencies{Store: st, Logger: logger}

	reqCtx, reqCancel := context.WithCancel(context.Background())
	defer reqCancel()

	req := httptest.NewRequest("GET", "/api/v1/sync/snapshot", nil).WithContext(reqCtx)
	w := newFlushRecorder()

	done := make(chan struct{})
	go func() {
		deps.SyncSnapshot(w, req)
		close(done)
	}()

	time.Sleep(200 * time.Millisecond)

	st.SaveSnapshot(ctx, model.VersionedSnapshot{
		ID:   "snap-1",
		Name: "v1",
		Snapshot: model.Snapshot{
			Listeners:    []model.Listener{},
			Routes:       []model.Route{},
			Groups:       []model.RouteGroup{},
			Destinations: []model.Destination{{ID: "d1", Name: "dest-1", Host: "10.0.0.1", Port: 80}},
			Middlewares:  []model.Middleware{},
		},
	})

	time.Sleep(100 * time.Millisecond)

	st.ActivateSnapshot(ctx, "snap-1")

	time.Sleep(200 * time.Millisecond)
	reqCancel()

	// Wait for the handler to fully exit before reading the body.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit")
	}

	body := w.Body.String()
	count := strings.Count(body, "event: snapshot")
	if count < 1 {
		t.Errorf("expected at least 1 snapshot event after activate, got %d. Body:\n%s", count, body)
	}
}

func TestSyncSnapshotNoSnapshotNoEvent(t *testing.T) {
	st := memstore.New()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	deps := &Dependencies{Store: st, Logger: logger}

	reqCtx, reqCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer reqCancel()

	req := httptest.NewRequest("GET", "/api/v1/sync/snapshot", nil).WithContext(reqCtx)
	w := newFlushRecorder()

	done := make(chan struct{})
	go func() {
		deps.SyncSnapshot(w, req)
		close(done)
	}()

	<-done

	body := w.Body.String()
	if strings.Contains(body, "event: snapshot") {
		t.Fatalf("expected no snapshot event without active snapshot, got:\n%s", body)
	}
}

// flushRecorder is an httptest.ResponseRecorder that implements http.Flusher.
type flushRecorder struct {
	*httptest.ResponseRecorder
}

func newFlushRecorder() *flushRecorder {
	return &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
}

func (fr *flushRecorder) Flush() {}

// Unwrap exposes the underlying ResponseWriter for http.ResponseController.
func (fr *flushRecorder) Unwrap() http.ResponseWriter {
	return fr.ResponseRecorder
}
