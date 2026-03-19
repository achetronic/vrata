package e2e

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// ─── skipWhen / onlyWhen ───────────────────────────────────────────────────

// TestE2E_Middleware_SkipWhen verifies that a middleware is skipped when
// the skipWhen CEL expression matches the request.
func TestE2E_Middleware_SkipWhen(t *testing.T) {
	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "skip-headers",
		"type": "headers",
		"headers": map[string]any{
			"responseHeadersToAdd": []map[string]any{
				{"key": "X-Filtered", "value": "yes"},
			},
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "skip-when-route",
		"match":          map[string]any{"pathPrefix": "/app/skip-test"},
		"directResponse": map[string]any{"status": 200, "body": "ok"},
		"middlewareIds":   []string{id(mw)},
		"middlewareOverrides": map[string]any{
			id(mw): map[string]any{
				"skipWhen": []string{`"x-bypass" in request.headers`},
			},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	// Without bypass header: middleware runs, X-Filtered present.
	_, hdrs, _ := proxyGet(t, "/app/skip-test", nil)
	if hdrs.Get("X-Filtered") != "yes" {
		t.Errorf("without bypass: expected X-Filtered=yes, got %q", hdrs.Get("X-Filtered"))
	}

	// With bypass header: middleware skipped, X-Filtered absent.
	_, hdrs, _ = proxyGet(t, "/app/skip-test", map[string]string{"X-Bypass": "true"})
	if hdrs.Get("X-Filtered") != "" {
		t.Errorf("with bypass: expected no X-Filtered, got %q", hdrs.Get("X-Filtered"))
	}
}

// TestE2E_Middleware_OnlyWhen verifies that a middleware only runs when
// the onlyWhen CEL expression matches.
func TestE2E_Middleware_OnlyWhen(t *testing.T) {
	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "only-headers",
		"type": "headers",
		"headers": map[string]any{
			"responseHeadersToAdd": []map[string]any{
				{"key": "X-Special", "value": "active"},
			},
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "only-when-route",
		"match":          map[string]any{"pathPrefix": "/app/only-test"},
		"directResponse": map[string]any{"status": 200, "body": "ok"},
		"middlewareIds":   []string{id(mw)},
		"middlewareOverrides": map[string]any{
			id(mw): map[string]any{
				"onlyWhen": []string{`request.method == "POST"`},
			},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	// GET: onlyWhen doesn't match → middleware skipped.
	_, hdrs, _ := proxyGet(t, "/app/only-test", nil)
	if hdrs.Get("X-Special") != "" {
		t.Errorf("GET: expected no X-Special, got %q", hdrs.Get("X-Special"))
	}

	// POST: onlyWhen matches → middleware runs.
	code, hdrs, _ := proxyRequest(t, "POST", "/app/only-test", nil, nil)
	if code != 200 {
		t.Fatalf("POST: %d", code)
	}
	if hdrs.Get("X-Special") != "active" {
		t.Errorf("POST: expected X-Special=active, got %q", hdrs.Get("X-Special"))
	}
}

// TestE2E_Middleware_SkipWhen_OnlyWhen_Combined tests the sandbox scenario:
// extAuthz active by default, skipped when X-Authz-Skip header present.
// Headers middleware only active when X-Authz-Skip present (simulates JWT path).
func TestE2E_Middleware_SkipWhen_OnlyWhen_Combined(t *testing.T) {
	_, mwAuthz := apiPost(t, "/middlewares", map[string]any{
		"name": "authz-sim",
		"type": "headers",
		"headers": map[string]any{
			"responseHeadersToAdd": []map[string]any{
				{"key": "X-Authz", "value": "checked"},
			},
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mwAuthz))

	_, mwJwt := apiPost(t, "/middlewares", map[string]any{
		"name": "jwt-sim",
		"type": "headers",
		"headers": map[string]any{
			"responseHeadersToAdd": []map[string]any{
				{"key": "X-JWT", "value": "checked"},
			},
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mwJwt))

	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "combined-conditions",
		"match":          map[string]any{"pathPrefix": "/app/sandbox"},
		"directResponse": map[string]any{"status": 200, "body": "ok"},
		"middlewareIds":   []string{id(mwAuthz), id(mwJwt)},
		"middlewareOverrides": map[string]any{
			id(mwAuthz): map[string]any{
				"skipWhen": []string{`"x-authz-skip" in request.headers`},
			},
			id(mwJwt): map[string]any{
				"onlyWhen": []string{`"x-authz-skip" in request.headers`},
			},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	// Normal flow: authz runs, jwt skipped.
	_, hdrs, _ := proxyGet(t, "/app/sandbox", nil)
	if hdrs.Get("X-Authz") != "checked" {
		t.Errorf("normal: expected X-Authz=checked, got %q", hdrs.Get("X-Authz"))
	}
	if hdrs.Get("X-JWT") != "" {
		t.Errorf("normal: expected no X-JWT, got %q", hdrs.Get("X-JWT"))
	}

	// Bypass flow: authz skipped, jwt runs.
	_, hdrs, _ = proxyGet(t, "/app/sandbox", map[string]string{"X-Authz-Skip": "true"})
	if hdrs.Get("X-Authz") != "" {
		t.Errorf("bypass: expected no X-Authz, got %q", hdrs.Get("X-Authz"))
	}
	if hdrs.Get("X-JWT") != "checked" {
		t.Errorf("bypass: expected X-JWT=checked, got %q", hdrs.Get("X-JWT"))
	}
}

// ─── assertClaims ──────────────────────────────────────────────────────────

// TestE2E_JWT_AssertClaims verifies that assertClaims CEL expressions reject
// tokens with invalid claims (403) and accept tokens with valid claims (200).
func TestE2E_JWT_AssertClaims(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	jwksServer := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		n := base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1})
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"keys":[{"kty":"RSA","n":"%s","e":"%s","kid":"k1","alg":"RS256","use":"sig"}]}`, n, e)
	})

	jwksDest := createDestination(t, "jwks-server", jwksServer.host(), jwksServer.port())
	defer apiDelete(t, "/destinations/"+jwksDest)

	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "jwt-assert",
		"type": "jwt",
		"jwt": map[string]any{
			"issuer":            "test-issuer",
			"jwksUri":           "/.well-known/jwks.json",
			"jwksDestinationId": jwksDest,
			"assertClaims": []string{
				`claims.role == "admin"`,
			},
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "assert-claims-route",
		"match":          map[string]any{"pathPrefix": "/app/admin-only"},
		"directResponse": map[string]any{"status": 200, "body": "admin-ok"},
		"middlewareIds":   []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	// Helper to sign a JWT.
	sign := func(claims map[string]any) string {
		hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"k1"}`))
		claimsJSON, _ := json.Marshal(claims)
		payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
		signed := hdr + "." + payload
		h := sha256.Sum256([]byte(signed))
		sig, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
		return signed + "." + base64.RawURLEncoding.EncodeToString(sig)
	}

	// Admin token → 200.
	adminToken := sign(map[string]any{
		"iss":  "test-issuer",
		"exp":  float64(time.Now().Add(time.Hour).Unix()),
		"role": "admin",
	})
	code, _, _ := proxyGet(t, "/app/admin-only", map[string]string{
		"Authorization": "Bearer " + adminToken,
	})
	if code != 200 {
		t.Errorf("admin token: expected 200, got %d", code)
	}

	// Viewer token → 403.
	viewerToken := sign(map[string]any{
		"iss":  "test-issuer",
		"exp":  float64(time.Now().Add(time.Hour).Unix()),
		"role": "viewer",
	})
	code, _, _ = proxyGet(t, "/app/admin-only", map[string]string{
		"Authorization": "Bearer " + viewerToken,
	})
	if code != 403 {
		t.Errorf("viewer token: expected 403, got %d", code)
	}

	// No token → 401.
	code, _, _ = proxyGet(t, "/app/admin-only", nil)
	if code != 401 {
		t.Errorf("no token: expected 401, got %d", code)
	}
}
