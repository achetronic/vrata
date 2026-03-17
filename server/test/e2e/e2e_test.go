// Package e2e runs end-to-end tests against a live Rutoso control plane
// and proxy. Tests expect:
//   - Control plane on localhost:8080
//   - Proxy on localhost:3000
//   - Kind cluster "rutoso-dev" with podinfo in app-01 and app-02 namespaces
//
// Run with: go test -tags e2e -v -timeout 120s ./test/e2e/
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	apiBase  = "http://localhost:8080/api/v1"
	proxyURL = "http://localhost:3000"
)

// ─── Helpers ────────────────────────────────────────────────────────────────

func apiPost(t *testing.T, path string, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(apiBase+path, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(data, &result)
	return resp.StatusCode, result
}

func apiGet(t *testing.T, path string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(apiBase + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

func apiDelete(t *testing.T, path string) int {
	t.Helper()
	req, _ := http.NewRequest("DELETE", apiBase+path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func apiPut(t *testing.T, path string, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("PUT", apiBase+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(data, &result)
	return resp.StatusCode, result
}

func proxyGet(t *testing.T, path string, headers map[string]string) (int, http.Header, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", proxyURL+path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("proxy GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(data)
}

func proxyRequest(t *testing.T, method, path string, body []byte, headers map[string]string) (int, http.Header, string) {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, proxyURL+path, bodyReader)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("proxy %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(data)
}

func id(m map[string]any) string {
	return m["id"].(string)
}

func waitForProxy(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(proxyURL + "/__healthcheck_nonexistent")
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("proxy not reachable")
}

func activateSnapshot(t *testing.T) string {
	t.Helper()
	code, result := apiPost(t, "/snapshots", map[string]string{"name": fmt.Sprintf("e2e-%d", time.Now().UnixNano())})
	if code != 201 {
		t.Fatalf("create snapshot: %d %v", code, result)
	}
	snapID := id(result)
	code, _ = apiPost(t, "/snapshots/"+snapID+"/activate", nil)
	if code != 200 {
		t.Fatalf("activate snapshot: %d", code)
	}
	time.Sleep(500 * time.Millisecond)
	return snapID
}

// ─── API CRUD Tests ─────────────────────────────────────────────────────────

func TestE2E_API_DestinationCRUD(t *testing.T) {
	code, created := apiPost(t, "/destinations", map[string]any{
		"name": "e2e-dest", "host": "127.0.0.1", "port": 9999,
	})
	if code != 201 {
		t.Fatalf("create: %d", code)
	}
	destID := id(created)
	defer apiDelete(t, "/destinations/"+destID)

	code, _ = apiGet(t, "/destinations/"+destID)
	if code != 200 {
		t.Errorf("get: %d", code)
	}

	code, updated := apiPut(t, "/destinations/"+destID, map[string]any{
		"name": "e2e-dest-updated", "host": "127.0.0.1", "port": 8888,
	})
	if code != 200 {
		t.Errorf("update: %d", code)
	}
	if updated["name"] != "e2e-dest-updated" {
		t.Errorf("name not updated")
	}

	code = apiDelete(t, "/destinations/"+destID)
	if code != 204 {
		t.Errorf("delete: %d", code)
	}

	code, _ = apiGet(t, "/destinations/"+destID)
	if code != 404 {
		t.Errorf("get after delete: %d", code)
	}
}

func TestE2E_API_ListenerCRUD(t *testing.T) {
	code, created := apiPost(t, "/listeners", map[string]any{
		"name": "e2e-listener", "port": 19999,
	})
	if code != 201 {
		t.Fatalf("create: %d", code)
	}
	lid := id(created)
	defer apiDelete(t, "/listeners/"+lid)

	if created["address"] != "0.0.0.0" {
		t.Errorf("expected default address, got %v", created["address"])
	}

	code = apiDelete(t, "/listeners/"+lid)
	if code != 204 {
		t.Errorf("delete: %d", code)
	}
}

func TestE2E_API_RouteCRUD(t *testing.T) {
	code, created := apiPost(t, "/routes", map[string]any{
		"name":           "e2e-route",
		"match":          map[string]any{"pathPrefix": "/e2e-test"},
		"directResponse": map[string]any{"status": 200, "body": "e2e-ok"},
	})
	if code != 201 {
		t.Fatalf("create: %d", code)
	}
	rid := id(created)
	defer apiDelete(t, "/routes/"+rid)

	code = apiDelete(t, "/routes/"+rid)
	if code != 204 {
		t.Errorf("delete: %d", code)
	}
}

func TestE2E_API_GroupCRUD(t *testing.T) {
	code, created := apiPost(t, "/groups", map[string]any{
		"name":       "e2e-group",
		"pathPrefix": "/e2e",
	})
	if code != 201 {
		t.Fatalf("create: %d", code)
	}
	gid := id(created)
	defer apiDelete(t, "/groups/"+gid)

	code = apiDelete(t, "/groups/"+gid)
	if code != 204 {
		t.Errorf("delete: %d", code)
	}
}

func TestE2E_API_MiddlewareCRUD(t *testing.T) {
	code, created := apiPost(t, "/middlewares", map[string]any{
		"name": "e2e-headers",
		"type": "headers",
		"headers": map[string]any{
			"requestHeadersToAdd": []map[string]any{
				{"key": "X-E2E", "value": "true"},
			},
		},
	})
	if code != 201 {
		t.Fatalf("create: %d", code)
	}
	mid := id(created)
	defer apiDelete(t, "/middlewares/"+mid)

	code = apiDelete(t, "/middlewares/"+mid)
	if code != 204 {
		t.Errorf("delete: %d", code)
	}
}

// ─── Snapshot Tests ─────────────────────────────────────────────────────────

func TestE2E_SnapshotLifecycle(t *testing.T) {
	// Create snapshot
	code, snap := apiPost(t, "/snapshots", map[string]string{"name": "e2e-snap"})
	if code != 201 {
		t.Fatalf("create: %d", code)
	}
	snapID := id(snap)
	defer apiDelete(t, "/snapshots/"+snapID)

	// List
	code, body := apiGet(t, "/snapshots")
	if code != 200 {
		t.Fatalf("list: %d", code)
	}
	var summaries []map[string]any
	json.Unmarshal(body, &summaries)
	found := false
	for _, s := range summaries {
		if s["id"] == snapID {
			found = true
			if s["active"] != false {
				t.Error("snapshot should not be active yet")
			}
		}
	}
	if !found {
		t.Error("snapshot not found in list")
	}

	// Activate
	code, activated := apiPost(t, "/snapshots/"+snapID+"/activate", nil)
	if code != 200 {
		t.Fatalf("activate: %d", code)
	}
	if activated["active"] != true {
		t.Error("expected active=true after activation")
	}

	// Delete
	code = apiDelete(t, "/snapshots/"+snapID)
	if code != 204 {
		t.Errorf("delete: %d", code)
	}
}

// ─── Proxy Routing Tests ────────────────────────────────────────────────────

func TestE2E_Proxy_DirectResponse(t *testing.T) {
	_, dest := apiPost(t, "/destinations", map[string]any{"name": "e2e-dr-dest", "host": "127.0.0.1", "port": 1})
	defer apiDelete(t, "/destinations/"+id(dest))

	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "e2e-direct",
		"match":          map[string]any{"pathPrefix": "/e2e-direct"},
		"directResponse": map[string]any{"status": 418, "body": "i am a teapot"},
	})
	defer apiDelete(t, "/routes/"+id(route))

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyGet(t, "/e2e-direct", nil)
	if code != 418 {
		t.Errorf("expected 418, got %d", code)
	}
	if body != "i am a teapot" {
		t.Errorf("expected 'i am a teapot', got %q", body)
	}
}

func TestE2E_Proxy_Redirect(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "e2e-redirect",
		"match": map[string]any{"pathPrefix": "/e2e-redirect"},
		"redirect": map[string]any{
			"url":  "https://example.com",
			"code": 302,
		},
	})
	defer apiDelete(t, "/routes/"+id(route))

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, headers, _ := proxyGet(t, "/e2e-redirect", nil)
	if code != 302 {
		t.Errorf("expected 302, got %d", code)
	}
	if loc := headers.Get("Location"); loc != "https://example.com" {
		t.Errorf("expected Location: https://example.com, got %q", loc)
	}
}

func TestE2E_Proxy_ForwardToUpstream(t *testing.T) {
	// Use existing podinfo destination on localhost:9898
	code, _ := apiGet(t, "/destinations")
	if code != 200 {
		t.Fatal("cannot list destinations")
	}

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "e2e-forward",
		"match": map[string]any{"pathPrefix": "/e2e-forward"},
		"forward": map[string]any{
			"backends": []map[string]any{
				{"destinationId": "c42e5e5f-d729-424e-8787-b829670c5bf5", "weight": 100},
			},
			"rewrite": map[string]any{"path": "/"},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code2, _, body := proxyGet(t, "/e2e-forward", nil)
	if code2 != 200 {
		t.Errorf("expected 200, got %d: %s", code2, body)
	}
	if !strings.Contains(body, "podinfo") {
		t.Errorf("expected podinfo response, got: %s", body[:min(len(body), 200)])
	}
}

func TestE2E_Proxy_GroupRegexComposition(t *testing.T) {
	// The existing config has group with pathRegex /(es|en|pk) and route with pathPrefix /pepe
	// Test that /es/pepe works and /fr/pepe doesn't
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyGet(t, "/es/pepe", nil)
	if code != 200 {
		t.Errorf("/es/pepe: expected 200, got %d: %s", code, body)
	}

	code, _, _ = proxyGet(t, "/fr/pepe", nil)
	if code != 404 {
		t.Errorf("/fr/pepe: expected 404, got %d", code)
	}
}

func TestE2E_Proxy_MethodMatch(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "e2e-method",
		"match":          map[string]any{"pathPrefix": "/e2e-method", "methods": []string{"POST"}},
		"directResponse": map[string]any{"status": 200, "body": "post-only"},
	})
	defer apiDelete(t, "/routes/"+id(route))

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyRequest(t, "POST", "/e2e-method", nil, nil)
	if code != 200 || body != "post-only" {
		t.Errorf("POST: expected 200 'post-only', got %d %q", code, body)
	}

	code, _, _ = proxyGet(t, "/e2e-method", nil)
	if code != 404 {
		t.Errorf("GET: expected 404, got %d", code)
	}
}

func TestE2E_Proxy_HeaderMatch(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-header-match",
		"match": map[string]any{
			"pathPrefix": "/e2e-header-match",
			"headers":    []map[string]any{{"name": "X-Test", "value": "yes"}},
		},
		"directResponse": map[string]any{"status": 200, "body": "header-matched"},
	})
	defer apiDelete(t, "/routes/"+id(route))

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyGet(t, "/e2e-header-match", map[string]string{"X-Test": "yes"})
	if code != 200 || body != "header-matched" {
		t.Errorf("with header: expected 200, got %d %q", code, body)
	}

	code, _, _ = proxyGet(t, "/e2e-header-match", nil)
	if code != 404 {
		t.Errorf("without header: expected 404, got %d", code)
	}
}

func TestE2E_Proxy_CELMatch(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-cel",
		"match": map[string]any{
			"pathPrefix": "/e2e-cel",
			"cel":        `"x-magic" in request.headers && request.headers["x-magic"] == "42"`,
		},
		"directResponse": map[string]any{"status": 200, "body": "cel-ok"},
	})
	defer apiDelete(t, "/routes/"+id(route))

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyGet(t, "/e2e-cel", map[string]string{"X-Magic": "42"})
	if code != 200 || body != "cel-ok" {
		t.Errorf("with magic header: expected 200, got %d %q", code, body)
	}

	code, _, _ = proxyGet(t, "/e2e-cel", nil)
	if code != 404 {
		t.Errorf("without magic header: expected 404, got %d", code)
	}

	code, _, _ = proxyGet(t, "/e2e-cel", map[string]string{"X-Magic": "99"})
	if code != 404 {
		t.Errorf("wrong magic value: expected 404, got %d", code)
	}
}

// ─── Middleware Tests ────────────────────────────────────────────────────────

func TestE2E_Proxy_HeadersMiddleware(t *testing.T) {
	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "e2e-add-header",
		"type": "headers",
		"headers": map[string]any{
			"responseHeadersToAdd": []map[string]any{
				{"key": "X-Rutoso-E2E", "value": "true"},
			},
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "e2e-mw-headers",
		"match":          map[string]any{"pathPrefix": "/e2e-mw-headers"},
		"directResponse": map[string]any{"status": 200, "body": "ok"},
		"middlewareIds":  []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, headers, _ := proxyGet(t, "/e2e-mw-headers", nil)
	if code != 200 {
		t.Fatalf("expected 200, got %d", code)
	}
	if headers.Get("X-Rutoso-E2E") != "true" {
		t.Errorf("expected X-Rutoso-E2E: true, got %q", headers.Get("X-Rutoso-E2E"))
	}
}

func TestE2E_Proxy_CORSMiddleware(t *testing.T) {
	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "e2e-cors",
		"type": "cors",
		"cors": map[string]any{
			"allowOrigins":     []map[string]any{{"value": "https://example.com"}},
			"allowMethods":     []string{"GET", "POST"},
			"allowHeaders":     []string{"Content-Type"},
			"allowCredentials": true,
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "e2e-cors-route",
		"match":          map[string]any{"pathPrefix": "/e2e-cors"},
		"directResponse": map[string]any{"status": 200, "body": "ok"},
		"middlewareIds":  []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	// Preflight
	code, headers, _ := proxyRequest(t, "OPTIONS", "/e2e-cors", nil, map[string]string{
		"Origin": "https://example.com",
	})
	if code != 200 {
		t.Errorf("preflight: expected 200, got %d", code)
	}
	if headers.Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Errorf("expected ACAO header, got %q", headers.Get("Access-Control-Allow-Origin"))
	}

	// Normal request with origin
	code, headers, _ = proxyGet(t, "/e2e-cors", map[string]string{"Origin": "https://example.com"})
	if code != 200 {
		t.Errorf("normal: expected 200, got %d", code)
	}
	if headers.Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Errorf("ACAO missing on normal request")
	}

	// Request without origin — no CORS headers
	code, headers, _ = proxyGet(t, "/e2e-cors", nil)
	if code != 200 {
		t.Errorf("no origin: expected 200, got %d", code)
	}
	if headers.Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("ACAO should be absent without Origin, got %q", headers.Get("Access-Control-Allow-Origin"))
	}
}

func TestE2E_Proxy_RateLimitMiddleware(t *testing.T) {
	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "e2e-ratelimit",
		"type": "rateLimit",
		"rateLimit": map[string]any{
			"requestsPerSecond": 2,
			"burst":             2,
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "e2e-rl",
		"match":          map[string]any{"pathPrefix": "/e2e-rl"},
		"directResponse": map[string]any{"status": 200, "body": "ok"},
		"middlewareIds":  []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	// Burst should allow first 2 requests
	for i := 0; i < 2; i++ {
		code, _, _ := proxyGet(t, "/e2e-rl", nil)
		if code != 200 {
			t.Errorf("request %d: expected 200, got %d", i, code)
		}
	}

	// Subsequent requests should be rate limited (429)
	rateLimited := false
	for i := 0; i < 10; i++ {
		code, _, _ := proxyGet(t, "/e2e-rl", nil)
		if code == 429 {
			rateLimited = true
			break
		}
	}
	if !rateLimited {
		t.Error("expected at least one 429 response")
	}
}

// ─── Config Dump ────────────────────────────────────────────────────────────

func TestE2E_API_ConfigDump(t *testing.T) {
	code, body := apiGet(t, "/debug/config")
	if code != 200 {
		t.Fatalf("config dump: %d", code)
	}

	var dump map[string]json.RawMessage
	json.Unmarshal(body, &dump)
	for _, key := range []string{"listeners", "routes", "groups", "destinations", "middlewares"} {
		if _, ok := dump[key]; !ok {
			t.Errorf("missing %q in config dump", key)
		}
	}
}

// ─── Sync Stream ────────────────────────────────────────────────────────────

func TestE2E_SyncStreamRequiresActiveSnapshot(t *testing.T) {
	// Connect to SSE stream — should stay open but may or may not send data
	// depending on whether there's an active snapshot
	client := &http.Client{Timeout: 2 * time.Second}
	req, _ := http.NewRequest("GET", apiBase+"/sync/stream", nil)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		// Timeout is expected if no snapshot is active
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %q", ct)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
