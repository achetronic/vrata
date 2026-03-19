// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/achetronic/vrata/internal/model"
	"github.com/achetronic/vrata/internal/proxy"
)

func TestClientAppliesSnapshot(t *testing.T) {
	vs := model.VersionedSnapshot{
		ID:   "snap-1",
		Name: "test-snap",
		Snapshot: model.Snapshot{
			Listeners: []model.Listener{
				{ID: "l1", Name: "test-listener", Address: "0.0.0.0", Port: 9090},
			},
			Routes: []model.Route{
				{
					ID:   "r1",
					Name: "test-route",
					Match: model.MatchRule{
						PathPrefix: "/test",
					},
					DirectResponse: &model.RouteDirectResponse{
						Status: 200,
						Body:   "ok",
					},
				},
			},
			Groups:       []model.RouteGroup{},
			Destinations: []model.Destination{},
			Middlewares:  []model.Middleware{},
		},
	}

	data, err := json.Marshal(vs)
	if err != nil {
		t.Fatal(err)
	}

	snapshotSent := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/sync/snapshot" {
			http.NotFound(w, r)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", 500)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher.Flush()

		fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", data)
		flusher.Flush()

		close(snapshotSent)

		<-r.Context().Done()
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	router := proxy.NewRouter()
	listenerMgr := proxy.NewListenerManager(router, logger)

	client := New(Dependencies{
		ControlPlaneAddr:  srv.URL,
		ReconnectInterval: 100 * time.Millisecond,
		Router:            router,
		ListenerManager:   listenerMgr,
		Logger:            logger,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go client.Run(ctx)

	select {
	case <-snapshotSent:
	case <-ctx.Done():
		t.Fatal("timed out waiting for snapshot to be sent")
	}

	time.Sleep(100 * time.Millisecond)

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if w.Body.String() != "ok" {
		t.Errorf("expected body %q, got %q", "ok", w.Body.String())
	}
}

func TestClientReconnectsOnDisconnect(t *testing.T) {
	vs := model.VersionedSnapshot{
		ID:   "snap-r",
		Name: "reconnect",
		Snapshot: model.Snapshot{
			Listeners:    []model.Listener{},
			Routes:       []model.Route{},
			Groups:       []model.RouteGroup{},
			Destinations: []model.Destination{},
			Middlewares:  []model.Middleware{},
		},
	}

	data, _ := json.Marshal(vs)
	connectCount := make(chan struct{}, 10)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}

		connectCount <- struct{}{}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher.Flush()

		fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", data)
		flusher.Flush()
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	router := proxy.NewRouter()
	listenerMgr := proxy.NewListenerManager(router, logger)

	client := New(Dependencies{
		ControlPlaneAddr:  srv.URL,
		ReconnectInterval: 50 * time.Millisecond,
		Router:            router,
		ListenerManager:   listenerMgr,
		Logger:            logger,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go client.Run(ctx)

	count := 0
	for count < 2 {
		select {
		case <-connectCount:
			count++
		case <-ctx.Done():
			t.Fatalf("expected at least 2 connections, got %d", count)
		}
	}
}
