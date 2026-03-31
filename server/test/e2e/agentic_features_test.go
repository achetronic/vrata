// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"encoding/json"
	"net/http"
	"testing"
)

// ─── CEL body access ────────────────────────────────────────────────────────

func TestE2E_Proxy_CELBodyJSON(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("upstream-ok"))
	})
	destID := createDestination(t, "cel-body-dest", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "cel-body-json",
		"match": map[string]any{
			"pathPrefix": "/cel-body-json",
			"methods":    []string{"POST"},
			"cel":        `has(request.body) && has(request.body.json) && request.body.json.method == "tools/call"`,
		},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID}},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	// Matching JSON body.
	code, _, _ := proxyRequest(t, "POST", "/cel-body-json",
		[]byte(`{"method":"tools/call","params":{"name":"add"}}`),
		map[string]string{"Content-Type": "application/json"})
	if code != 200 {
		t.Errorf("matching JSON body: got %d, want 200", code)
	}

	// Non-matching JSON body.
	code2, _, _ := proxyRequest(t, "POST", "/cel-body-json",
		[]byte(`{"method":"initialize"}`),
		map[string]string{"Content-Type": "application/json"})
	if code2 != 404 {
		t.Errorf("non-matching JSON body: got %d, want 404", code2)
	}

	// Non-JSON body.
	code3, _, _ := proxyRequest(t, "POST", "/cel-body-json",
		[]byte(`plain text`),
		map[string]string{"Content-Type": "text/plain"})
	if code3 != 404 {
		t.Errorf("non-JSON body: got %d, want 404", code3)
	}
}

func TestE2E_Proxy_CELBodyRaw(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "cel-body-raw",
		"match": map[string]any{
			"pathPrefix": "/cel-body-raw",
			"cel":        `has(request.body) && request.body.raw.contains("CRITICAL")`,
		},
		"directResponse": map[string]any{"status": 200, "body": "alert-matched"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyRequest(t, "POST", "/cel-body-raw",
		[]byte("event: CRITICAL failure"),
		map[string]string{"Content-Type": "text/plain"})
	if code != 200 || body != "alert-matched" {
		t.Errorf("raw body match: got %d %q", code, body)
	}

	code2, _, _ := proxyRequest(t, "POST", "/cel-body-raw",
		[]byte("event: normal"),
		map[string]string{"Content-Type": "text/plain"})
	if code2 != 404 {
		t.Errorf("raw body mismatch: got %d, want 404", code2)
	}
}

// ─── inlineAuthz middleware ─────────────────────────────────────────────────

func TestE2E_Proxy_InlineAuthz_MCPScenario(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("upstream-ok")) })
	destID := createDestination(t, "authz-mcp-dest", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	// Create inlineAuthz middleware.
	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "mcp-tool-authz",
		"type": "inlineAuthz",
		"inlineAuthz": map[string]any{
			"rules": []map[string]any{
				{"cel": `request.method == "GET" || request.method == "DELETE"`, "action": "allow"},
				{"cel": `has(request.body) && has(request.body.json) && request.body.json.method in ["initialize", "notifications/initialized", "tools/list"]`, "action": "allow"},
				{"cel": `has(request.body) && has(request.body.json) && request.body.json.method == "tools/call" && request.body.json.params.name in ["add", "subtract"]`, "action": "allow"},
			},
			"defaultAction": "deny",
			"denyStatus":    403,
			"denyBody":      `{"error":"tool access denied"}`,
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "authz-mcp-route",
		"match": map[string]any{
			"pathPrefix": "/mcp-authz",
		},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID}},
		},
		"middlewareIDs": []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	tests := []struct {
		name   string
		method string
		body   string
		ct     string
		want   int
	}{
		{"GET allowed", "GET", "", "", 200},
		{"DELETE allowed", "DELETE", "", "", 200},
		{"initialize allowed", "POST", `{"method":"initialize"}`, "application/json", 200},
		{"tools/list allowed", "POST", `{"method":"tools/list"}`, "application/json", 200},
		{"add tool allowed", "POST", `{"method":"tools/call","params":{"name":"add"}}`, "application/json", 200},
		{"subtract tool allowed", "POST", `{"method":"tools/call","params":{"name":"subtract"}}`, "application/json", 200},
		{"evil tool denied", "POST", `{"method":"tools/call","params":{"name":"evil"}}`, "application/json", 403},
		{"unknown method denied", "POST", `{"method":"unknown"}`, "application/json", 403},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := map[string]string{}
			var body []byte
			if tt.body != "" {
				body = []byte(tt.body)
				headers["Content-Type"] = tt.ct
			}
			code, _, respBody := proxyRequest(t, tt.method, "/mcp-authz", body, headers)
			if code != tt.want {
				t.Errorf("got %d, want %d (body: %s)", code, tt.want, respBody)
			}
			if tt.want == 403 {
				var errResp map[string]any
				json.Unmarshal([]byte(respBody), &errResp)
				if errResp["error"] != "tool access denied" {
					t.Errorf("deny body: got %q", respBody)
				}
			}
		})
	}
}

func TestE2E_Proxy_InlineAuthz_DefaultAllow(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("upstream-ok")) })
	destID := createDestination(t, "authz-allow-dest", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "authz-allow-all",
		"type": "inlineAuthz",
		"inlineAuthz": map[string]any{
			"rules": []map[string]any{
				{"cel": `request.path.endsWith("/blocked")`, "action": "deny"},
			},
			"defaultAction": "allow",
			"denyStatus":    403,
			"denyBody":      `{"error":"blocked"}`,
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "authz-allow-route",
		"match": map[string]any{"pathPrefix": "/authz-allow"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID}},
		},
		"middlewareIDs": []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	// Normal path — allowed by default.
	code, _, _ := proxyGet(t, "/authz-allow/foo", nil)
	if code != 200 {
		t.Errorf("normal path should pass, got %d", code)
	}

	// Blocked path — denied by rule (path ends with /blocked, under route prefix).
	code2, _, body2 := proxyGet(t, "/authz-allow/blocked", nil)
	if code2 != 403 {
		t.Errorf("/authz-allow/blocked should be denied, got %d", code2)
	}
	var errResp map[string]any
	json.Unmarshal([]byte(body2), &errResp)
	if errResp["error"] != "blocked" {
		t.Errorf("deny body: got %q", body2)
	}
}
