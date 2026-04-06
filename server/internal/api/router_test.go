// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/achetronic/vrata/internal/config"
	memstore "github.com/achetronic/vrata/internal/store/memory"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestRouterNoAuthConfigAllowsAll(t *testing.T) {
	st := memstore.New()
	router := NewRouter(st, testLogger(), nil, nil)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/routes", nil))

	if w.Code != 200 {
		t.Errorf("expected 200 without auth config, got %d", w.Code)
	}
}

func TestRouterAuthRejectsMissingKey(t *testing.T) {
	st := memstore.New()
	keys := []config.APIKeyEntry{{Name: "test", Key: "secret"}}
	router := NewRouter(st, testLogger(), nil, keys)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/routes", nil))

	if w.Code != 401 {
		t.Errorf("expected 401 without auth header, got %d", w.Code)
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["error"] != "missing authorization header" {
		t.Errorf("unexpected error: %s", body["error"])
	}
}

func TestRouterAuthRejectsInvalidKey(t *testing.T) {
	st := memstore.New()
	keys := []config.APIKeyEntry{{Name: "test", Key: "secret"}}
	router := NewRouter(st, testLogger(), nil, keys)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/routes", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	router.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("expected 401 for invalid key, got %d", w.Code)
	}
}

func TestRouterAuthRejectsBadScheme(t *testing.T) {
	st := memstore.New()
	keys := []config.APIKeyEntry{{Name: "test", Key: "secret"}}
	router := NewRouter(st, testLogger(), nil, keys)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/routes", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	router.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("expected 401 for bad scheme, got %d", w.Code)
	}
}

func TestRouterAuthAcceptsValidKey(t *testing.T) {
	st := memstore.New()
	keys := []config.APIKeyEntry{{Name: "test", Key: "secret"}}
	router := NewRouter(st, testLogger(), nil, keys)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/routes", nil)
	req.Header.Set("Authorization", "Bearer secret")
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200 with valid key, got %d", w.Code)
	}
}

func TestRouterAuthMultipleKeys(t *testing.T) {
	st := memstore.New()
	keys := []config.APIKeyEntry{
		{Name: "proxy", Key: "proxy-key"},
		{Name: "operator", Key: "operator-key"},
	}
	router := NewRouter(st, testLogger(), nil, keys)

	for _, k := range keys {
		t.Run(k.Name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/api/v1/routes", nil)
			req.Header.Set("Authorization", "Bearer "+k.Key)
			router.ServeHTTP(w, req)
			if w.Code != 200 {
				t.Errorf("expected 200 for key %q, got %d", k.Name, w.Code)
			}
		})
	}
}

func TestRouterAuthProtectsAllEndpoints(t *testing.T) {
	st := memstore.New()
	keys := []config.APIKeyEntry{{Name: "test", Key: "secret"}}
	router := NewRouter(st, testLogger(), nil, keys)

	endpoints := []struct {
		method string
		path   string
	}{
		{"GET", "/api/v1/routes"},
		{"GET", "/api/v1/groups"},
		{"GET", "/api/v1/destinations"},
		{"GET", "/api/v1/listeners"},
		{"GET", "/api/v1/middlewares"},
		{"GET", "/api/v1/snapshots"},
		{"GET", "/api/v1/debug/config"},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(ep.method, ep.path, nil))
			if w.Code != 401 {
				t.Errorf("expected 401 without auth for %s %s, got %d", ep.method, ep.path, w.Code)
			}
		})
	}
}
