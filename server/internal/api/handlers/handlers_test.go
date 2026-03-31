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

func newDeps(t *testing.T) (*Dependencies, *memstore.Store) {
	t.Helper()
	st := memstore.New()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return &Dependencies{Store: st, Logger: logger}, st
}

func jsonBody(t *testing.T, v any) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(b)
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(w.Body).Decode(&v); err != nil {
		t.Fatalf("decode: %v (body: %s)", err, w.Body.String())
	}
	return v
}

// ─── Routes ─────────────────────────────────────────────────────────────────

func TestRouteListEmpty(t *testing.T) {
	d, _ := newDeps(t)
	w := httptest.NewRecorder()
	d.ListRoutes(w, httptest.NewRequest("GET", "/api/v1/routes", nil))
	if w.Code != 200 {
		t.Fatalf("got %d", w.Code)
	}
	routes := decode[[]model.Route](t, w)
	if len(routes) != 0 {
		t.Errorf("expected empty, got %d", len(routes))
	}
}

func TestRouteCreateAndGet(t *testing.T) {
	d, _ := newDeps(t)
	body := model.Route{Name: "r1", Match: model.MatchRule{PathPrefix: "/"},
		DirectResponse: &model.RouteDirectResponse{Status: 200}}

	w := httptest.NewRecorder()
	d.CreateRoute(w, httptest.NewRequest("POST", "/", jsonBody(t, body)))
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	created := decode[model.Route](t, w)
	if created.ID == "" {
		t.Error("expected auto-generated ID")
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.SetPathValue("routeId", created.ID)
	w2 := httptest.NewRecorder()
	d.GetRoute(w2, req)
	if w2.Code != 200 {
		t.Fatalf("get: %d", w2.Code)
	}
}

func TestRouteCreateConflictingAction(t *testing.T) {
	d, _ := newDeps(t)
	body := model.Route{Name: "bad",
		Forward:        &model.ForwardAction{},
		DirectResponse: &model.RouteDirectResponse{Status: 200},
	}
	w := httptest.NewRecorder()
	d.CreateRoute(w, httptest.NewRequest("POST", "/", jsonBody(t, body)))
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouteCreateInvalidWeights(t *testing.T) {
	d, _ := newDeps(t)
	body := model.Route{
		Name: "badweights",
		Forward: &model.ForwardAction{
			Destinations: []model.DestinationRef{
				{DestinationID: "d1", Weight: 60},
				{DestinationID: "d2", Weight: 60},
			},
		},
	}
	w := httptest.NewRecorder()
	d.CreateRoute(w, httptest.NewRequest("POST", "/", jsonBody(t, body)))
	if w.Code != 400 {
		t.Fatalf("expected 400 for weights not summing to 100, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouteCreateSingleDestinationNoWeightCheck(t *testing.T) {
	d, _ := newDeps(t)
	body := model.Route{
		Name: "singledest",
		Forward: &model.ForwardAction{
			Destinations: []model.DestinationRef{
				{DestinationID: "d1", Weight: 50},
			},
		},
	}
	w := httptest.NewRecorder()
	d.CreateRoute(w, httptest.NewRequest("POST", "/", jsonBody(t, body)))
	if w.Code != 201 {
		t.Fatalf("expected 201 for single destination (no weight check), got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouteCreateNoAction(t *testing.T) {
	d, _ := newDeps(t)
	body := model.Route{Name: "empty"}
	w := httptest.NewRecorder()
	d.CreateRoute(w, httptest.NewRequest("POST", "/", jsonBody(t, body)))
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestRouteUpdate(t *testing.T) {
	d, st := newDeps(t)
	ctx := context.Background()
	st.SaveRoute(ctx, model.Route{ID: "r1", Name: "old", DirectResponse: &model.RouteDirectResponse{Status: 200}})

	body := model.Route{Name: "new", DirectResponse: &model.RouteDirectResponse{Status: 201}}
	req := httptest.NewRequest("PUT", "/", jsonBody(t, body))
	req.SetPathValue("routeId", "r1")
	w := httptest.NewRecorder()
	d.UpdateRoute(w, req)
	if w.Code != 200 {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	updated := decode[model.Route](t, w)
	if updated.ID != "r1" {
		t.Error("ID should be forced from path")
	}
	if updated.Name != "new" {
		t.Errorf("expected name 'new', got %q", updated.Name)
	}
}

func TestRouteUpdateNotFound(t *testing.T) {
	d, _ := newDeps(t)
	req := httptest.NewRequest("PUT", "/", jsonBody(t, model.Route{DirectResponse: &model.RouteDirectResponse{Status: 200}}))
	req.SetPathValue("routeId", "nonexistent")
	w := httptest.NewRecorder()
	d.UpdateRoute(w, req)
	if w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestRouteDelete(t *testing.T) {
	d, st := newDeps(t)
	st.SaveRoute(context.Background(), model.Route{ID: "r1", Name: "x"})

	req := httptest.NewRequest("DELETE", "/", nil)
	req.SetPathValue("routeId", "r1")
	w := httptest.NewRecorder()
	d.DeleteRoute(w, req)
	if w.Code != 204 {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestRouteDeleteNotFound(t *testing.T) {
	d, _ := newDeps(t)
	req := httptest.NewRequest("DELETE", "/", nil)
	req.SetPathValue("routeId", "nope")
	w := httptest.NewRecorder()
	d.DeleteRoute(w, req)
	if w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// ─── Groups ─────────────────────────────────────────────────────────────────

func TestGroupCRUD(t *testing.T) {
	d, _ := newDeps(t)

	// List empty
	w := httptest.NewRecorder()
	d.ListGroups(w, httptest.NewRequest("GET", "/", nil))
	if len(decode[[]model.RouteGroup](t, w)) != 0 {
		t.Error("expected empty")
	}

	// Create
	w = httptest.NewRecorder()
	d.CreateGroup(w, httptest.NewRequest("POST", "/", jsonBody(t, model.RouteGroup{Name: "g1"})))
	if w.Code != 201 {
		t.Fatalf("create: %d", w.Code)
	}
	created := decode[model.RouteGroup](t, w)
	if created.ID == "" {
		t.Error("expected auto ID")
	}

	// Get
	req := httptest.NewRequest("GET", "/", nil)
	req.SetPathValue("groupId", created.ID)
	w = httptest.NewRecorder()
	d.GetGroup(w, req)
	if w.Code != 200 {
		t.Fatalf("get: %d", w.Code)
	}

	// Update
	req = httptest.NewRequest("PUT", "/", jsonBody(t, model.RouteGroup{Name: "updated"}))
	req.SetPathValue("groupId", created.ID)
	w = httptest.NewRecorder()
	d.UpdateGroup(w, req)
	if w.Code != 200 {
		t.Fatalf("update: %d", w.Code)
	}

	// Delete
	req = httptest.NewRequest("DELETE", "/", nil)
	req.SetPathValue("groupId", created.ID)
	w = httptest.NewRecorder()
	d.DeleteGroup(w, req)
	if w.Code != 204 {
		t.Fatalf("delete: %d", w.Code)
	}

	// Delete not found
	req = httptest.NewRequest("DELETE", "/", nil)
	req.SetPathValue("groupId", "nope")
	w = httptest.NewRecorder()
	d.DeleteGroup(w, req)
	if w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// ─── Destinations ───────────────────────────────────────────────────────────

func TestDestinationCRUD(t *testing.T) {
	d, _ := newDeps(t)

	w := httptest.NewRecorder()
	d.ListDestinations(w, httptest.NewRequest("GET", "/", nil))
	if len(decode[[]model.Destination](t, w)) != 0 {
		t.Error("expected empty")
	}

	w = httptest.NewRecorder()
	d.CreateDestination(w, httptest.NewRequest("POST", "/", jsonBody(t, model.Destination{Name: "d1", Host: "10.0.0.1", Port: 80})))
	if w.Code != 201 {
		t.Fatalf("create: %d", w.Code)
	}
	created := decode[model.Destination](t, w)

	req := httptest.NewRequest("GET", "/", nil)
	req.SetPathValue("destinationId", created.ID)
	w = httptest.NewRecorder()
	d.GetDestination(w, req)
	if w.Code != 200 {
		t.Fatalf("get: %d", w.Code)
	}

	req = httptest.NewRequest("PUT", "/", jsonBody(t, model.Destination{Name: "updated", Host: "10.0.0.2", Port: 8080}))
	req.SetPathValue("destinationId", created.ID)
	w = httptest.NewRecorder()
	d.UpdateDestination(w, req)
	if w.Code != 200 {
		t.Fatalf("update: %d", w.Code)
	}
	got := decode[model.Destination](t, w)
	if got.Name != "updated" {
		t.Errorf("expected updated, got %q", got.Name)
	}

	req = httptest.NewRequest("DELETE", "/", nil)
	req.SetPathValue("destinationId", created.ID)
	w = httptest.NewRecorder()
	d.DeleteDestination(w, req)
	if w.Code != 204 {
		t.Fatalf("delete: %d", w.Code)
	}
}

// ─── Listeners ──────────────────────────────────────────────────────────────

func TestListenerCRUD(t *testing.T) {
	d, _ := newDeps(t)

	// Create with default address
	w := httptest.NewRecorder()
	d.CreateListener(w, httptest.NewRequest("POST", "/", jsonBody(t, model.Listener{Name: "main", Port: 3000})))
	if w.Code != 201 {
		t.Fatalf("create: %d", w.Code)
	}
	created := decode[model.Listener](t, w)
	if created.Address != "0.0.0.0" {
		t.Errorf("expected default address 0.0.0.0, got %q", created.Address)
	}

	req := httptest.NewRequest("PUT", "/", jsonBody(t, model.Listener{Name: "updated", Port: 8080}))
	req.SetPathValue("listenerId", created.ID)
	w = httptest.NewRecorder()
	d.UpdateListener(w, req)
	if w.Code != 200 {
		t.Fatalf("update: %d", w.Code)
	}
	got := decode[model.Listener](t, w)
	if got.Address != "0.0.0.0" {
		t.Errorf("expected default address on update, got %q", got.Address)
	}

	req = httptest.NewRequest("DELETE", "/", nil)
	req.SetPathValue("listenerId", created.ID)
	w = httptest.NewRecorder()
	d.DeleteListener(w, req)
	if w.Code != 204 {
		t.Fatalf("delete: %d", w.Code)
	}
}

// ─── Middlewares ─────────────────────────────────────────────────────────────

func TestMiddlewareCRUD(t *testing.T) {
	d, _ := newDeps(t)

	w := httptest.NewRecorder()
	d.CreateMiddleware(w, httptest.NewRequest("POST", "/", jsonBody(t, model.Middleware{Name: "cors", Type: model.MiddlewareTypeCORS})))
	if w.Code != 201 {
		t.Fatalf("create: %d", w.Code)
	}
	created := decode[model.Middleware](t, w)

	req := httptest.NewRequest("PUT", "/", jsonBody(t, model.Middleware{Name: "updated", Type: model.MiddlewareTypeCORS}))
	req.SetPathValue("middlewareId", created.ID)
	w = httptest.NewRecorder()
	d.UpdateMiddleware(w, req)
	if w.Code != 200 {
		t.Fatalf("update: %d", w.Code)
	}

	req = httptest.NewRequest("DELETE", "/", nil)
	req.SetPathValue("middlewareId", created.ID)
	w = httptest.NewRecorder()
	d.DeleteMiddleware(w, req)
	if w.Code != 204 {
		t.Fatalf("delete: %d", w.Code)
	}
}

// ─── Debug ──────────────────────────────────────────────────────────────────

func TestConfigDump(t *testing.T) {
	d, st := newDeps(t)
	ctx := context.Background()
	st.SaveListener(ctx, model.Listener{ID: "l1", Name: "main", Port: 3000})
	st.SaveRoute(ctx, model.Route{ID: "r1", Name: "test", DirectResponse: &model.RouteDirectResponse{Status: 200}})
	st.SaveDestination(ctx, model.Destination{ID: "d1", Name: "up", Host: "10.0.0.1", Port: 80})

	w := httptest.NewRecorder()
	d.GetConfigDump(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 {
		t.Fatalf("config dump: %d", w.Code)
	}

	var dump map[string]json.RawMessage
	json.NewDecoder(w.Body).Decode(&dump)
	for _, key := range []string{"listeners", "routes", "destinations", "groups", "middlewares"} {
		if _, ok := dump[key]; !ok {
			t.Errorf("missing key %q in config dump", key)
		}
	}
}

// ─── Invalid JSON body ──────────────────────────────────────────────────────

func TestCreateInvalidJSON(t *testing.T) {
	d, _ := newDeps(t)

	tests := []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"route", d.CreateRoute},
		{"group", d.CreateGroup},
		{"destination", d.CreateDestination},
		{"listener", d.CreateListener},
		{"middleware", d.CreateMiddleware},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tt.handler(w, httptest.NewRequest("POST", "/", bytes.NewReader([]byte("not json"))))
			if w.Code != 400 {
				t.Errorf("expected 400, got %d", w.Code)
			}
		})
	}
}

// ─── Listener clientAuth validation ─────────────────────────────────────────

func TestListenerValidation_ClientAuthUnknownMode(t *testing.T) {
	d, _ := newDeps(t)
	w := httptest.NewRecorder()
	d.CreateListener(w, httptest.NewRequest("POST", "/", jsonBody(t, map[string]any{
		"name": "test", "port": 8443,
		"tls": map[string]any{
			"certPath": "/cert.pem", "keyPath": "/key.pem",
			"clientAuth": map[string]any{"mode": "bogus"},
		},
	})))
	if w.Code != 400 {
		t.Errorf("unknown clientAuth mode should be 400, got %d", w.Code)
	}
}

func TestListenerValidation_ClientAuthRequireNoCA(t *testing.T) {
	d, _ := newDeps(t)
	w := httptest.NewRecorder()
	d.CreateListener(w, httptest.NewRequest("POST", "/", jsonBody(t, map[string]any{
		"name": "test", "port": 8443,
		"tls": map[string]any{
			"certPath": "/cert.pem", "keyPath": "/key.pem",
			"clientAuth": map[string]any{"mode": "require"},
		},
	})))
	if w.Code != 400 {
		t.Errorf("require without caFile should be 400, got %d", w.Code)
	}
}

func TestListenerValidation_ClientAuthOptionalNoCA(t *testing.T) {
	d, _ := newDeps(t)
	w := httptest.NewRecorder()
	d.CreateListener(w, httptest.NewRequest("POST", "/", jsonBody(t, map[string]any{
		"name": "test", "port": 8443,
		"tls": map[string]any{
			"certPath": "/cert.pem", "keyPath": "/key.pem",
			"clientAuth": map[string]any{"mode": "optional"},
		},
	})))
	if w.Code != 400 {
		t.Errorf("optional without caFile should be 400, got %d", w.Code)
	}
}

func TestListenerValidation_ClientAuthRequireWithCA(t *testing.T) {
	d, _ := newDeps(t)
	w := httptest.NewRecorder()
	d.CreateListener(w, httptest.NewRequest("POST", "/", jsonBody(t, map[string]any{
		"name": "test", "port": 8443,
		"tls": map[string]any{
			"certPath": "/cert.pem", "keyPath": "/key.pem",
			"clientAuth": map[string]any{"mode": "require", "caFile": "/ca.pem"},
		},
	})))
	if w.Code != 201 {
		t.Errorf("valid clientAuth should be 201, got %d (body: %s)", w.Code, w.Body.String())
	}
}

func TestListenerValidation_NoClientAuth(t *testing.T) {
	d, _ := newDeps(t)
	w := httptest.NewRecorder()
	d.CreateListener(w, httptest.NewRequest("POST", "/", jsonBody(t, map[string]any{
		"name": "test", "port": 8080,
	})))
	if w.Code != 201 {
		t.Errorf("no TLS should be 201, got %d", w.Code)
	}
}

func TestListenerValidation_ClientAuthNoneMode(t *testing.T) {
	d, _ := newDeps(t)
	w := httptest.NewRecorder()
	d.CreateListener(w, httptest.NewRequest("POST", "/", jsonBody(t, map[string]any{
		"name": "test", "port": 8443,
		"tls": map[string]any{
			"certPath": "/cert.pem", "keyPath": "/key.pem",
			"clientAuth": map[string]any{"mode": "none"},
		},
	})))
	if w.Code != 201 {
		t.Errorf("mode none should be 201, got %d", w.Code)
	}
}

// ─── Middleware inlineAuthz validation ───────────────────────────────────────

func TestMiddlewareValidation_InlineAuthzNoConfig(t *testing.T) {
	d, _ := newDeps(t)
	w := httptest.NewRecorder()
	d.CreateMiddleware(w, httptest.NewRequest("POST", "/", jsonBody(t, map[string]any{
		"name": "test", "type": "inlineAuthz",
	})))
	if w.Code != 400 {
		t.Errorf("inlineAuthz without config should be 400, got %d", w.Code)
	}
}

func TestMiddlewareValidation_InlineAuthzEmptyRules(t *testing.T) {
	d, _ := newDeps(t)
	w := httptest.NewRecorder()
	d.CreateMiddleware(w, httptest.NewRequest("POST", "/", jsonBody(t, map[string]any{
		"name": "test", "type": "inlineAuthz",
		"inlineAuthz": map[string]any{
			"rules":         []any{},
			"defaultAction": "deny",
		},
	})))
	if w.Code != 400 {
		t.Errorf("empty rules should be 400, got %d", w.Code)
	}
}

func TestMiddlewareValidation_InlineAuthzBadAction(t *testing.T) {
	d, _ := newDeps(t)
	w := httptest.NewRecorder()
	d.CreateMiddleware(w, httptest.NewRequest("POST", "/", jsonBody(t, map[string]any{
		"name": "test", "type": "inlineAuthz",
		"inlineAuthz": map[string]any{
			"rules": []map[string]any{
				{"cel": `request.method == "GET"`, "action": "maybe"},
			},
			"defaultAction": "deny",
		},
	})))
	if w.Code != 400 {
		t.Errorf("bad action should be 400, got %d", w.Code)
	}
}

func TestMiddlewareValidation_InlineAuthzBadCEL(t *testing.T) {
	d, _ := newDeps(t)
	w := httptest.NewRecorder()
	d.CreateMiddleware(w, httptest.NewRequest("POST", "/", jsonBody(t, map[string]any{
		"name": "test", "type": "inlineAuthz",
		"inlineAuthz": map[string]any{
			"rules": []map[string]any{
				{"cel": "not valid cel !!!", "action": "allow"},
			},
			"defaultAction": "deny",
		},
	})))
	if w.Code != 400 {
		t.Errorf("bad CEL should be 400, got %d", w.Code)
	}
}

func TestMiddlewareValidation_InlineAuthzEmptyCEL(t *testing.T) {
	d, _ := newDeps(t)
	w := httptest.NewRecorder()
	d.CreateMiddleware(w, httptest.NewRequest("POST", "/", jsonBody(t, map[string]any{
		"name": "test", "type": "inlineAuthz",
		"inlineAuthz": map[string]any{
			"rules": []map[string]any{
				{"cel": "", "action": "allow"},
			},
			"defaultAction": "deny",
		},
	})))
	if w.Code != 400 {
		t.Errorf("empty CEL should be 400, got %d", w.Code)
	}
}

func TestMiddlewareValidation_InlineAuthzBadDefaultAction(t *testing.T) {
	d, _ := newDeps(t)
	w := httptest.NewRecorder()
	d.CreateMiddleware(w, httptest.NewRequest("POST", "/", jsonBody(t, map[string]any{
		"name": "test", "type": "inlineAuthz",
		"inlineAuthz": map[string]any{
			"rules": []map[string]any{
				{"cel": `request.method == "GET"`, "action": "allow"},
			},
			"defaultAction": "maybe",
		},
	})))
	if w.Code != 400 {
		t.Errorf("bad defaultAction should be 400, got %d", w.Code)
	}
}

func TestMiddlewareValidation_InlineAuthzValid(t *testing.T) {
	d, _ := newDeps(t)
	w := httptest.NewRecorder()
	d.CreateMiddleware(w, httptest.NewRequest("POST", "/", jsonBody(t, map[string]any{
		"name": "test", "type": "inlineAuthz",
		"inlineAuthz": map[string]any{
			"rules": []map[string]any{
				{"cel": `request.method == "GET"`, "action": "allow"},
				{"cel": `request.path == "/admin"`, "action": "deny"},
			},
			"defaultAction": "deny",
			"denyStatus":    403,
		},
	})))
	if w.Code != 201 {
		t.Errorf("valid inlineAuthz should be 201, got %d (body: %s)", w.Code, w.Body.String())
	}
}

func TestMiddlewareValidation_NonInlineAuthzPassesThrough(t *testing.T) {
	d, _ := newDeps(t)
	w := httptest.NewRecorder()
	d.CreateMiddleware(w, httptest.NewRequest("POST", "/", jsonBody(t, map[string]any{
		"name": "test", "type": "cors",
		"cors": map[string]any{"allowOrigins": []map[string]any{{"value": "*"}}},
	})))
	if w.Code != 201 {
		t.Errorf("cors should pass validation, got %d", w.Code)
	}
}
