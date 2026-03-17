package e2e

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

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
		"jwt": map[string]any{"providers": map[string]any{"default": map[string]any{
			"issuer": "e2e-issuer", "audiences": []string{"e2e-aud"},
			"jwksUri": "/.well-known/jwks.json", "jwksDestinationId": jwksDestID,
		}}},
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
		t.Errorf("valid: %d %q", code, body)
	}

	code, _, _ = proxyGet(t, "/e2e-jwt", nil)
	if code != 401 {
		t.Errorf("no token: %d", code)
	}

	code, _, _ = proxyGet(t, "/e2e-jwt", map[string]string{"Authorization": "Bearer invalid.token.here"})
	if code != 401 {
		t.Errorf("invalid: %d", code)
	}

	expiredToken := signToken(map[string]any{"iss": "e2e-issuer", "aud": "e2e-aud", "exp": float64(time.Now().Add(-time.Hour).Unix())})
	code, _, _ = proxyGet(t, "/e2e-jwt", map[string]string{"Authorization": "Bearer " + expiredToken})
	if code != 401 {
		t.Errorf("expired: %d", code)
	}

	wrongKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	forgedClaims, _ := json.Marshal(map[string]any{"iss": "e2e-issuer", "aud": "e2e-aud", "exp": float64(time.Now().Add(time.Hour).Unix())})
	hEnc := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"e2e-kid"}`))
	cEnc := base64.RawURLEncoding.EncodeToString(forgedClaims)
	forgedHash := sha256.Sum256([]byte(hEnc + "." + cEnc))
	forgedSig, _ := rsa.SignPKCS1v15(rand.Reader, wrongKey, crypto.SHA256, forgedHash[:])
	forgedToken := hEnc + "." + cEnc + "." + base64.RawURLEncoding.EncodeToString(forgedSig)
	code, _, _ = proxyGet(t, "/e2e-jwt", map[string]string{"Authorization": "Bearer " + forgedToken})
	if code != 401 {
		t.Errorf("forged: %d", code)
	}
}

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
	hash := sha256.Sum256([]byte(hEnc + "." + cEnc))
	sig, _ := ecdsa.SignASN1(rand.Reader, ecKey, hash[:])
	token := hEnc + "." + cEnc + "." + base64.RawURLEncoding.EncodeToString(sig)

	code, _, body := proxyGet(t, "/e2e-jwt-ec", map[string]string{"Authorization": "Bearer " + token})
	if code != 200 || body != "ec-ok" {
		t.Errorf("EC: %d %q", code, body)
	}
}

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
		"forward": map[string]any{"backends": []map[string]any{{"destinationId": upDestID, "weight": 100}}},
		"middlewareIds": []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyGet(t, "/e2e-authz", map[string]string{"Authorization": "Bearer valid-token"})
	if code != 200 || !strings.Contains(body, "user=user-1") {
		t.Errorf("allow: %d %q", code, body)
	}

	code, _, _ = proxyGet(t, "/e2e-authz", map[string]string{"Authorization": "Bearer bad"})
	if code != 403 {
		t.Errorf("deny: %d", code)
	}
}

func TestE2E_Proxy_ExtProcMiddleware(t *testing.T) {
	procSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		phase, _ := req["phase"].(string)
		if phase == "requestHeaders" {
			json.NewEncoder(w).Encode(map[string]any{"action": "requestHeaders", "status": "continue", "setHeaders": []map[string]string{{"key": "x-processed", "value": "true"}}})
		} else {
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
		"forward": map[string]any{"backends": []map[string]any{{"destinationId": upDestID, "weight": 100}}},
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
		json.NewEncoder(w).Encode(map[string]any{"action": "reject", "rejectStatus": 403, "rejectBody": "YmxvY2tlZA=="})
	}))
	defer procSrv.Close()

	procAddr := procSrv.Listener.Addr().(*net.TCPAddr)
	procDestID := createDestination(t, "e2e-proc-rej", procAddr.IP.String(), procAddr.Port)
	defer apiDelete(t, "/destinations/"+procDestID)

	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "e2e-extproc-rej", "type": "extProc", "extProc": map[string]any{"destinationId": procDestID, "mode": "http"},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-extproc-rej", "match": map[string]any{"pathPrefix": "/e2e-extproc-rej"},
		"directResponse": map[string]any{"status": 200, "body": "nope"}, "middlewareIds": []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, _ := proxyGet(t, "/e2e-extproc-rej", nil)
	if code != 403 {
		t.Errorf("reject: %d", code)
	}
}
