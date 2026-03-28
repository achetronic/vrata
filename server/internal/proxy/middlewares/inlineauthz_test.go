// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package middlewares

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/achetronic/vrata/internal/model"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})
}

func TestInlineAuthz_SingleAllowRule(t *testing.T) {
	cfg := &model.InlineAuthzConfig{
		Rules: []model.InlineAuthzRule{
			{CEL: `request.method == "GET"`, Action: "allow"},
		},
		DefaultAction: "deny",
	}
	mw := InlineAuthzMiddleware(cfg, 65536)
	h := mw(okHandler())

	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Errorf("GET should be allowed, got %d", w.Code)
	}

	r2 := httptest.NewRequest("POST", "/", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Code != 403 {
		t.Errorf("POST should be denied, got %d", w2.Code)
	}
}

func TestInlineAuthz_SingleDenyRule(t *testing.T) {
	cfg := &model.InlineAuthzConfig{
		Rules: []model.InlineAuthzRule{
			{CEL: `request.method == "DELETE"`, Action: "deny"},
		},
		DefaultAction: "allow",
	}
	mw := InlineAuthzMiddleware(cfg, 65536)
	h := mw(okHandler())

	r := httptest.NewRequest("DELETE", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Errorf("DELETE should be denied, got %d", w.Code)
	}

	r2 := httptest.NewRequest("GET", "/", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Code != 200 {
		t.Errorf("GET should be allowed (default), got %d", w2.Code)
	}
}

func TestInlineAuthz_FirstMatchWins(t *testing.T) {
	cfg := &model.InlineAuthzConfig{
		Rules: []model.InlineAuthzRule{
			{CEL: `request.path == "/admin"`, Action: "deny"},
			{CEL: `request.method == "GET"`, Action: "allow"},
		},
		DefaultAction: "deny",
	}
	mw := InlineAuthzMiddleware(cfg, 65536)
	h := mw(okHandler())

	// /admin GET — first rule matches (deny).
	r := httptest.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Errorf("/admin GET should be denied by first rule, got %d", w.Code)
	}

	// /other GET — first rule misses, second matches (allow).
	r2 := httptest.NewRequest("GET", "/other", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Code != 200 {
		t.Errorf("/other GET should be allowed by second rule, got %d", w2.Code)
	}

	// /other POST — both rules miss, default deny.
	r3 := httptest.NewRequest("POST", "/other", nil)
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, r3)
	if w3.Code != 403 {
		t.Errorf("/other POST should be denied by default, got %d", w3.Code)
	}
}

func TestInlineAuthz_DefaultAllow(t *testing.T) {
	cfg := &model.InlineAuthzConfig{
		Rules:         []model.InlineAuthzRule{},
		DefaultAction: "allow",
	}
	mw := InlineAuthzMiddleware(cfg, 65536)
	h := mw(okHandler())

	r := httptest.NewRequest("GET", "/anything", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Errorf("empty rules + default allow should pass, got %d", w.Code)
	}
}

func TestInlineAuthz_DefaultDeny(t *testing.T) {
	cfg := &model.InlineAuthzConfig{
		Rules:         []model.InlineAuthzRule{},
		DefaultAction: "deny",
	}
	mw := InlineAuthzMiddleware(cfg, 65536)
	h := mw(okHandler())

	r := httptest.NewRequest("GET", "/anything", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Errorf("empty rules + default deny should block, got %d", w.Code)
	}
}

func TestInlineAuthz_CustomDenyStatus(t *testing.T) {
	cfg := &model.InlineAuthzConfig{
		Rules:         []model.InlineAuthzRule{},
		DefaultAction: "deny",
		DenyStatus:    401,
		DenyBody:      `{"error":"unauthorized"}`,
	}
	mw := InlineAuthzMiddleware(cfg, 65536)
	h := mw(okHandler())

	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Errorf("custom deny status: got %d, want 401", w.Code)
	}
	if !strings.Contains(w.Body.String(), "unauthorized") {
		t.Errorf("custom deny body: got %q", w.Body.String())
	}
}

func TestInlineAuthz_BodyJSONRule(t *testing.T) {
	cfg := &model.InlineAuthzConfig{
		Rules: []model.InlineAuthzRule{
			{CEL: `has(request.body) && has(request.body.json) && request.body.json.method == "tools/call" && request.body.json.params.name in ["add", "subtract"]`, Action: "allow"},
		},
		DefaultAction: "deny",
	}
	mw := InlineAuthzMiddleware(cfg, 65536)
	h := mw(okHandler())

	// Allowed tool.
	body := `{"method":"tools/call","params":{"name":"add"}}`
	r := httptest.NewRequest("POST", "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Errorf("add tool should be allowed, got %d", w.Code)
	}

	// Denied tool.
	body2 := `{"method":"tools/call","params":{"name":"delete"}}`
	r2 := httptest.NewRequest("POST", "/", strings.NewReader(body2))
	r2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Code != 403 {
		t.Errorf("delete tool should be denied, got %d", w2.Code)
	}

	// Non-tools/call method — denied (rule doesn't match).
	body3 := `{"method":"initialize"}`
	r3 := httptest.NewRequest("POST", "/", strings.NewReader(body3))
	r3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, r3)
	if w3.Code != 403 {
		t.Errorf("non-matching method should be denied by default, got %d", w3.Code)
	}
}

func TestInlineAuthz_MCPFullScenario(t *testing.T) {
	cfg := &model.InlineAuthzConfig{
		Rules: []model.InlineAuthzRule{
			{CEL: `request.method == "GET" || request.method == "DELETE"`, Action: "allow"},
			{CEL: `has(request.body) && has(request.body.json) && request.body.json.method in ["initialize", "notifications/initialized", "tools/list"]`, Action: "allow"},
			{CEL: `has(request.body) && has(request.body.json) && request.body.json.method == "tools/call" && request.body.json.params.name in ["add", "subtract"]`, Action: "allow"},
		},
		DefaultAction: "deny",
	}
	mw := InlineAuthzMiddleware(cfg, 65536)
	h := mw(okHandler())

	tests := []struct {
		name   string
		method string
		body   string
		ct     string
		want   int
	}{
		{"GET SSE", "GET", "", "", 200},
		{"DELETE session", "DELETE", "", "", 200},
		{"initialize", "POST", `{"method":"initialize"}`, "application/json", 200},
		{"tools/list", "POST", `{"method":"tools/list"}`, "application/json", 200},
		{"allowed tool", "POST", `{"method":"tools/call","params":{"name":"add"}}`, "application/json", 200},
		{"denied tool", "POST", `{"method":"tools/call","params":{"name":"evil"}}`, "application/json", 403},
		{"unknown method", "POST", `{"method":"unknown"}`, "application/json", 403},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var r *http.Request
			if tt.body != "" {
				r = httptest.NewRequest(tt.method, "/mcp", strings.NewReader(tt.body))
				r.Header.Set("Content-Type", tt.ct)
			} else {
				r = httptest.NewRequest(tt.method, "/mcp", nil)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Errorf("got %d, want %d", w.Code, tt.want)
			}
		})
	}
}

func TestInlineAuthz_CELCompileErrorSkipsRule(t *testing.T) {
	cfg := &model.InlineAuthzConfig{
		Rules: []model.InlineAuthzRule{
			{CEL: `not valid cel !!!`, Action: "allow"},
			{CEL: `request.method == "GET"`, Action: "allow"},
		},
		DefaultAction: "deny",
	}
	mw := InlineAuthzMiddleware(cfg, 65536)
	h := mw(okHandler())

	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Errorf("second rule should still work after bad first rule, got %d", w.Code)
	}
}

func TestInlineAuthz_NilConfig(t *testing.T) {
	mw := InlineAuthzMiddleware(nil, 65536)
	h := mw(okHandler())

	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Errorf("nil config should pass through, got %d", w.Code)
	}
}

func TestInlineAuthzNeedsBody(t *testing.T) {
	tests := []struct {
		name string
		cfg  *model.InlineAuthzConfig
		want bool
	}{
		{"nil config", nil, false},
		{"no body ref", &model.InlineAuthzConfig{
			Rules: []model.InlineAuthzRule{{CEL: `request.method == "GET"`, Action: "allow"}},
		}, false},
		{"body ref", &model.InlineAuthzConfig{
			Rules: []model.InlineAuthzRule{{CEL: `request.body.json.method == "test"`, Action: "allow"}},
		}, true},
		{"mixed rules", &model.InlineAuthzConfig{
			Rules: []model.InlineAuthzRule{
				{CEL: `request.method == "GET"`, Action: "allow"},
				{CEL: `request.body.raw.contains("test")`, Action: "deny"},
			},
		}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InlineAuthzNeedsBody(tt.cfg)
			if got != tt.want {
				t.Errorf("NeedsBody: got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInlineAuthz_TLSCertRule(t *testing.T) {
	cfg := &model.InlineAuthzConfig{
		Rules: []model.InlineAuthzRule{
			{CEL: `has(request.tls) && request.tls.peerCertificate.uris.exists(u, u == "spiffe://cluster.local/ns/default/sa/agent-a")`, Action: "allow"},
		},
		DefaultAction: "deny",
	}
	mw := InlineAuthzMiddleware(cfg, 65536)
	h := mw(okHandler())

	// Build a request with mTLS cert.
	r := httptest.NewRequest("GET", "/", nil)
	r.TLS = buildTestTLSState([]string{"spiffe://cluster.local/ns/default/sa/agent-a"})
	// Need to buffer body for CEL to populate tls fields.
	// Actually TLS fields don't need body buffering, they come from r.TLS.
	// But we need the request to pass through the middleware.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Errorf("matching SPIFFE URI should be allowed, got %d", w.Code)
	}

	r2 := httptest.NewRequest("GET", "/", nil)
	r2.TLS = buildTestTLSState([]string{"spiffe://cluster.local/ns/default/sa/agent-b"})
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Code != 403 {
		t.Errorf("non-matching SPIFFE URI should be denied, got %d", w2.Code)
	}

	r3 := httptest.NewRequest("GET", "/", nil)
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, r3)
	if w3.Code != 403 {
		t.Errorf("no TLS should be denied, got %d", w3.Code)
	}
}

// buildTestTLSState creates a minimal tls.ConnectionState with a self-signed cert
// containing the given URI SANs.
func buildTestTLSState(uris []string) *tls.ConnectionState {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	for _, u := range uris {
		parsed, _ := url.Parse(u)
		tmpl.URIs = append(tmpl.URIs, parsed)
	}
	certDER, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	cert, _ := x509.ParseCertificate(certDER)
	return &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
	}
}
