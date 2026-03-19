// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"net/http"
	"strings"
	"testing"
)

func TestE2E_Proxy_DirectResponse(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "maintenance-page",
		"match":          map[string]any{"pathPrefix": "/app/maintenance"},
		"directResponse": map[string]any{"status": 503, "body": "service temporarily unavailable"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyGet(t, "/app/maintenance", nil)
	if code != 503 || body != "service temporarily unavailable" {
		t.Errorf("got %d %q", code, body)
	}
}

func TestE2E_Proxy_Redirect(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name":     "legacy-redirect",
		"match":    map[string]any{"pathPrefix": "/app/old-dashboard"},
		"redirect": map[string]any{"url": "https://app.example.com/dashboard", "code": 301},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, headers, _ := proxyGet(t, "/app/old-dashboard", nil)
	if code != 301 || headers.Get("Location") != "https://app.example.com/dashboard" {
		t.Errorf("got %d location=%q", code, headers.Get("Location"))
	}
}

func TestE2E_Proxy_ForwardToUpstream(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("upstream-ok"))
	})
	destID := createDestination(t, "backend-api", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":    "api-forward",
		"match":   map[string]any{"pathPrefix": "/api/v1/users"},
		"forward": map[string]any{"destinations": []map[string]any{{"destinationId": destID, "weight": 100}}},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyGet(t, "/api/v1/users", nil)
	if code != 200 || body != "upstream-ok" {
		t.Errorf("got %d %q", code, body)
	}
}

func TestE2E_Proxy_GroupRegexComposition(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "catalog-products",
		"match":          map[string]any{"pathPrefix": "/products"},
		"directResponse": map[string]any{"status": 200, "body": "product-list"},
	})
	defer apiDelete(t, "/routes/"+id(route))

	_, group := apiPost(t, "/groups", map[string]any{
		"name":      "i18n-storefront",
		"pathRegex": "/(en|es)",
		"routeIds":  []string{id(route)},
	})
	defer apiDelete(t, "/groups/"+id(group))

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, _ := proxyGet(t, "/es/products", nil)
	if code != 200 {
		t.Errorf("/es/products: %d", code)
	}
	code, _, _ = proxyGet(t, "/en/products", nil)
	if code != 200 {
		t.Errorf("/en/products: %d", code)
	}
	code, _, _ = proxyGet(t, "/fr/products", nil)
	if code != 404 {
		t.Errorf("/fr/products should 404: %d", code)
	}
}

func TestE2E_Proxy_MethodMatch(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "webhook-receiver",
		"match":          map[string]any{"pathPrefix": "/api/webhooks", "methods": []string{"POST"}},
		"directResponse": map[string]any{"status": 200, "body": "accepted"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyRequest(t, "POST", "/api/webhooks", nil, nil)
	if code != 200 || body != "accepted" {
		t.Errorf("POST: %d %q", code, body)
	}
	code, _, _ = proxyGet(t, "/api/webhooks", nil)
	if code != 404 {
		t.Errorf("GET should 404: %d", code)
	}
}

func TestE2E_Proxy_HeaderMatch(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "internal-api",
		"match":          map[string]any{"pathPrefix": "/api/internal", "headers": []map[string]any{{"name": "X-Internal-Token", "value": "secret-123"}}},
		"directResponse": map[string]any{"status": 200, "body": "internal-ok"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, _ := proxyGet(t, "/api/internal", map[string]string{"X-Internal-Token": "secret-123"})
	if code != 200 {
		t.Errorf("with token: %d", code)
	}
	code, _, _ = proxyGet(t, "/api/internal", nil)
	if code != 404 {
		t.Errorf("without token should 404: %d", code)
	}
}

func TestE2E_Proxy_CELMatch(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "admin-only",
		"match":          map[string]any{"pathPrefix": "/admin", "cel": `"x-role" in request.headers && request.headers["x-role"] == "admin"`},
		"directResponse": map[string]any{"status": 200, "body": "admin-panel"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, _ := proxyGet(t, "/admin", map[string]string{"X-Role": "admin"})
	if code != 200 {
		t.Errorf("admin match: %d", code)
	}
	code, _, _ = proxyGet(t, "/admin", nil)
	if code != 404 {
		t.Errorf("no role should 404: %d", code)
	}
}

func TestE2E_Proxy_QueryParamMatch(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "feature-flag",
		"match":          map[string]any{"pathPrefix": "/app/dashboard", "queryParams": []map[string]any{{"name": "beta", "value": "true"}}},
		"directResponse": map[string]any{"status": 200, "body": "beta-dashboard"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, _ := proxyGet(t, "/app/dashboard?beta=true", nil)
	if code != 200 {
		t.Errorf("with beta flag: %d", code)
	}
	code, _, _ = proxyGet(t, "/app/dashboard", nil)
	if code != 404 {
		t.Errorf("without flag should 404: %d", code)
	}
}

func TestE2E_Proxy_GRPCMatch(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "grpc-service",
		"match":          map[string]any{"pathPrefix": "/grpc/orders", "grpc": true},
		"directResponse": map[string]any{"status": 200, "body": "grpc-ok"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, _ := proxyRequest(t, "POST", "/grpc/orders", nil, map[string]string{"Content-Type": "application/grpc"})
	if code != 200 {
		t.Errorf("grpc: %d", code)
	}
	code, _, _ = proxyGet(t, "/grpc/orders", nil)
	if code != 404 {
		t.Errorf("non-grpc should 404: %d", code)
	}
}

func TestE2E_Proxy_HostnameMatch(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "tenant-api",
		"match":          map[string]any{"pathPrefix": "/api/tenant", "hostnames": []string{"acme.example.com"}},
		"directResponse": map[string]any{"status": 200, "body": "tenant-acme"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, _ := proxyRequest(t, "GET", "/api/tenant", nil, map[string]string{"Host": "acme.example.com"})
	if code != 200 {
		t.Errorf("tenant host match: %d", code)
	}
	code, _, _ = proxyGet(t, "/api/tenant", nil)
	if code != 404 {
		t.Errorf("wrong host should 404: %d", code)
	}
}

func TestE2E_Proxy_PathRewriteRegex(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("path=" + r.URL.Path))
	})
	destID := createDestination(t, "backend-rewrite", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "api-version-strip",
		"match": map[string]any{"pathPrefix": "/api/v2"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
			"rewrite":      map[string]any{"pathRegex": map[string]any{"pattern": "^/api/v2(.*)", "substitution": "/internal$1"}},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyGet(t, "/api/v2/orders", nil)
	if code != 200 || !strings.Contains(body, "path=/internal/orders") {
		t.Errorf("regex rewrite: %d %q", code, body)
	}
}
