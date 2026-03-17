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

	"github.com/achetronic/rutoso/internal/model"
	memstore "github.com/achetronic/rutoso/internal/store/memory"
)

func TestSyncStreamSendsInitialSnapshot(t *testing.T) {
	st := memstore.New()
	ctx := context.Background()

	st.SaveListener(ctx, model.Listener{ID: "l1", Name: "listener-1", Port: 8080})
	st.SaveRoute(ctx, model.Route{
		ID:   "r1",
		Name: "route-1",
		Match: model.MatchRule{PathPrefix: "/api"},
		DirectResponse: &model.RouteDirectResponse{Status: 200, Body: "ok"},
	})

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	deps := &Dependencies{Store: st, Logger: logger}

	reqCtx, reqCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer reqCancel()

	req := httptest.NewRequest("GET", "/api/v1/sync/stream", nil).WithContext(reqCtx)
	w := newFlushRecorder()

	done := make(chan struct{})
	go func() {
		deps.SyncStream(w, req)
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
			var snap model.Snapshot
			if err := json.Unmarshal([]byte(data), &snap); err != nil {
				t.Fatalf("invalid snapshot JSON: %v", err)
			}
			if len(snap.Listeners) != 1 {
				t.Errorf("expected 1 listener, got %d", len(snap.Listeners))
			}
			if len(snap.Routes) != 1 {
				t.Errorf("expected 1 route, got %d", len(snap.Routes))
			}
			return
		}
	}
	t.Fatal("no data line found in SSE stream")
}

func TestSyncStreamSendsOnStoreChange(t *testing.T) {
	st := memstore.New()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	deps := &Dependencies{Store: st, Logger: logger}

	reqCtx, reqCancel := context.WithCancel(context.Background())
	defer reqCancel()

	req := httptest.NewRequest("GET", "/api/v1/sync/stream", nil).WithContext(reqCtx)
	w := newFlushRecorder()

	go deps.SyncStream(w, req)

	time.Sleep(200 * time.Millisecond)

	st.SaveDestination(context.Background(), model.Destination{
		ID:   "d1",
		Name: "dest-1",
		Host: "10.0.0.1",
		Port: 80,
	})

	time.Sleep(200 * time.Millisecond)
	reqCancel()
	time.Sleep(100 * time.Millisecond)

	body := w.Body.String()
	count := strings.Count(body, "event: snapshot")
	if count < 2 {
		t.Errorf("expected at least 2 snapshot events, got %d. Body:\n%s", count, body)
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
