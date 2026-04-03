// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package reconciler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/achetronic/vrata/clients/controller/internal/mapper"
	"github.com/achetronic/vrata/clients/controller/internal/vrata"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// mockVrata is a minimal in-memory Vrata API for unit tests. It supports
// CRUD for routes, groups, destinations, middlewares, and listeners.
type mockVrata struct {
	mu           sync.Mutex
	routes       []vrata.Route
	groups       []vrata.RouteGroup
	destinations []vrata.Destination
	middlewares  []vrata.Middleware
	listeners    []vrata.Listener
	idCounter    int
}

func (m *mockVrata) nextID() string {
	m.idCounter++
	return "id-" + strings.Repeat("0", 4) + string(rune('0'+m.idCounter))
}

func (m *mockVrata) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/routes", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(m.routes)
		case http.MethodPost:
			var route vrata.Route
			json.NewDecoder(r.Body).Decode(&route)
			route.ID = m.nextID()
			m.routes = append(m.routes, route)
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(route)
		}
	})

	mux.HandleFunc("/api/v1/routes/", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/routes/")
		switch r.Method {
		case http.MethodPut:
			var route vrata.Route
			json.NewDecoder(r.Body).Decode(&route)
			for i, existing := range m.routes {
				if existing.ID == id {
					route.ID = id
					m.routes[i] = route
					break
				}
			}
			w.WriteHeader(200)
		case http.MethodDelete:
			for i, existing := range m.routes {
				if existing.ID == id {
					m.routes = append(m.routes[:i], m.routes[i+1:]...)
					break
				}
			}
			w.WriteHeader(200)
		}
	})

	mux.HandleFunc("/api/v1/groups", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(m.groups)
		case http.MethodPost:
			var group vrata.RouteGroup
			json.NewDecoder(r.Body).Decode(&group)
			group.ID = m.nextID()
			m.groups = append(m.groups, group)
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(group)
		}
	})

	mux.HandleFunc("/api/v1/groups/", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/groups/")
		switch r.Method {
		case http.MethodPut:
			var group vrata.RouteGroup
			json.NewDecoder(r.Body).Decode(&group)
			for i, existing := range m.groups {
				if existing.ID == id {
					group.ID = id
					m.groups[i] = group
					break
				}
			}
			w.WriteHeader(200)
		case http.MethodDelete:
			for i, existing := range m.groups {
				if existing.ID == id {
					m.groups = append(m.groups[:i], m.groups[i+1:]...)
					break
				}
			}
			w.WriteHeader(200)
		}
	})

	mux.HandleFunc("/api/v1/destinations", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(m.destinations)
		case http.MethodPost:
			var dest vrata.Destination
			json.NewDecoder(r.Body).Decode(&dest)
			dest.ID = m.nextID()
			m.destinations = append(m.destinations, dest)
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(dest)
		}
	})

	mux.HandleFunc("/api/v1/destinations/", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/destinations/")
		if r.Method == http.MethodDelete {
			for i, existing := range m.destinations {
				if existing.ID == id {
					m.destinations = append(m.destinations[:i], m.destinations[i+1:]...)
					break
				}
			}
			w.WriteHeader(200)
		}
	})

	mux.HandleFunc("/api/v1/middlewares", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(m.middlewares)
		case http.MethodPost:
			var mw vrata.Middleware
			json.NewDecoder(r.Body).Decode(&mw)
			mw.ID = m.nextID()
			m.middlewares = append(m.middlewares, mw)
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(mw)
		}
	})

	mux.HandleFunc("/api/v1/middlewares/", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/middlewares/")
		if r.Method == http.MethodDelete {
			for i, existing := range m.middlewares {
				if existing.ID == id {
					m.middlewares = append(m.middlewares[:i], m.middlewares[i+1:]...)
					break
				}
			}
			w.WriteHeader(200)
		}
	})

	mux.HandleFunc("/api/v1/listeners", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(m.listeners)
		case http.MethodPost:
			var l vrata.Listener
			json.NewDecoder(r.Body).Decode(&l)
			l.ID = m.nextID()
			m.listeners = append(m.listeners, l)
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(l)
		}
	})

	mux.HandleFunc("/api/v1/listeners/", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/listeners/")
		if r.Method == http.MethodDelete {
			for i, existing := range m.listeners {
				if existing.ID == id {
					m.listeners = append(m.listeners[:i], m.listeners[i+1:]...)
					break
				}
			}
			w.WriteHeader(200)
		}
	})

	return mux
}

func setupMock() (*mockVrata, *httptest.Server, *vrata.Client) {
	mock := &mockVrata{}
	srv := httptest.NewServer(mock.handler())
	client := vrata.NewClient(srv.URL)
	return mock, srv, client
}

func makeInput(name, ns, path string) mapper.HTTPRouteInput {
	return mapper.HTTPRouteInput{
		Name: name, Namespace: ns,
		Rules: []mapper.RuleInput{{
			Matches:     []mapper.MatchInput{{PathType: "PathPrefix", PathValue: path}},
			BackendRefs: []mapper.BackendRefInput{{ServiceName: "svc", ServiceNamespace: ns, Port: 80, Weight: 1}},
		}},
	}
}

func TestApplyHTTPRoute_CreateAll(t *testing.T) {
	mock, srv, client := setupMock()
	defer srv.Close()
	rec := NewReconciler(client, testLogger())

	input := makeInput("my-route", "default", "/api")
	mapped := mapper.MapHTTPRoute(input)
	changes, err := rec.ApplyHTTPRoute(context.Background(), mapped)
	if err != nil {
		t.Fatal(err)
	}
	if changes == 0 {
		t.Error("expected changes on first apply")
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.routes) != 1 {
		t.Errorf("expected 1 route, got %d", len(mock.routes))
	}
	if len(mock.groups) != 1 {
		t.Errorf("expected 1 group, got %d", len(mock.groups))
	}
	if len(mock.destinations) != 1 {
		t.Errorf("expected 1 destination, got %d", len(mock.destinations))
	}
}

func TestApplyHTTPRoute_UpdateExisting(t *testing.T) {
	_, srv, client := setupMock()
	defer srv.Close()
	rec := NewReconciler(client, testLogger())
	ctx := context.Background()

	input := makeInput("my-route", "default", "/api")
	if _, err := rec.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(input)); err != nil {
		t.Fatal(err)
	}

	input.Rules[0].Matches[0].PathValue = "/api/v2"
	changes, err := rec.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(input))
	if err != nil {
		t.Fatal(err)
	}
	if changes == 0 {
		t.Error("expected changes on update")
	}
}

func TestApplyHTTPRoute_IntraGroupGC_RemoveMatch(t *testing.T) {
	mock, srv, client := setupMock()
	defer srv.Close()
	rec := NewReconciler(client, testLogger())
	ctx := context.Background()

	input := mapper.HTTPRouteInput{
		Name: "my-route", Namespace: "default",
		Rules: []mapper.RuleInput{{
			Matches: []mapper.MatchInput{
				{PathType: "PathPrefix", PathValue: "/a"},
				{PathType: "PathPrefix", PathValue: "/b"},
			},
			BackendRefs: []mapper.BackendRefInput{{ServiceName: "svc", ServiceNamespace: "default", Port: 80, Weight: 1}},
		}},
	}
	if _, err := rec.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(input)); err != nil {
		t.Fatal(err)
	}

	mock.mu.Lock()
	if len(mock.routes) != 2 {
		t.Fatalf("expected 2 routes after first apply, got %d", len(mock.routes))
	}
	mock.mu.Unlock()

	input.Rules[0].Matches = input.Rules[0].Matches[:1]
	if _, err := rec.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(input)); err != nil {
		t.Fatal(err)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.routes) != 1 {
		t.Errorf("expected 1 route after removing match, got %d", len(mock.routes))
	}
}

func TestApplyHTTPRoute_IntraGroupGC_RemoveMiddleware(t *testing.T) {
	mock, srv, client := setupMock()
	defer srv.Close()
	rec := NewReconciler(client, testLogger())
	ctx := context.Background()

	input := mapper.HTTPRouteInput{
		Name: "my-route", Namespace: "default",
		Rules: []mapper.RuleInput{{
			Matches:     []mapper.MatchInput{{PathType: "PathPrefix", PathValue: "/"}},
			BackendRefs: []mapper.BackendRefInput{{ServiceName: "svc", ServiceNamespace: "default", Port: 80, Weight: 1}},
			Filters:     []mapper.FilterInput{{Type: "RequestHeaderModifier", HeadersToAdd: []mapper.HeaderValue{{Name: "X-Foo", Value: "bar"}}}},
		}},
	}
	if _, err := rec.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(input)); err != nil {
		t.Fatal(err)
	}

	mock.mu.Lock()
	if len(mock.middlewares) != 1 {
		t.Fatalf("expected 1 middleware after first apply, got %d", len(mock.middlewares))
	}
	mock.mu.Unlock()

	input.Rules[0].Filters = nil
	if _, err := rec.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(input)); err != nil {
		t.Fatal(err)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.middlewares) != 0 {
		t.Errorf("expected 0 middlewares after removing filter, got %d", len(mock.middlewares))
	}
}

func TestDeleteRouteGroup_CleansAll(t *testing.T) {
	mock, srv, client := setupMock()
	defer srv.Close()
	rec := NewReconciler(client, testLogger())
	ctx := context.Background()

	input := makeInput("my-route", "default", "/api")
	if _, err := rec.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(input)); err != nil {
		t.Fatal(err)
	}

	changes, err := rec.DeleteRouteGroup(ctx, "default", "my-route")
	if err != nil {
		t.Fatal(err)
	}
	if changes == 0 {
		t.Error("expected changes from delete")
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.routes) != 0 {
		t.Errorf("expected 0 routes, got %d", len(mock.routes))
	}
	if len(mock.groups) != 0 {
		t.Errorf("expected 0 groups, got %d", len(mock.groups))
	}
	if len(mock.destinations) != 0 {
		t.Errorf("expected 0 destinations, got %d", len(mock.destinations))
	}
}

func TestDeleteRouteGroup_SharedDestSurvives(t *testing.T) {
	mock, srv, client := setupMock()
	defer srv.Close()
	rec := NewReconciler(client, testLogger())
	ctx := context.Background()

	inputA := makeInput("route-a", "default", "/a")
	inputB := makeInput("route-b", "default", "/b")
	if _, err := rec.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(inputA)); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(inputB)); err != nil {
		t.Fatal(err)
	}

	if _, err := rec.DeleteRouteGroup(ctx, "default", "route-a"); err != nil {
		t.Fatal(err)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.destinations) != 1 {
		t.Errorf("shared destination should survive, got %d", len(mock.destinations))
	}
}

func TestInit_RebuildsRefCount(t *testing.T) {
	_, srv, client := setupMock()
	defer srv.Close()

	rec1 := NewReconciler(client, testLogger())
	ctx := context.Background()

	inputA := makeInput("route-a", "default", "/a")
	inputB := makeInput("route-b", "default", "/b")
	if _, err := rec1.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(inputA)); err != nil {
		t.Fatal(err)
	}
	if _, err := rec1.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(inputB)); err != nil {
		t.Fatal(err)
	}

	rec2 := NewReconciler(client, testLogger())
	if err := rec2.Init(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := rec2.DeleteRouteGroup(ctx, "default", "route-a"); err != nil {
		t.Fatal(err)
	}

	dests, _ := client.ListDestinations(ctx)
	if len(dests) != 1 {
		t.Errorf("expected 1 destination after delete with rebuilt refcount, got %d", len(dests))
	}
}

func TestOwnedGroupNames(t *testing.T) {
	mock, srv, client := setupMock()
	defer srv.Close()
	rec := NewReconciler(client, testLogger())
	ctx := context.Background()

	mock.mu.Lock()
	mock.groups = []vrata.RouteGroup{
		{ID: "1", Name: "k8s:default/my-route"},
		{ID: "2", Name: "k8s:prod/api"},
		{ID: "3", Name: "manual-group"},
	}
	mock.mu.Unlock()

	names, err := rec.OwnedGroupNames(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Errorf("expected 2 owned groups, got %d", len(names))
	}
}

func TestOwnedListenerNames(t *testing.T) {
	mock, srv, client := setupMock()
	defer srv.Close()
	rec := NewReconciler(client, testLogger())
	ctx := context.Background()

	mock.mu.Lock()
	mock.listeners = []vrata.Listener{
		{ID: "1", Name: "k8s:default/gw/http"},
		{ID: "2", Name: "manual-listener"},
	}
	mock.mu.Unlock()

	names, err := rec.OwnedListenerNames(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 {
		t.Errorf("expected 1 owned listener, got %d", len(names))
	}
}

func TestDeleteListenerByName(t *testing.T) {
	mock, srv, client := setupMock()
	defer srv.Close()
	rec := NewReconciler(client, testLogger())
	ctx := context.Background()

	mock.mu.Lock()
	mock.listeners = []vrata.Listener{
		{ID: "1", Name: "k8s:default/gw/http"},
		{ID: "2", Name: "k8s:default/gw/https"},
	}
	mock.mu.Unlock()

	changes, err := rec.DeleteListenerByName(ctx, "k8s:default/gw/http")
	if err != nil {
		t.Fatal(err)
	}
	if changes != 1 {
		t.Errorf("expected 1 change, got %d", changes)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.listeners) != 1 {
		t.Errorf("expected 1 listener remaining, got %d", len(mock.listeners))
	}
}

func TestDeleteListenerByName_NotFound(t *testing.T) {
	_, srv, client := setupMock()
	defer srv.Close()
	rec := NewReconciler(client, testLogger())

	changes, err := rec.DeleteListenerByName(context.Background(), "k8s:nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if changes != 0 {
		t.Errorf("expected 0 changes for nonexistent listener, got %d", changes)
	}
}
