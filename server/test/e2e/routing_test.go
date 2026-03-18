package e2e

import (
	"net/http"
	"strings"
	"testing"
)

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
		"forward": map[string]any{"destinations": []map[string]any{{"destinationId": destID, "weight": 100}}},
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

func TestE2E_Proxy_PathRewriteRegex(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("path=" + r.URL.Path))
	})
	destID := createDestination(t, "e2e-rwx", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-rwx", "match": map[string]any{"pathPrefix": "/e2e-rwx"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
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
