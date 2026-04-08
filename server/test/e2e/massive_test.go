// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// 1. API CRUD — exhaustive validation & edge cases
// ─────────────────────────────────────────────────────────────────────────────

func TestMassive_API_RouteActionValidation(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
		want int
	}{
		{"no action", map[string]any{"name": "bad", "match": map[string]any{"pathPrefix": "/x"}}, 400},
		{"two actions", map[string]any{"name": "bad", "match": map[string]any{"pathPrefix": "/x"},
			"directResponse": map[string]any{"status": 200}, "redirect": map[string]any{"url": "http://x"}}, 400},
		{"three actions", map[string]any{"name": "bad", "match": map[string]any{"pathPrefix": "/x"},
			"directResponse": map[string]any{"status": 200}, "redirect": map[string]any{"url": "http://x"},
			"forward": map[string]any{"destinations": []map[string]any{{"destinationId": "fake", "weight": 100}}}}, 400},
		{"valid direct", map[string]any{"name": "ok", "match": map[string]any{"pathPrefix": "/x"},
			"directResponse": map[string]any{"status": 200, "body": "ok"}}, 201},
		{"valid redirect", map[string]any{"name": "ok", "match": map[string]any{"pathPrefix": "/y"},
			"redirect": map[string]any{"url": "http://example.com", "code": 301}}, 201},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, result := apiPost(t, "/routes", tt.body)
			if code != tt.want {
				t.Errorf("expected %d, got %d: %v", tt.want, code, result)
			}
			if code == 201 {
				apiDelete(t, "/routes/"+id(result))
			}
		})
	}
}

func TestMassive_API_DestinationWeightValidation(t *testing.T) {
	d1 := createDestination(t, "w-dest1", "127.0.0.1", 9999)
	defer apiDelete(t, "/destinations/"+d1)
	d2 := createDestination(t, "w-dest2", "127.0.0.1", 9998)
	defer apiDelete(t, "/destinations/"+d2)

	tests := []struct {
		name    string
		dests   []map[string]any
		wantErr bool
	}{
		{"single dest no weight", []map[string]any{{"destinationId": d1, "weight": 0}}, false},
		{"two dests sum 100", []map[string]any{{"destinationId": d1, "weight": 60}, {"destinationId": d2, "weight": 40}}, false},
		{"two dests sum 99", []map[string]any{{"destinationId": d1, "weight": 59}, {"destinationId": d2, "weight": 40}}, true},
		{"two dests sum 101", []map[string]any{{"destinationId": d1, "weight": 61}, {"destinationId": d2, "weight": 40}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, result := apiPost(t, "/routes", map[string]any{
				"name": "weight-test", "match": map[string]any{"pathPrefix": "/wt"},
				"forward": map[string]any{"destinations": tt.dests},
			})
			if tt.wantErr && code != 400 {
				t.Errorf("expected 400, got %d: %v", code, result)
			}
			if !tt.wantErr && code != 201 {
				t.Errorf("expected 201, got %d: %v", code, result)
			}
			if code == 201 {
				apiDelete(t, "/routes/"+id(result))
			}
		})
	}
}

func TestMassive_API_InvalidJSON(t *testing.T) {
	endpoints := []string{"/routes", "/groups", "/destinations", "/listeners", "/middlewares", "/secrets"}
	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			resp, err := http.Post(apiBase+ep, "application/json", strings.NewReader("{invalid"))
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != 400 {
				t.Errorf("expected 400 for invalid JSON on %s, got %d", ep, resp.StatusCode)
			}
		})
	}
}

func TestMassive_API_NotFound(t *testing.T) {
	endpoints := []struct {
		method string
		path   string
	}{
		{"GET", "/routes/nonexistent-id"},
		{"GET", "/groups/nonexistent-id"},
		{"GET", "/destinations/nonexistent-id"},
		{"GET", "/listeners/nonexistent-id"},
		{"GET", "/middlewares/nonexistent-id"},
		{"GET", "/secrets/nonexistent-id"},
		{"GET", "/snapshots/nonexistent-id"},
		{"DELETE", "/routes/nonexistent-id"},
		{"DELETE", "/groups/nonexistent-id"},
		{"DELETE", "/destinations/nonexistent-id"},
		{"DELETE", "/listeners/nonexistent-id"},
		{"DELETE", "/middlewares/nonexistent-id"},
		{"DELETE", "/secrets/nonexistent-id"},
		{"DELETE", "/snapshots/nonexistent-id"},
	}
	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			req, _ := http.NewRequest(ep.method, apiBase+ep.path, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != 404 {
				t.Errorf("expected 404, got %d", resp.StatusCode)
			}
		})
	}
}

func TestMassive_API_ListenerValidation(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
		want int
	}{
		{"missing name", map[string]any{"port": 9900}, 400},
		{"missing port", map[string]any{"name": "bad"}, 400},
		{"zero port", map[string]any{"name": "bad", "port": 0}, 400},
		{"invalid clientAuth mode", map[string]any{"name": "bad", "port": 9900,
			"tls": map[string]any{"cert": "x", "key": "y", "clientAuth": map[string]any{"mode": "bogus"}}}, 400},
		{"clientAuth require without CA", map[string]any{"name": "bad", "port": 9900,
			"tls": map[string]any{"cert": "x", "key": "y", "clientAuth": map[string]any{"mode": "require"}}}, 400},
		{"valid minimal", map[string]any{"name": "good", "port": 9900}, 201},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, result := apiPost(t, "/listeners", tt.body)
			if code != tt.want {
				t.Errorf("expected %d, got %d: %v", tt.want, code, result)
			}
			if code == 201 {
				apiDelete(t, "/listeners/"+id(result))
			}
		})
	}
}

func TestMassive_API_MiddlewareTypeValidation(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
		want int
	}{
		{"missing type", map[string]any{"name": "bad"}, 400},
		{"unknown type", map[string]any{"name": "bad", "type": "nonexistent"}, 400},
		{"cors ok", map[string]any{"name": "ok", "type": "cors",
			"cors": map[string]any{"allowMethods": []string{"GET"}}}, 201},
		{"rateLimit ok", map[string]any{"name": "ok", "type": "rateLimit",
			"rateLimit": map[string]any{"requestsPerSecond": 10}}, 201},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, result := apiPost(t, "/middlewares", tt.body)
			if code != tt.want {
				t.Errorf("expected %d, got %d: %v", tt.want, code, result)
			}
			if code == 201 {
				apiDelete(t, "/middlewares/"+id(result))
			}
		})
	}
}

func TestMassive_API_SecretCRUD(t *testing.T) {
	code, sec := apiPost(t, "/secrets", map[string]any{"name": "test-secret", "value": "super-secret-value"})
	if code != 201 {
		t.Fatalf("create: %d", code)
	}
	secID := id(sec)
	defer apiDelete(t, "/secrets/"+secID)

	// Get returns value.
	getCode, getData := apiGet(t, "/secrets/"+secID)
	if getCode != 200 {
		t.Fatalf("get: %d", getCode)
	}
	var getSec map[string]any
	json.Unmarshal(getData, &getSec)
	if getSec["value"] != "super-secret-value" {
		t.Errorf("get value mismatch: %v", getSec["value"])
	}

	// List does NOT return value.
	listCode, listData := apiGet(t, "/secrets")
	if listCode != 200 {
		t.Fatalf("list: %d", listCode)
	}
	if strings.Contains(string(listData), "super-secret-value") {
		t.Error("list endpoint should not expose secret values")
	}

	// Update.
	putCode, _ := apiPut(t, "/secrets/"+secID, map[string]any{"name": "test-secret-updated", "value": "new-value"})
	if putCode != 200 {
		t.Errorf("update: %d", putCode)
	}

	// Delete.
	delCode := apiDelete(t, "/secrets/"+secID)
	if delCode != 204 {
		t.Errorf("delete: %d", delCode)
	}
}

func TestMassive_API_UpdateNonexistent(t *testing.T) {
	endpoints := []string{"/routes/fake-id", "/groups/fake-id", "/destinations/fake-id",
		"/listeners/fake-id", "/middlewares/fake-id", "/secrets/fake-id"}
	for _, ep := range endpoints {
		t.Run("PUT "+ep, func(t *testing.T) {
			code, _ := apiPut(t, ep, map[string]any{"name": "whatever"})
			if code != 404 && code != 400 {
				t.Errorf("expected 404 or 400, got %d", code)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. Snapshot lifecycle
// ─────────────────────────────────────────────────────────────────────────────

func TestMassive_Snapshot_CreateActivateDeleteCycle(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "snap-test", "match": map[string]any{"pathPrefix": "/snap-test"},
		"directResponse": map[string]any{"status": 200, "body": "snap"},
	})
	defer apiDelete(t, "/routes/"+id(route))

	// Create.
	code, snap := apiPost(t, "/snapshots", map[string]any{"name": "massive-test-snap"})
	if code != 201 {
		t.Fatalf("create snapshot: %d %v", code, snap)
	}
	snapID := id(snap)

	// List shows it.
	listCode, listData := apiGet(t, "/snapshots")
	if listCode != 200 {
		t.Fatalf("list: %d", listCode)
	}
	if !strings.Contains(string(listData), snapID) {
		t.Error("snapshot not in list")
	}

	// Activate.
	actCode, _ := apiPost(t, "/snapshots/"+snapID+"/activate", nil)
	if actCode != 200 {
		t.Fatalf("activate: %d", actCode)
	}

	// List shows active flag.
	_, listData2 := apiGet(t, "/snapshots")
	if !strings.Contains(string(listData2), `"active":true`) {
		t.Error("active flag not set in list")
	}

	// Delete clears active.
	delCode := apiDelete(t, "/snapshots/"+snapID)
	if delCode != 204 {
		t.Errorf("delete: %d", delCode)
	}
}

func TestMassive_Snapshot_ActivateNonexistent(t *testing.T) {
	code, _ := apiPost(t, "/snapshots/nonexistent-id/activate", nil)
	if code != 404 {
		t.Errorf("expected 404, got %d", code)
	}
}

func TestMassive_Snapshot_ValidationWarnings(t *testing.T) {
	// Create a route with a bad regex.
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "bad-regex", "match": map[string]any{"pathRegex": "[invalid"},
		"directResponse": map[string]any{"status": 200},
	})
	defer apiDelete(t, "/routes/"+id(route))

	code, snap := apiPost(t, "/snapshots", map[string]any{"name": "warn-test"})
	if code != 201 {
		t.Fatalf("create: %d %v", code, snap)
	}
	defer apiDelete(t, "/snapshots/"+id(snap))

	// Response should contain warnings.
	if snap["warnings"] == nil {
		t.Error("expected warnings for bad regex, got none")
	}
}

func TestMassive_Snapshot_SecretResolution(t *testing.T) {
	_, sec := apiPost(t, "/secrets", map[string]any{"name": "e2e-sec", "value": "resolved-val"})
	secID := id(sec)
	defer apiDelete(t, "/secrets/"+secID)

	// Create listener with secret reference.
	_, listener := apiPost(t, "/listeners", map[string]any{
		"name": "sec-listener", "port": 19999, "serverName": fmt.Sprintf("{{secret:value:%s}}", secID),
	})
	defer apiDelete(t, "/listeners/"+id(listener))

	code, snap := apiPost(t, "/snapshots", map[string]any{"name": "sec-snap"})
	if code != 201 {
		t.Fatalf("create: %d %v", code, snap)
	}
	defer apiDelete(t, "/snapshots/"+id(snap))

	// Get snapshot and verify resolution.
	getCode, getData := apiGet(t, "/snapshots/"+id(snap))
	if getCode != 200 {
		t.Fatalf("get: %d", getCode)
	}
	if !strings.Contains(string(getData), "resolved-val") {
		t.Error("secret not resolved in snapshot")
	}
	if strings.Contains(string(getData), "{{secret:") {
		t.Error("unresolved secret ref in snapshot")
	}
}

func TestMassive_Snapshot_FailOnMissingSecret(t *testing.T) {
	_, listener := apiPost(t, "/listeners", map[string]any{
		"name": "miss-sec", "port": 19998, "serverName": "{{secret:value:nonexistent-id}}",
	})
	defer apiDelete(t, "/listeners/"+id(listener))

	code, result := apiPost(t, "/snapshots", map[string]any{"name": "fail-snap"})
	if code != 400 {
		t.Errorf("expected 400 for unresolved secret, got %d: %v", code, result)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. Proxy routing — all match types
// ─────────────────────────────────────────────────────────────────────────────

func TestMassive_Routing_PathPrefix(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-prefix", "match": map[string]any{"pathPrefix": "/m-prefix"},
		"directResponse": map[string]any{"status": 200, "body": "prefix-hit"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, body := proxyGet(t, "/m-prefix/sub", nil)
	if code != 200 || body != "prefix-hit" {
		t.Errorf("prefix match: %d %q", code, body)
	}
	code, _, _ = proxyGet(t, "/other", nil)
	if code != 404 {
		t.Errorf("prefix miss should 404: %d", code)
	}
}

func TestMassive_Routing_PathExact(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-exact", "match": map[string]any{"path": "/m-exact-path"},
		"directResponse": map[string]any{"status": 200, "body": "exact-hit"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, body := proxyGet(t, "/m-exact-path", nil)
	if code != 200 || body != "exact-hit" {
		t.Errorf("exact match: %d %q", code, body)
	}
	code, _, _ = proxyGet(t, "/m-exact-path/extra", nil)
	if code != 404 {
		t.Errorf("exact with extra should 404: %d", code)
	}
}

func TestMassive_Routing_PathRegex(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-regex", "match": map[string]any{"pathRegex": "^/m-regex/[0-9]+$"},
		"directResponse": map[string]any{"status": 200, "body": "regex-hit"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, _ := proxyGet(t, "/m-regex/123", nil)
	if code != 200 {
		t.Errorf("/m-regex/123: %d", code)
	}
	code, _, _ = proxyGet(t, "/m-regex/abc", nil)
	if code != 404 {
		t.Errorf("/m-regex/abc should 404: %d", code)
	}
}

func TestMassive_Routing_MultipleMethodMatch(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-methods", "match": map[string]any{"pathPrefix": "/m-methods", "methods": []string{"POST", "PUT"}},
		"directResponse": map[string]any{"status": 200, "body": "methods-hit"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	for _, method := range []string{"POST", "PUT"} {
		code, _, _ := proxyRequest(t, method, "/m-methods", nil, nil)
		if code != 200 {
			t.Errorf("%s: %d", method, code)
		}
	}
	code, _, _ := proxyGet(t, "/m-methods", nil)
	if code != 404 {
		t.Errorf("GET should 404: %d", code)
	}
}

func TestMassive_Routing_HeaderRegex(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-hdr-regex", "match": map[string]any{
			"pathPrefix": "/m-hdr-regex",
			"headers":    []map[string]any{{"name": "X-Version", "value": "^v[0-9]+$", "regex": true}},
		},
		"directResponse": map[string]any{"status": 200, "body": "hdr-regex-hit"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, _ := proxyGet(t, "/m-hdr-regex", map[string]string{"X-Version": "v2"})
	if code != 200 {
		t.Errorf("regex header match: %d", code)
	}
	code, _, _ = proxyGet(t, "/m-hdr-regex", map[string]string{"X-Version": "latest"})
	if code != 404 {
		t.Errorf("regex header miss should 404: %d", code)
	}
}

func TestMassive_Routing_QueryParamRegex(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-qp-regex", "match": map[string]any{
			"pathPrefix":  "/m-qp-regex",
			"queryParams": []map[string]any{{"name": "id", "value": "^[0-9]+$", "regex": true}},
		},
		"directResponse": map[string]any{"status": 200, "body": "qp-regex-hit"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, _ := proxyGet(t, "/m-qp-regex?id=42", nil)
	if code != 200 {
		t.Errorf("qp regex match: %d", code)
	}
	code, _, _ = proxyGet(t, "/m-qp-regex?id=abc", nil)
	if code != 404 {
		t.Errorf("qp regex miss should 404: %d", code)
	}
}

func TestMassive_Routing_HostnameMatch(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-host", "match": map[string]any{
			"pathPrefix": "/m-host", "hostnames": []string{"app.test.local"},
		},
		"directResponse": map[string]any{"status": 200, "body": "host-hit"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, _ := proxyRequest(t, "GET", "/m-host", nil, map[string]string{"Host": "app.test.local"})
	if code != 200 {
		t.Errorf("host match: %d", code)
	}
	code, _, _ = proxyGet(t, "/m-host", nil)
	if code != 404 {
		t.Errorf("wrong host should 404: %d", code)
	}
}

func TestMassive_Routing_GRPCContentType(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-grpc", "match": map[string]any{"pathPrefix": "/m-grpc", "grpc": true},
		"directResponse": map[string]any{"status": 200, "body": "grpc-hit"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, _ := proxyRequest(t, "POST", "/m-grpc", nil, map[string]string{"Content-Type": "application/grpc"})
	if code != 200 {
		t.Errorf("grpc: %d", code)
	}
	code, _, _ = proxyRequest(t, "POST", "/m-grpc", nil, map[string]string{"Content-Type": "application/json"})
	if code != 404 {
		t.Errorf("non-grpc should 404: %d", code)
	}
}

func TestMassive_Routing_CELExpression(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-cel", "match": map[string]any{
			"pathPrefix": "/m-cel",
			"cel":        `request.headers["x-tier"] == "premium"`,
		},
		"directResponse": map[string]any{"status": 200, "body": "cel-hit"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, _ := proxyGet(t, "/m-cel", map[string]string{"X-Tier": "premium"})
	if code != 200 {
		t.Errorf("cel match: %d", code)
	}
	code, _, _ = proxyGet(t, "/m-cel", map[string]string{"X-Tier": "free"})
	if code != 404 {
		t.Errorf("cel miss should 404: %d", code)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. Route actions
// ─────────────────────────────────────────────────────────────────────────────

func TestMassive_Action_DirectResponse(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-dr", "match": map[string]any{"pathPrefix": "/m-dr"},
		"directResponse": map[string]any{"status": 418, "body": "teapot"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, body := proxyGet(t, "/m-dr", nil)
	if code != 418 || body != "teapot" {
		t.Errorf("direct response: %d %q", code, body)
	}
}

func TestMassive_Action_Redirect(t *testing.T) {
	codes := []int{301, 302, 307, 308}
	for _, rc := range codes {
		t.Run(fmt.Sprintf("code-%d", rc), func(t *testing.T) {
			_, route := apiPost(t, "/routes", map[string]any{
				"name": fmt.Sprintf("m-redir-%d", rc), "match": map[string]any{"pathPrefix": fmt.Sprintf("/m-redir-%d", rc)},
				"redirect": map[string]any{"url": "https://example.com/target", "code": rc},
			})
			defer apiDelete(t, "/routes/"+id(route))
			snap := activateSnapshot(t)
			defer apiDelete(t, "/snapshots/"+snap)

			code, hdr, _ := proxyGet(t, fmt.Sprintf("/m-redir-%d", rc), nil)
			if code != rc {
				t.Errorf("expected %d, got %d", rc, code)
			}
			if hdr.Get("Location") != "https://example.com/target" {
				t.Errorf("location: %q", hdr.Get("Location"))
			}
		})
	}
}

func TestMassive_Action_Forward(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upstream", "reached")
		w.Write([]byte("forwarded"))
	})
	destID := createDestination(t, "m-fwd", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-fwd", "match": map[string]any{"pathPrefix": "/m-fwd"},
		"forward": map[string]any{"destinations": []map[string]any{{"destinationId": destID, "weight": 100}}},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, hdr, body := proxyGet(t, "/m-fwd", nil)
	if code != 200 || body != "forwarded" || hdr.Get("X-Upstream") != "reached" {
		t.Errorf("forward: %d %q upstream=%q", code, body, hdr.Get("X-Upstream"))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 5. Forward features — rewrite, retry, timeout, mirror
// ─────────────────────────────────────────────────────────────────────────────

func TestMassive_Forward_PrefixRewrite(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("path=" + r.URL.Path))
	})
	destID := createDestination(t, "m-rewrite", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-rewrite", "match": map[string]any{"pathPrefix": "/m-rewrite/v1"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
			"rewrite":      map[string]any{"path": "/internal"},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, body := proxyGet(t, "/m-rewrite/v1/users", nil)
	if code != 200 || !strings.Contains(body, "path=/internal/users") {
		t.Errorf("prefix rewrite: %d %q", code, body)
	}
}

func TestMassive_Forward_RegexRewrite(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("path=" + r.URL.Path))
	})
	destID := createDestination(t, "m-rxrw", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-rxrw", "match": map[string]any{"pathPrefix": "/m-rxrw"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
			"rewrite":      map[string]any{"pathRegex": map[string]any{"pattern": "^/m-rxrw/(.*)", "substitution": "/new/$1"}},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, body := proxyGet(t, "/m-rxrw/orders", nil)
	if code != 200 || !strings.Contains(body, "path=/new/orders") {
		t.Errorf("regex rewrite: %d %q", code, body)
	}
}

func TestMassive_Forward_HostRewrite(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("host=" + r.Host))
	})
	destID := createDestination(t, "m-hostrw", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-hostrw", "match": map[string]any{"pathPrefix": "/m-hostrw"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
			"rewrite":      map[string]any{"host": "rewritten.example.com"},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, body := proxyGet(t, "/m-hostrw", nil)
	if code != 200 || !strings.Contains(body, "host=rewritten.example.com") {
		t.Errorf("host rewrite: %d %q", code, body)
	}
}

func TestMassive_Forward_Retry(t *testing.T) {
	var count atomic.Int64
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		n := count.Add(1)
		if n <= 2 {
			w.WriteHeader(502)
			return
		}
		w.Write([]byte("ok-retry"))
	})
	destID := createDestination(t, "m-retry", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-retry", "match": map[string]any{"pathPrefix": "/m-retry"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
			"retry":        map[string]any{"attempts": 3, "on": []string{"gateway-error"}},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, body := proxyGet(t, "/m-retry", nil)
	if code != 200 || body != "ok-retry" {
		t.Errorf("retry: %d %q", code, body)
	}
}

func TestMassive_Forward_RequestTimeout(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.Write([]byte("late"))
	})
	destID := createDestination(t, "m-tout", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-tout", "match": map[string]any{"pathPrefix": "/m-tout"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
			"timeouts":     map[string]any{"request": "500ms"},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, _ := proxyGet(t, "/m-tout", nil)
	if code != http.StatusGatewayTimeout && code != http.StatusServiceUnavailable {
		t.Errorf("timeout: expected 504 or 503, got %d", code)
	}
}

func TestMassive_Forward_Mirror(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("primary")) })
	mirrorUp := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })

	destID := createDestination(t, "m-mir-up", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)
	mirrorID := createDestination(t, "m-mir-tgt", mirrorUp.host(), mirrorUp.port())
	defer apiDelete(t, "/destinations/"+mirrorID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-mir", "match": map[string]any{"pathPrefix": "/m-mir"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
			"mirror":       map[string]any{"destinationId": mirrorID, "percentage": 100},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, body := proxyGet(t, "/m-mir", nil)
	if code != 200 || body != "primary" {
		t.Errorf("mirror primary: %d %q", code, body)
	}
	time.Sleep(500 * time.Millisecond)
	if mirrorUp.requestCount.Load() == 0 {
		t.Error("mirror target received no requests")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 6. Group composition
// ─────────────────────────────────────────────────────────────────────────────

func TestMassive_Group_PathPrefixComposition(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-grp-route", "match": map[string]any{"pathPrefix": "/items"},
		"directResponse": map[string]any{"status": 200, "body": "items-list"},
	})
	defer apiDelete(t, "/routes/"+id(route))

	_, group := apiPost(t, "/groups", map[string]any{
		"name": "m-grp", "pathPrefix": "/api/v1", "routeIds": []string{id(route)},
	})
	defer apiDelete(t, "/groups/"+id(group))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, _ := proxyGet(t, "/api/v1/items", nil)
	if code != 200 {
		t.Errorf("group prefix: %d", code)
	}
	code, _, _ = proxyGet(t, "/items", nil)
	if code != 404 {
		t.Errorf("ungrouped should 404: %d", code)
	}
}

func TestMassive_Group_HostnameMerge(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-grp-host-route", "match": map[string]any{"pathPrefix": "/dash"},
		"directResponse": map[string]any{"status": 200, "body": "dash"},
	})
	defer apiDelete(t, "/routes/"+id(route))

	_, group := apiPost(t, "/groups", map[string]any{
		"name": "m-grp-host", "hostnames": []string{"dash.test.local"}, "routeIds": []string{id(route)},
	})
	defer apiDelete(t, "/groups/"+id(group))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, _ := proxyRequest(t, "GET", "/dash", nil, map[string]string{"Host": "dash.test.local"})
	if code != 200 {
		t.Errorf("group hostname: %d", code)
	}
	code, _, _ = proxyGet(t, "/dash", nil)
	if code != 404 {
		t.Errorf("wrong host should 404: %d", code)
	}
}

func TestMassive_Group_RegexComposition(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-grp-rx-route", "match": map[string]any{"pathPrefix": "/data"},
		"directResponse": map[string]any{"status": 200, "body": "data"},
	})
	defer apiDelete(t, "/routes/"+id(route))

	_, group := apiPost(t, "/groups", map[string]any{
		"name": "m-grp-rx", "pathRegex": "/v[0-9]+", "routeIds": []string{id(route)},
	})
	defer apiDelete(t, "/groups/"+id(group))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, _ := proxyGet(t, "/v3/data", nil)
	if code != 200 {
		t.Errorf("regex group: %d", code)
	}
	code, _, _ = proxyGet(t, "/vX/data", nil)
	if code != 404 {
		t.Errorf("regex miss should 404: %d", code)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 7. Middlewares
// ─────────────────────────────────────────────────────────────────────────────

func TestMassive_Middleware_CORS(t *testing.T) {
	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "m-cors", "type": "cors",
		"cors": map[string]any{
			"allowOrigins": []map[string]any{{"value": "https://example.com"}},
			"allowMethods": []string{"GET", "POST"},
			"allowHeaders": []string{"Authorization"},
			"maxAge":       3600,
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-cors-route", "match": map[string]any{"pathPrefix": "/m-cors"},
		"directResponse": map[string]any{"status": 200, "body": "ok"},
		"middlewareIds":   []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	// Preflight.
	code, hdr, _ := proxyRequest(t, "OPTIONS", "/m-cors", nil, map[string]string{
		"Origin": "https://example.com", "Access-Control-Request-Method": "POST",
	})
	if code != 204 {
		t.Errorf("preflight: %d", code)
	}
	if hdr.Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Errorf("ACAO: %q", hdr.Get("Access-Control-Allow-Origin"))
	}

	// Normal request.
	code, hdr, _ = proxyGet(t, "/m-cors", map[string]string{"Origin": "https://example.com"})
	if code != 200 {
		t.Errorf("cors normal: %d", code)
	}
	if hdr.Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Errorf("ACAO on normal: %q", hdr.Get("Access-Control-Allow-Origin"))
	}
}

func TestMassive_Middleware_Headers(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("injected=" + r.Header.Get("X-Injected")))
	})
	destID := createDestination(t, "m-hdr-dest", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "m-headers", "type": "headers",
		"headers": map[string]any{
			"requestHeadersToAdd": []map[string]any{{"key": "X-Injected", "value": "by-vrata"}},
			"responseHeadersToAdd": []map[string]any{{"key": "X-Resp-Custom", "value": "added"}},
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-hdr-route", "match": map[string]any{"pathPrefix": "/m-hdr"},
		"forward":       map[string]any{"destinations": []map[string]any{{"destinationId": destID, "weight": 100}}},
		"middlewareIds": []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, hdr, body := proxyGet(t, "/m-hdr", nil)
	if code != 200 || !strings.Contains(body, "injected=by-vrata") {
		t.Errorf("request header: %d %q", code, body)
	}
	if hdr.Get("X-Resp-Custom") != "added" {
		t.Errorf("response header: %q", hdr.Get("X-Resp-Custom"))
	}
}

func TestMassive_Middleware_RateLimit(t *testing.T) {
	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "m-rl", "type": "rateLimit",
		"rateLimit": map[string]any{"requestsPerSecond": 2, "burst": 2},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-rl-route", "match": map[string]any{"pathPrefix": "/m-rl"},
		"directResponse": map[string]any{"status": 200, "body": "ok"},
		"middlewareIds":   []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	got429 := false
	for i := 0; i < 10; i++ {
		code, _, _ := proxyGet(t, "/m-rl", nil)
		if code == 429 {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Error("rate limiter did not trigger 429")
	}
}

func TestMassive_Middleware_DisablePerRoute(t *testing.T) {
	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "m-disable", "type": "rateLimit",
		"rateLimit": map[string]any{"requestsPerSecond": 0.001, "burst": 1},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-disable-route", "match": map[string]any{"pathPrefix": "/m-disable"},
		"directResponse": map[string]any{"status": 200, "body": "ok"},
		"middlewareIds":   []string{id(mw)},
		"middlewareOverrides": map[string]any{id(mw): map[string]any{"disabled": true}},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	for i := 0; i < 5; i++ {
		code, _, _ := proxyGet(t, "/m-disable", nil)
		if code != 200 {
			t.Errorf("request %d: expected 200 (middleware disabled), got %d", i, code)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 8. Proxy errors
// ─────────────────────────────────────────────────────────────────────────────

func TestMassive_ProxyErrors_NoRoute(t *testing.T) {
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, body := proxyGet(t, "/no-such-route-exists", nil)
	if code != 404 {
		t.Errorf("expected 404, got %d", code)
	}
	var errResp map[string]any
	json.Unmarshal([]byte(body), &errResp)
	if errResp["error"] != "no_route" {
		t.Errorf("expected no_route, got %v", errResp["error"])
	}
}

func TestMassive_ProxyErrors_ConnectionRefused(t *testing.T) {
	destID := createDestination(t, "m-dead", "127.0.0.1", 1)
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-dead-route", "match": map[string]any{"pathPrefix": "/m-dead"},
		"forward": map[string]any{"destinations": []map[string]any{{"destinationId": destID, "weight": 100}}},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, body := proxyGet(t, "/m-dead", nil)
	if code != 502 {
		t.Errorf("expected 502, got %d", code)
	}
	var errResp map[string]any
	json.Unmarshal([]byte(body), &errResp)
	errType, _ := errResp["error"].(string)
	if errType != "connection_refused" && errType != "unknown" {
		t.Errorf("expected connection_refused or unknown, got %q", errType)
	}
}

func TestMassive_ProxyErrors_JSONFormat(t *testing.T) {
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	_, hdr, body := proxyGet(t, "/guaranteed-no-route", nil)
	ct := hdr.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("proxy error Content-Type: %q", ct)
	}
	var errResp map[string]any
	if err := json.Unmarshal([]byte(body), &errResp); err != nil {
		t.Errorf("proxy error is not valid JSON: %v\nbody: %q", err, body)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 9. Concurrent proxy stress
// ─────────────────────────────────────────────────────────────────────────────

func TestMassive_Concurrent_ProxyRouting(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("concurrent-ok"))
	})
	destID := createDestination(t, "m-conc", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-conc", "match": map[string]any{"pathPrefix": "/m-conc"},
		"forward": map[string]any{"destinations": []map[string]any{{"destinationId": destID, "weight": 100}}},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	var failures atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Get(proxyURL + "/m-conc")
			if err != nil {
				failures.Add(1)
				return
			}
			defer resp.Body.Close()
			io.ReadAll(resp.Body)
			if resp.StatusCode != 200 {
				failures.Add(1)
			}
		}()
	}
	wg.Wait()
	if f := failures.Load(); f > 0 {
		t.Errorf("%d/100 concurrent requests failed", f)
	}
}

func TestMassive_Concurrent_APIOperations(t *testing.T) {
	var failures atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			code, result := apiPost(t, "/routes", map[string]any{
				"name": fmt.Sprintf("conc-route-%d", idx),
				"match": map[string]any{"pathPrefix": fmt.Sprintf("/conc-%d", idx)},
				"directResponse": map[string]any{"status": 200},
			})
			if code != 201 {
				failures.Add(1)
				return
			}
			apiDelete(t, "/routes/"+id(result))
		}(i)
	}
	wg.Wait()
	if f := failures.Load(); f > 0 {
		t.Errorf("%d/20 concurrent API operations failed", f)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 10. Weighted destination distribution
// ─────────────────────────────────────────────────────────────────────────────

func TestMassive_Weighted_Distribution(t *testing.T) {
	var countA, countB atomic.Int64
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { countA.Add(1); w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { countB.Add(1); w.Write([]byte("B")) })

	dA := createDestination(t, "m-wt-a", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+dA)
	dB := createDestination(t, "m-wt-b", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+dB)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-wt", "match": map[string]any{"pathPrefix": "/m-wt"},
		"forward": map[string]any{"destinations": []map[string]any{
			{"destinationId": dA, "weight": 70},
			{"destinationId": dB, "weight": 30},
		}},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	total := 1000
	client := &http.Client{Timeout: 5 * time.Second}
	for i := 0; i < total; i++ {
		resp, err := client.Get(proxyURL + "/m-wt")
		if err != nil {
			t.Fatal(err)
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()
	}

	a := countA.Load()
	b := countB.Load()
	ratioA := float64(a) / float64(total)
	if ratioA < 0.55 || ratioA > 0.85 {
		t.Errorf("A got %.1f%% (expected ~70%%): A=%d B=%d", ratioA*100, a, b)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 11. InlineAuthz
// ─────────────────────────────────────────────────────────────────────────────

func TestMassive_Middleware_InlineAuthz(t *testing.T) {
	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "m-iaz", "type": "inlineAuthz",
		"inlineAuthz": map[string]any{
			"rules": []map[string]any{
				{"cel": `request.headers["x-role"] == "admin"`, "action": "allow"},
			},
			"defaultAction": "deny",
			"denyStatus":    403,
			"denyBody":      `{"error":"forbidden"}`,
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "m-iaz-route", "match": map[string]any{"pathPrefix": "/m-iaz"},
		"directResponse": map[string]any{"status": 200, "body": "allowed"},
		"middlewareIds":   []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, body := proxyGet(t, "/m-iaz", map[string]string{"X-Role": "admin"})
	if code != 200 || body != "allowed" {
		t.Errorf("admin: %d %q", code, body)
	}

	code, _, body = proxyGet(t, "/m-iaz", map[string]string{"X-Role": "user"})
	if code != 403 {
		t.Errorf("user should be denied: %d", code)
	}

	code, _, _ = proxyGet(t, "/m-iaz", nil)
	if code != 403 {
		t.Errorf("no role should be denied: %d", code)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 12. Config dump
// ─────────────────────────────────────────────────────────────────────────────

func TestMassive_ConfigDump(t *testing.T) {
	code, data := apiGet(t, "/debug/config")
	if code != 200 {
		t.Fatalf("config dump: %d", code)
	}
	var dump map[string]any
	if err := json.Unmarshal(data, &dump); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"routes", "groups", "destinations", "listeners", "middlewares"} {
		if dump[key] == nil {
			t.Errorf("missing key %q in config dump", key)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 13. SSE sync endpoint
// ─────────────────────────────────────────────────────────────────────────────

func TestMassive_SSE_NoActiveSnapshot(t *testing.T) {
	// Ensure no snapshot is active by creating and deleting one.
	code, snap := apiPost(t, "/snapshots", map[string]any{"name": "sse-test"})
	if code == 201 {
		apiDelete(t, "/snapshots/"+id(snap))
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(apiBase + "/sync/snapshot")
	if err != nil {
		t.Fatalf("GET sync/snapshot: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200 for SSE endpoint, got %d", resp.StatusCode)
	}
}

func TestMassive_SSE_WithActiveSnapshot(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "sse-route", "match": map[string]any{"pathPrefix": "/sse"},
		"directResponse": map[string]any{"status": 200},
	})
	defer apiDelete(t, "/routes/"+id(route))

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(apiBase + "/sync/snapshot")
	if err != nil {
		t.Fatalf("GET sync/snapshot: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	data, _ := io.ReadAll(resp.Body)
	body := string(data)
	if !strings.Contains(body, "sse-route") {
		t.Errorf("SSE stream did not contain snapshot data")
	}
}
