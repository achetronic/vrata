// Package e2e runs end-to-end tests against a live Rutoso control plane
// and proxy. Tests spin up their own helper servers (JWKS, auth, processor,
// WebSocket, controllable upstreams) and create all required Rutoso
// entities via the API. Every test cleans up after itself.
//
// Requirements:
//   - Control plane on localhost:8080
//   - Proxy on localhost:3000
package e2e

import (
	"bufio"
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

const (
	apiBase  = "http://localhost:8080/api/v1"
	proxyURL = "http://localhost:3000"
)

// ─── API helpers ────────────────────────────────────────────────────────────

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
		if strings.EqualFold(k, "Host") {
			req.Host = v
		} else {
			req.Header.Set(k, v)
		}
	}
	client := &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
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
		if strings.EqualFold(k, "Host") {
			req.Host = v
		} else {
			req.Header.Set(k, v)
		}
	}
	client := &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("proxy %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(data)
}

func id(m map[string]any) string { return m["id"].(string) }

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

// createDestination creates a destination pointing to addr (host:port).
func createDestination(t *testing.T, name, host string, port int) string {
	t.Helper()
	_, d := apiPost(t, "/destinations", map[string]any{"name": name, "host": host, "port": port})
	if d["id"] == nil {
		t.Fatalf("create destination %s failed", name)
	}
	return id(d)
}

// ─── Controllable upstream server ───────────────────────────────────────────

type testUpstream struct {
	*httptest.Server
	requestCount atomic.Int64
	lastBody     atomic.Value
}

func startUpstream(t *testing.T, handler http.HandlerFunc) *testUpstream {
	t.Helper()
	u := &testUpstream{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.requestCount.Add(1)
		body, _ := io.ReadAll(r.Body)
		u.lastBody.Store(string(body))
		handler(w, r)
	}))
	t.Cleanup(u.Close)
	return u
}

func (u *testUpstream) host() string {
	addr := u.Listener.Addr().(*net.TCPAddr)
	return addr.IP.String()
}

func (u *testUpstream) port() int {
	return u.Listener.Addr().(*net.TCPAddr).Port
}

// ─── API CRUD Tests ─────────────────────────────────────────────────────────

func TestE2E_API_DestinationCRUD(t *testing.T) {
	code, created := apiPost(t, "/destinations", map[string]any{"name": "e2e-dest", "host": "127.0.0.1", "port": 9999})
	if code != 201 {
		t.Fatalf("create: %d", code)
	}
	defer apiDelete(t, "/destinations/"+id(created))

	code, _ = apiGet(t, "/destinations/"+id(created))
	if code != 200 {
		t.Errorf("get: %d", code)
	}

	code, updated := apiPut(t, "/destinations/"+id(created), map[string]any{"name": "e2e-dest-updated", "host": "127.0.0.1", "port": 8888})
	if code != 200 || updated["name"] != "e2e-dest-updated" {
		t.Errorf("update: %d", code)
	}

	if apiDelete(t, "/destinations/"+id(created)) != 204 {
		t.Error("delete failed")
	}
	if c, _ := apiGet(t, "/destinations/"+id(created)); c != 404 {
		t.Errorf("get after delete: %d", c)
	}
}

func TestE2E_API_ListenerCRUD(t *testing.T) {
	code, created := apiPost(t, "/listeners", map[string]any{"name": "e2e-listener", "port": 19999})
	if code != 201 {
		t.Fatalf("create: %d", code)
	}
	defer apiDelete(t, "/listeners/"+id(created))
	if created["address"] != "0.0.0.0" {
		t.Errorf("expected default address")
	}
	apiDelete(t, "/listeners/"+id(created))
}

func TestE2E_API_RouteCRUD(t *testing.T) {
	code, created := apiPost(t, "/routes", map[string]any{
		"name": "e2e-route", "match": map[string]any{"pathPrefix": "/e2e-test"},
		"directResponse": map[string]any{"status": 200, "body": "e2e-ok"},
	})
	if code != 201 {
		t.Fatalf("create: %d", code)
	}
	defer apiDelete(t, "/routes/"+id(created))
	apiDelete(t, "/routes/"+id(created))
}

func TestE2E_API_GroupCRUD(t *testing.T) {
	code, created := apiPost(t, "/groups", map[string]any{"name": "e2e-group", "pathPrefix": "/e2e"})
	if code != 201 {
		t.Fatalf("create: %d", code)
	}
	defer apiDelete(t, "/groups/"+id(created))
	apiDelete(t, "/groups/"+id(created))
}

func TestE2E_API_MiddlewareCRUD(t *testing.T) {
	code, created := apiPost(t, "/middlewares", map[string]any{
		"name": "e2e-headers", "type": "headers",
		"headers": map[string]any{"requestHeadersToAdd": []map[string]any{{"key": "X-E2E", "value": "true"}}},
	})
	if code != 201 {
		t.Fatalf("create: %d", code)
	}
	defer apiDelete(t, "/middlewares/"+id(created))
	apiDelete(t, "/middlewares/"+id(created))
}

// ─── Snapshot Tests ─────────────────────────────────────────────────────────

func TestE2E_SnapshotLifecycle(t *testing.T) {
	code, snap := apiPost(t, "/snapshots", map[string]string{"name": "e2e-snap"})
	if code != 201 {
		t.Fatalf("create: %d", code)
	}
	snapID := id(snap)
	defer apiDelete(t, "/snapshots/"+snapID)

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
				t.Error("should not be active yet")
			}
		}
	}
	if !found {
		t.Error("not in list")
	}

	code, activated := apiPost(t, "/snapshots/"+snapID+"/activate", nil)
	if code != 200 || activated["active"] != true {
		t.Fatalf("activate: %d", code)
	}

	apiDelete(t, "/snapshots/"+snapID)
}

// ─── Proxy Routing ──────────────────────────────────────────────────────────

func TestE2E_Proxy_DirectResponse(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-direct", "match": map[string]any{"pathPrefix": "/e2e-direct"},
		"directResponse": map[string]any{"status": 418, "body": "i am a teapot"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyGet(t, "/e2e-direct", nil)
	if code != 418 || body != "i am a teapot" {
		t.Errorf("got %d %q", code, body)
	}
}

func TestE2E_Proxy_Redirect(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-redirect", "match": map[string]any{"pathPrefix": "/e2e-redirect"},
		"redirect": map[string]any{"url": "https://example.com", "code": 302},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, headers, _ := proxyGet(t, "/e2e-redirect", nil)
	if code != 302 || headers.Get("Location") != "https://example.com" {
		t.Errorf("got %d location=%q", code, headers.Get("Location"))
	}
}

func TestE2E_Proxy_ForwardToUpstream(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("upstream-ok"))
	})
	destID := createDestination(t, "e2e-fwd", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-fwd", "match": map[string]any{"pathPrefix": "/e2e-fwd"},
		"forward": map[string]any{"backends": []map[string]any{{"destinationId": destID, "weight": 100}}},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyGet(t, "/e2e-fwd", nil)
	if code != 200 || body != "upstream-ok" {
		t.Errorf("got %d %q", code, body)
	}
}

func TestE2E_Proxy_GroupRegexComposition(t *testing.T) {
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)
	code, _, _ := proxyGet(t, "/es/pepe", nil)
	if code != 200 {
		t.Errorf("/es/pepe: %d", code)
	}
	code, _, _ = proxyGet(t, "/fr/pepe", nil)
	if code != 404 {
		t.Errorf("/fr/pepe: %d", code)
	}
}

func TestE2E_Proxy_MethodMatch(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-method", "match": map[string]any{"pathPrefix": "/e2e-method", "methods": []string{"POST"}},
		"directResponse": map[string]any{"status": 200, "body": "post-only"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyRequest(t, "POST", "/e2e-method", nil, nil)
	if code != 200 || body != "post-only" {
		t.Errorf("POST: %d %q", code, body)
	}
	code, _, _ = proxyGet(t, "/e2e-method", nil)
	if code != 404 {
		t.Errorf("GET: %d", code)
	}
}

func TestE2E_Proxy_HeaderMatch(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-hdr", "match": map[string]any{"pathPrefix": "/e2e-hdr", "headers": []map[string]any{{"name": "X-Test", "value": "yes"}}},
		"directResponse": map[string]any{"status": 200, "body": "hdr-ok"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, _ := proxyGet(t, "/e2e-hdr", map[string]string{"X-Test": "yes"})
	if code != 200 {
		t.Errorf("with header: %d", code)
	}
	code, _, _ = proxyGet(t, "/e2e-hdr", nil)
	if code != 404 {
		t.Errorf("without: %d", code)
	}
}

func TestE2E_Proxy_CELMatch(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-cel", "match": map[string]any{"pathPrefix": "/e2e-cel", "cel": `"x-magic" in request.headers && request.headers["x-magic"] == "42"`},
		"directResponse": map[string]any{"status": 200, "body": "cel-ok"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, _ := proxyGet(t, "/e2e-cel", map[string]string{"X-Magic": "42"})
	if code != 200 {
		t.Errorf("match: %d", code)
	}
	code, _, _ = proxyGet(t, "/e2e-cel", nil)
	if code != 404 {
		t.Errorf("no header: %d", code)
	}
}

func TestE2E_Proxy_QueryParamMatch(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-qp", "match": map[string]any{"pathPrefix": "/e2e-qp", "queryParams": []map[string]any{{"name": "token", "value": "abc"}}},
		"directResponse": map[string]any{"status": 200, "body": "qp-ok"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, _ := proxyGet(t, "/e2e-qp?token=abc", nil)
	if code != 200 {
		t.Errorf("match: %d", code)
	}
	code, _, _ = proxyGet(t, "/e2e-qp", nil)
	if code != 404 {
		t.Errorf("no param: %d", code)
	}
}

func TestE2E_Proxy_GRPCMatch(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-grpc", "match": map[string]any{"pathPrefix": "/e2e-grpc", "grpc": true},
		"directResponse": map[string]any{"status": 200, "body": "grpc-ok"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, _ := proxyRequest(t, "POST", "/e2e-grpc", nil, map[string]string{"Content-Type": "application/grpc"})
	if code != 200 {
		t.Errorf("grpc: %d", code)
	}
	code, _, _ = proxyGet(t, "/e2e-grpc", nil)
	if code != 404 {
		t.Errorf("no grpc: %d", code)
	}
}

func TestE2E_Proxy_HostnameMatch(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-host", "match": map[string]any{"pathPrefix": "/e2e-host", "hostnames": []string{"test.example.com"}},
		"directResponse": map[string]any{"status": 200, "body": "host-ok"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, _ := proxyRequest(t, "GET", "/e2e-host", nil, map[string]string{"Host": "test.example.com"})
	if code != 200 {
		t.Errorf("match: %d", code)
	}
	code, _, _ = proxyGet(t, "/e2e-host", nil)
	if code != 404 {
		t.Errorf("no match: %d", code)
	}
}

// ─── Middleware Tests ────────────────────────────────────────────────────────

func TestE2E_Proxy_HeadersMiddleware(t *testing.T) {
	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "e2e-hdr-mw", "type": "headers",
		"headers": map[string]any{"responseHeadersToAdd": []map[string]any{{"key": "X-Rutoso-E2E", "value": "true"}}},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-hdr-mw", "match": map[string]any{"pathPrefix": "/e2e-hdr-mw"},
		"directResponse": map[string]any{"status": 200, "body": "ok"}, "middlewareIds": []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	_, headers, _ := proxyGet(t, "/e2e-hdr-mw", nil)
	if headers.Get("X-Rutoso-E2E") != "true" {
		t.Errorf("missing header")
	}
}

func TestE2E_Proxy_CORSMiddleware(t *testing.T) {
	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "e2e-cors", "type": "cors",
		"cors": map[string]any{"allowOrigins": []map[string]any{{"value": "https://example.com"}}, "allowMethods": []string{"GET", "POST"}, "allowCredentials": true},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-cors", "match": map[string]any{"pathPrefix": "/e2e-cors"},
		"directResponse": map[string]any{"status": 200, "body": "ok"}, "middlewareIds": []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, headers, _ := proxyRequest(t, "OPTIONS", "/e2e-cors", nil, map[string]string{"Origin": "https://example.com"})
	if code != 204 {
		t.Errorf("preflight: %d", code)
	}
	if headers.Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Error("missing ACAO on preflight")
	}

	_, headers, _ = proxyGet(t, "/e2e-cors", map[string]string{"Origin": "https://example.com"})
	if headers.Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Error("missing ACAO on normal")
	}

	_, headers, _ = proxyGet(t, "/e2e-cors", nil)
	if headers.Get("Access-Control-Allow-Origin") != "" {
		t.Error("ACAO should be absent without Origin")
	}
}

func TestE2E_Proxy_CORSWildcard(t *testing.T) {
	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "e2e-cors-wild", "type": "cors",
		"cors": map[string]any{"allowOrigins": []map[string]any{{"value": "*"}}, "allowMethods": []string{"GET"}},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-cors-wild", "match": map[string]any{"pathPrefix": "/e2e-cors-wild"},
		"directResponse": map[string]any{"status": 200, "body": "ok"}, "middlewareIds": []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	_, headers, _ := proxyGet(t, "/e2e-cors-wild", map[string]string{"Origin": "https://anything.com"})
	if headers.Get("Access-Control-Allow-Origin") != "https://anything.com" {
		t.Error("wildcard origin not working")
	}
}

func TestE2E_Proxy_RateLimitMiddleware(t *testing.T) {
	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "e2e-rl", "type": "rateLimit", "rateLimit": map[string]any{"requestsPerSecond": 2, "burst": 2},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-rl", "match": map[string]any{"pathPrefix": "/e2e-rl"},
		"directResponse": map[string]any{"status": 200, "body": "ok"}, "middlewareIds": []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	rateLimited := false
	for i := 0; i < 20; i++ {
		code, _, _ := proxyGet(t, "/e2e-rl", nil)
		if code == 429 {
			rateLimited = true
			break
		}
	}
	if !rateLimited {
		t.Error("expected 429")
	}
}

// ─── JWT with real JWKS server ──────────────────────────────────────────────

func TestE2E_Proxy_JWTMiddleware(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	pub := &rsaKey.PublicKey
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
	jwksJSON := fmt.Sprintf(`{"keys":[{"kty":"RSA","kid":"e2e-kid","n":"%s","e":"%s"}]}`, n, e)

	jwksSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(jwksJSON))
	}))
	defer jwksSrv.Close()

	jwksAddr := jwksSrv.Listener.Addr().(*net.TCPAddr)
	jwksDestID := createDestination(t, "e2e-jwks", jwksAddr.IP.String(), jwksAddr.Port)
	defer apiDelete(t, "/destinations/"+jwksDestID)

	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "e2e-jwt", "type": "jwt",
		"jwt": map[string]any{
			"providers": map[string]any{
				"default": map[string]any{
					"issuer": "e2e-issuer", "audiences": []string{"e2e-aud"},
					"jwksUri": "/.well-known/jwks.json", "jwksDestinationId": jwksDestID,
				},
			},
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-jwt", "match": map[string]any{"pathPrefix": "/e2e-jwt"},
		"directResponse": map[string]any{"status": 200, "body": "jwt-ok"}, "middlewareIds": []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	signToken := func(claims map[string]any) string {
		hJSON, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "e2e-kid"})
		cJSON, _ := json.Marshal(claims)
		hEnc := base64.RawURLEncoding.EncodeToString(hJSON)
		cEnc := base64.RawURLEncoding.EncodeToString(cJSON)
		signed := hEnc + "." + cEnc
		hash := sha256.Sum256([]byte(signed))
		sig, _ := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, hash[:])
		return signed + "." + base64.RawURLEncoding.EncodeToString(sig)
	}

	validToken := signToken(map[string]any{"iss": "e2e-issuer", "aud": "e2e-aud", "exp": float64(time.Now().Add(time.Hour).Unix())})
	code, _, body := proxyGet(t, "/e2e-jwt", map[string]string{"Authorization": "Bearer " + validToken})
	if code != 200 || body != "jwt-ok" {
		t.Errorf("valid token: %d %q", code, body)
	}

	code, _, _ = proxyGet(t, "/e2e-jwt", nil)
	if code != 401 {
		t.Errorf("no token: expected 401, got %d", code)
	}

	code, _, _ = proxyGet(t, "/e2e-jwt", map[string]string{"Authorization": "Bearer invalid.token.here"})
	if code != 401 {
		t.Errorf("invalid token: expected 401, got %d", code)
	}

	expiredToken := signToken(map[string]any{"iss": "e2e-issuer", "aud": "e2e-aud", "exp": float64(time.Now().Add(-time.Hour).Unix())})
	code, _, _ = proxyGet(t, "/e2e-jwt", map[string]string{"Authorization": "Bearer " + expiredToken})
	if code != 401 {
		t.Errorf("expired: expected 401, got %d", code)
	}

	wrongKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	forgedClaims, _ := json.Marshal(map[string]any{"iss": "e2e-issuer", "aud": "e2e-aud", "exp": float64(time.Now().Add(time.Hour).Unix())})
	hEnc := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"e2e-kid"}`))
	cEnc := base64.RawURLEncoding.EncodeToString(forgedClaims)
	forgedSigned := hEnc + "." + cEnc
	forgedHash := sha256.Sum256([]byte(forgedSigned))
	forgedSig, _ := rsa.SignPKCS1v15(rand.Reader, wrongKey, crypto.SHA256, forgedHash[:])
	forgedToken := forgedSigned + "." + base64.RawURLEncoding.EncodeToString(forgedSig)
	code, _, _ = proxyGet(t, "/e2e-jwt", map[string]string{"Authorization": "Bearer " + forgedToken})
	if code != 401 {
		t.Errorf("forged sig: expected 401, got %d", code)
	}
}

// ─── JWT with EC key ────────────────────────────────────────────────────────

func TestE2E_Proxy_JWTMiddlewareEC(t *testing.T) {
	ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pub := &ecKey.PublicKey
	x := base64.RawURLEncoding.EncodeToString(pub.X.Bytes())
	y := base64.RawURLEncoding.EncodeToString(pub.Y.Bytes())
	jwksJSON := fmt.Sprintf(`{"keys":[{"kty":"EC","kid":"ec-kid","crv":"P-256","x":"%s","y":"%s"}]}`, x, y)

	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "e2e-jwt-ec", "type": "jwt",
		"jwt": map[string]any{"providers": map[string]any{"default": map[string]any{"issuer": "iss", "jwksInline": jwksJSON}}},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-jwt-ec", "match": map[string]any{"pathPrefix": "/e2e-jwt-ec"},
		"directResponse": map[string]any{"status": 200, "body": "ec-ok"}, "middlewareIds": []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	hJSON, _ := json.Marshal(map[string]string{"alg": "ES256", "kid": "ec-kid"})
	cJSON, _ := json.Marshal(map[string]any{"iss": "iss", "exp": float64(time.Now().Add(time.Hour).Unix())})
	hEnc := base64.RawURLEncoding.EncodeToString(hJSON)
	cEnc := base64.RawURLEncoding.EncodeToString(cJSON)
	signed := hEnc + "." + cEnc
	hash := sha256.Sum256([]byte(signed))
	sig, _ := ecdsa.SignASN1(rand.Reader, ecKey, hash[:])
	token := signed + "." + base64.RawURLEncoding.EncodeToString(sig)

	code, _, body := proxyGet(t, "/e2e-jwt-ec", map[string]string{"Authorization": "Bearer " + token})
	if code != 200 || body != "ec-ok" {
		t.Errorf("EC token: %d %q", code, body)
	}
}

// ─── ExtAuthz with real HTTP auth server ────────────────────────────────────

func TestE2E_Proxy_ExtAuthzMiddleware(t *testing.T) {
	authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer valid-token" {
			w.Header().Set("X-Auth-User", "user-1")
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(403)
		w.Write([]byte("denied"))
	}))
	defer authSrv.Close()

	authAddr := authSrv.Listener.Addr().(*net.TCPAddr)
	authDestID := createDestination(t, "e2e-authz", authAddr.IP.String(), authAddr.Port)
	defer apiDelete(t, "/destinations/"+authDestID)

	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "e2e-authz", "type": "extAuthz",
		"extAuthz": map[string]any{
			"destinationId": authDestID, "path": "/check",
			"onCheck": map[string]any{"forwardHeaders": []string{"authorization"}},
			"onAllow": map[string]any{"copyToUpstream": []string{"x-auth-user"}},
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("user=" + r.Header.Get("X-Auth-User")))
	})
	upDestID := createDestination(t, "e2e-authz-up", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+upDestID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-authz", "match": map[string]any{"pathPrefix": "/e2e-authz"},
		"forward":       map[string]any{"backends": []map[string]any{{"destinationId": upDestID, "weight": 100}}},
		"middlewareIds": []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyGet(t, "/e2e-authz", map[string]string{"Authorization": "Bearer valid-token"})
	if code != 200 || !strings.Contains(body, "user=user-1") {
		t.Errorf("allow: %d %q", code, body)
	}

	code, _, body = proxyGet(t, "/e2e-authz", map[string]string{"Authorization": "Bearer bad"})
	if code != 403 {
		t.Errorf("deny: expected 403, got %d", code)
	}
}

// ─── ExtProc with real HTTP processor ───────────────────────────────────────

func TestE2E_Proxy_ExtProcMiddleware(t *testing.T) {
	procSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		phase, _ := req["phase"].(string)

		switch phase {
		case "requestHeaders":
			json.NewEncoder(w).Encode(map[string]any{
				"action":     "requestHeaders",
				"status":     "continue",
				"setHeaders": []map[string]string{{"key": "x-processed", "value": "true"}},
			})
		default:
			json.NewEncoder(w).Encode(map[string]any{"action": phase, "status": "continue"})
		}
	}))
	defer procSrv.Close()

	procAddr := procSrv.Listener.Addr().(*net.TCPAddr)
	procDestID := createDestination(t, "e2e-proc", procAddr.IP.String(), procAddr.Port)
	defer apiDelete(t, "/destinations/"+procDestID)

	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "e2e-extproc", "type": "extProc",
		"extProc": map[string]any{"destinationId": procDestID, "mode": "http", "timeout": "2s"},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("processed=" + r.Header.Get("X-Processed")))
	})
	upDestID := createDestination(t, "e2e-proc-up", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+upDestID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-extproc", "match": map[string]any{"pathPrefix": "/e2e-extproc"},
		"forward":       map[string]any{"backends": []map[string]any{{"destinationId": upDestID, "weight": 100}}},
		"middlewareIds": []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyGet(t, "/e2e-extproc", nil)
	if code != 200 || !strings.Contains(body, "processed=true") {
		t.Errorf("extproc: %d %q", code, body)
	}
}

func TestE2E_Proxy_ExtProcReject(t *testing.T) {
	procSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"action": "reject", "rejectStatus": 403, "rejectBody": "YmxvY2tlZA==",
		})
	}))
	defer procSrv.Close()

	procAddr := procSrv.Listener.Addr().(*net.TCPAddr)
	procDestID := createDestination(t, "e2e-proc-rej", procAddr.IP.String(), procAddr.Port)
	defer apiDelete(t, "/destinations/"+procDestID)

	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "e2e-extproc-rej", "type": "extProc",
		"extProc": map[string]any{"destinationId": procDestID, "mode": "http"},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-extproc-rej", "match": map[string]any{"pathPrefix": "/e2e-extproc-rej"},
		"directResponse": map[string]any{"status": 200, "body": "should-not-reach"}, "middlewareIds": []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, _ := proxyGet(t, "/e2e-extproc-rej", nil)
	if code != 403 {
		t.Errorf("reject: expected 403, got %d", code)
	}
}

// ─── Retry with failing upstream ────────────────────────────────────────────

func TestE2E_Proxy_Retry(t *testing.T) {
	var count atomic.Int64
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		n := count.Add(1)
		if n <= 2 {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte("ok-after-retry"))
	})
	destID := createDestination(t, "e2e-retry", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-retry", "match": map[string]any{"pathPrefix": "/e2e-retry"},
		"forward": map[string]any{
			"backends": []map[string]any{{"destinationId": destID, "weight": 100}},
			"retry":    map[string]any{"attempts": 3, "on": []string{"server-error"}},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyGet(t, "/e2e-retry", nil)
	if code != 200 || body != "ok-after-retry" {
		t.Errorf("retry: %d %q (count=%d)", code, body, count.Load())
	}
	if count.Load() < 3 {
		t.Errorf("expected at least 3 attempts, got %d", count.Load())
	}
}

// ─── Request timeout with slow upstream ─────────────────────────────────────

func TestE2E_Proxy_RequestTimeout(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.Write([]byte("too-late"))
	})
	destID := createDestination(t, "e2e-timeout", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-timeout", "match": map[string]any{"pathPrefix": "/e2e-timeout"},
		"forward": map[string]any{
			"backends": []map[string]any{{"destinationId": destID, "weight": 100}},
			"timeouts": map[string]any{"request": "500ms"},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, _ := proxyGet(t, "/e2e-timeout", nil)
	if code != 503 {
		t.Errorf("timeout: expected 503, got %d", code)
	}
}

// ─── Mirror to second upstream ──────────────────────────────────────────────

func TestE2E_Proxy_Mirror(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("primary"))
	})
	mirror := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})

	upDestID := createDestination(t, "e2e-mirror-up", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+upDestID)
	mirrorDestID := createDestination(t, "e2e-mirror-dest", mirror.host(), mirror.port())
	defer apiDelete(t, "/destinations/"+mirrorDestID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-mirror", "match": map[string]any{"pathPrefix": "/e2e-mirror"},
		"forward": map[string]any{
			"backends": []map[string]any{{"destinationId": upDestID, "weight": 100}},
			"mirror":   map[string]any{"destinationId": mirrorDestID, "percentage": 100},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyGet(t, "/e2e-mirror", nil)
	if code != 200 || body != "primary" {
		t.Errorf("primary: %d %q", code, body)
	}

	time.Sleep(500 * time.Millisecond)
	if mirror.requestCount.Load() == 0 {
		t.Error("mirror upstream received no requests")
	}
}

// ─── WebSocket upgrade ──────────────────────────────────────────────────────

func TestE2E_Proxy_WebSocket(t *testing.T) {
	wsSrv := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		var msg string
		websocket.Message.Receive(ws, &msg)
		websocket.Message.Send(ws, "echo:"+msg)
	}))
	defer wsSrv.Close()

	wsAddr := wsSrv.Listener.Addr().(*net.TCPAddr)
	destID := createDestination(t, "e2e-ws", wsAddr.IP.String(), wsAddr.Port)
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-ws", "match": map[string]any{"pathPrefix": "/e2e-ws"},
		"forward": map[string]any{"backends": []map[string]any{{"destinationId": destID, "weight": 100}}},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	wsURL := fmt.Sprintf("ws://localhost:3000/e2e-ws")
	ws, err := websocket.Dial(wsURL, "", "http://localhost")
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer ws.Close()

	websocket.Message.Send(ws, "hello")
	var reply string
	websocket.Message.Receive(ws, &reply)
	if reply != "echo:hello" {
		t.Errorf("expected echo:hello, got %q", reply)
	}
}

// ─── Access log to file ─────────────────────────────────────────────────────

func TestE2E_Proxy_AccessLogToFile(t *testing.T) {
	logDir := t.TempDir()
	logFile := filepath.Join(logDir, "access.log")

	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "e2e-accesslog", "type": "accessLog",
		"accessLog": map[string]any{
			"path": logFile, "json": true,
			"onRequest":  map[string]any{"fields": map[string]string{"event": "req", "method": "${request.method}", "path": "${request.path}"}},
			"onResponse": map[string]any{"fields": map[string]string{"event": "resp", "status": "${response.status}"}},
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-alog", "match": map[string]any{"pathPrefix": "/e2e-alog"},
		"directResponse": map[string]any{"status": 200, "body": "ok"}, "middlewareIds": []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	proxyGet(t, "/e2e-alog", nil)

	time.Sleep(500 * time.Millisecond)

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected 2 log lines, got %d: %s", len(lines), string(data))
	}

	var reqEntry map[string]string
	json.Unmarshal([]byte(lines[0]), &reqEntry)
	if reqEntry["event"] != "req" || reqEntry["method"] != "GET" {
		t.Errorf("request log: %v", reqEntry)
	}

	var respEntry map[string]string
	json.Unmarshal([]byte(lines[1]), &respEntry)
	if respEntry["event"] != "resp" || respEntry["status"] != "200" {
		t.Errorf("response log: %v", respEntry)
	}
}

// ─── Config Dump & Sync ─────────────────────────────────────────────────────

func TestE2E_API_ConfigDump(t *testing.T) {
	code, body := apiGet(t, "/debug/config")
	if code != 200 {
		t.Fatalf("config dump: %d", code)
	}
	var dump map[string]json.RawMessage
	json.Unmarshal(body, &dump)
	for _, key := range []string{"listeners", "routes", "groups", "destinations", "middlewares"} {
		if _, ok := dump[key]; !ok {
			t.Errorf("missing %q", key)
		}
	}
}

func TestE2E_SyncStream(t *testing.T) {
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	client := &http.Client{Timeout: 3 * time.Second}
	req, _ := http.NewRequest("GET", apiBase+"/sync/stream", nil)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("sse: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("content-type: %q", resp.Header.Get("Content-Type"))
	}

	scanner := bufio.NewScanner(resp.Body)
	foundSnapshot := false
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "event: snapshot") {
			foundSnapshot = true
			break
		}
	}
	if !foundSnapshot {
		t.Error("no snapshot event received")
	}
}

// ─── Path rewrite regex ─────────────────────────────────────────────────────

func TestE2E_Proxy_PathRewriteRegex(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("path=" + r.URL.Path))
	})
	destID := createDestination(t, "e2e-rwx", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-rwx", "match": map[string]any{"pathPrefix": "/e2e-rwx"},
		"forward": map[string]any{
			"backends": []map[string]any{{"destinationId": destID, "weight": 100}},
			"rewrite":  map[string]any{"pathRegex": map[string]any{"pattern": "^/e2e-rwx(.*)", "substitution": "/rewritten$1"}},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyGet(t, "/e2e-rwx/foo", nil)
	if code != 200 || !strings.Contains(body, "path=/rewritten/foo") {
		t.Errorf("regex rewrite: %d %q", code, body)
	}
}
