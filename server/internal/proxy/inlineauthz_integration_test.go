// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/achetronic/vrata/internal/model"
)

func TestRouterInlineAuthzMiddleware_E2E(t *testing.T) {
	router := NewRouter()

	table, err := BuildTable(
		[]model.Route{
			{
				ID:   "r1",
				Name: "authed-route",
				Match: model.MatchRule{PathPrefix: "/protected"},
				Forward: &model.ForwardAction{
					Destinations: []model.DestinationRef{{DestinationID: "d1"}},
				},
				MiddlewareIDs: []string{"mw1"},
			},
		},
		nil,
		[]model.Destination{
			{ID: "d1", Name: "up", Host: "127.0.0.1", Port: 19876},
		},
		[]model.Middleware{
			{
				ID:   "mw1",
				Name: "guard",
				Type: model.MiddlewareTypeInlineAuthz,
				InlineAuthz: &model.InlineAuthzConfig{
					Rules: []model.InlineAuthzRule{
						{CEL: `request.method == "GET"`, Action: "allow"},
					},
					DefaultAction: "deny",
					DenyStatus:    403,
					DenyBody:      `{"error":"blocked"}`,
				},
			},
		},
		nil, 65536,
	)
	if err != nil {
		t.Fatal(err)
	}
	router.SwapTable(table)

	// GET → allowed by rule.
	r := httptest.NewRequest("GET", "/protected", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	// 502 because the upstream isn't running, but NOT 403 = middleware allowed it.
	if w.Code == 403 {
		t.Error("GET should be allowed, got 403")
	}

	// POST → denied by default.
	r2 := httptest.NewRequest("POST", "/protected", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, r2)
	if w2.Code != 403 {
		t.Errorf("POST should be denied, got %d", w2.Code)
	}
}

func TestRouterInlineAuthzBodyRule_E2E(t *testing.T) {
	router := NewRouter()

	table, err := BuildTable(
		[]model.Route{
			{
				ID:   "r1",
				Name: "body-auth",
				Match: model.MatchRule{PathPrefix: "/mcp"},
				DirectResponse: &model.RouteDirectResponse{Status: 200, Body: "ok"},
				MiddlewareIDs: []string{"mw1"},
			},
		},
		nil, nil,
		[]model.Middleware{
			{
				ID:   "mw1",
				Name: "mcp-guard",
				Type: model.MiddlewareTypeInlineAuthz,
				InlineAuthz: &model.InlineAuthzConfig{
					Rules: []model.InlineAuthzRule{
						{CEL: `request.method == "GET"`, Action: "allow"},
						{CEL: `has(request.body) && has(request.body.json) && request.body.json.method in ["initialize", "tools/list"]`, Action: "allow"},
						{CEL: `has(request.body) && has(request.body.json) && request.body.json.method == "tools/call" && request.body.json.params.name in ["add"]`, Action: "allow"},
					},
					DefaultAction: "deny",
					DenyStatus:    403,
				},
			},
		},
		nil, 65536,
	)
	if err != nil {
		t.Fatal(err)
	}
	router.SwapTable(table)

	tests := []struct {
		name   string
		method string
		body   string
		ct     string
		want   int
	}{
		{"GET allowed", "GET", "", "", 200},
		{"initialize allowed", "POST", `{"method":"initialize"}`, "application/json", 200},
		{"tools/list allowed", "POST", `{"method":"tools/list"}`, "application/json", 200},
		{"add allowed", "POST", `{"method":"tools/call","params":{"name":"add"}}`, "application/json", 200},
		{"subtract denied", "POST", `{"method":"tools/call","params":{"name":"subtract"}}`, "application/json", 403},
		{"unknown denied", "POST", `{"method":"unknown"}`, "application/json", 403},
		{"non-json denied", "POST", "plain text", "text/plain", 403},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rq *http.Request
			if tt.body != "" {
				rq = httptest.NewRequest(tt.method, "/mcp", strings.NewReader(tt.body))
				rq.Header.Set("Content-Type", tt.ct)
			} else {
				rq = httptest.NewRequest(tt.method, "/mcp", nil)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, rq)
			if w.Code != tt.want {
				t.Errorf("got %d, want %d (body: %s)", w.Code, tt.want, w.Body.String())
			}
		})
	}
}

func TestRouterSkipWhenWithBody(t *testing.T) {
	router := NewRouter()

	table, err := BuildTable(
		[]model.Route{
			{
				ID:   "r1",
				Name: "skip-body",
				Match: model.MatchRule{PathPrefix: "/"},
				DirectResponse: &model.RouteDirectResponse{Status: 200, Body: "passed"},
				MiddlewareIDs: []string{"mw1"},
				MiddlewareOverrides: map[string]model.MiddlewareOverride{
					"mw1": {
						SkipWhen: []string{`has(request.body) && has(request.body.json) && request.body.json.method == "initialize"`},
					},
				},
			},
		},
		nil, nil,
		[]model.Middleware{
			{
				ID:   "mw1",
				Name: "blocker",
				Type: model.MiddlewareTypeInlineAuthz,
				InlineAuthz: &model.InlineAuthzConfig{
					Rules:         []model.InlineAuthzRule{},
					DefaultAction: "deny",
					DenyStatus:    403,
				},
			},
		},
		nil, 65536,
	)
	if err != nil {
		t.Fatal(err)
	}
	router.SwapTable(table)

	// Body with method=initialize → skipWhen fires → middleware skipped → 200.
	r := httptest.NewRequest("POST", "/test", strings.NewReader(`{"method":"initialize"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Errorf("skipWhen should skip middleware for initialize, got %d", w.Code)
	}

	// Body with method=other → skipWhen doesn't fire → middleware runs → 403.
	r2 := httptest.NewRequest("POST", "/test", strings.NewReader(`{"method":"other"}`))
	r2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, r2)
	if w2.Code != 403 {
		t.Errorf("skipWhen should NOT skip for other method, got %d", w2.Code)
	}
}

func TestRouterOnlyWhenWithBody(t *testing.T) {
	router := NewRouter()

	table, err := BuildTable(
		[]model.Route{
			{
				ID:   "r1",
				Name: "only-body",
				Match: model.MatchRule{PathPrefix: "/"},
				DirectResponse: &model.RouteDirectResponse{Status: 200, Body: "passed"},
				MiddlewareIDs: []string{"mw1"},
				MiddlewareOverrides: map[string]model.MiddlewareOverride{
					"mw1": {
						OnlyWhen: []string{`has(request.body) && has(request.body.json) && request.body.json.method == "tools/call"`},
					},
				},
			},
		},
		nil, nil,
		[]model.Middleware{
			{
				ID:   "mw1",
				Name: "blocker",
				Type: model.MiddlewareTypeInlineAuthz,
				InlineAuthz: &model.InlineAuthzConfig{
					Rules:         []model.InlineAuthzRule{},
					DefaultAction: "deny",
					DenyStatus:    403,
				},
			},
		},
		nil, 65536,
	)
	if err != nil {
		t.Fatal(err)
	}
	router.SwapTable(table)

	// Body with tools/call → onlyWhen matches → middleware runs → 403.
	r := httptest.NewRequest("POST", "/test", strings.NewReader(`{"method":"tools/call"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Errorf("onlyWhen matched → middleware should run → deny, got %d", w.Code)
	}

	// Body with initialize → onlyWhen doesn't match → middleware skipped → 200.
	r2 := httptest.NewRequest("POST", "/test", strings.NewReader(`{"method":"initialize"}`))
	r2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, r2)
	if w2.Code != 200 {
		t.Errorf("onlyWhen not matched → middleware skipped → 200, got %d", w2.Code)
	}
}

func TestRouterMiddlewareOverrideDisabled(t *testing.T) {
	router := NewRouter()

	table, err := BuildTable(
		[]model.Route{
			{
				ID:   "r1",
				Name: "disabled-mw",
				Match: model.MatchRule{PathPrefix: "/"},
				DirectResponse: &model.RouteDirectResponse{Status: 200, Body: "ok"},
				MiddlewareIDs: []string{"mw1"},
				MiddlewareOverrides: map[string]model.MiddlewareOverride{
					"mw1": {Disabled: true},
				},
			},
		},
		nil, nil,
		[]model.Middleware{
			{
				ID:   "mw1",
				Name: "blocker",
				Type: model.MiddlewareTypeInlineAuthz,
				InlineAuthz: &model.InlineAuthzConfig{
					Rules:         []model.InlineAuthzRule{},
					DefaultAction: "deny",
					DenyStatus:    403,
				},
			},
		},
		nil, 65536,
	)
	if err != nil {
		t.Fatal(err)
	}
	router.SwapTable(table)

	r := httptest.NewRequest("POST", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Errorf("disabled middleware should be skipped → 200, got %d", w.Code)
	}
}
