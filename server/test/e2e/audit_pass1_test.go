// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestE2E_JWT_ClaimToHeaders verifies that JWT claimToHeaders extracts claim
// values into upstream request headers via CEL expressions.
func TestE2E_JWT_ClaimToHeaders(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	jwksSrv := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		n := base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1})
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"keys":[{"kty":"RSA","n":"%s","e":"%s","kid":"cth","alg":"RS256","use":"sig"}]}`, n, e)
	})
	jwksDest := createDestination(t, "cth-jwks", jwksSrv.host(), jwksSrv.port())
	defer apiDelete(t, "/destinations/"+jwksDest)

	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "cth-jwt",
		"type": "jwt",
		"jwt": map[string]any{
			"issuer":            "cth-iss",
			"jwksPath":          "/.well-known/jwks.json",
			"jwksDestinationId": jwksDest,
			"claimToHeaders": []map[string]any{
				{"expr": "claims.sub", "header": "X-User-Id"},
				{"expr": "claims.role", "header": "X-User-Role"},
			},
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fmt.Sprintf("sub=%s,role=%s",
			r.Header.Get("X-User-Id"), r.Header.Get("X-User-Role"))))
	})
	upDest := createDestination(t, "cth-up", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+upDest)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "cth-route",
		"match": map[string]any{"pathPrefix": "/cth-test"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": upDest, "weight": 100}},
		},
		"middlewareIds": []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	sign := func(claims map[string]any) string {
		hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"cth"}`))
		claimsJSON, _ := json.Marshal(claims)
		payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
		signed := hdr + "." + payload
		h := sha256.Sum256([]byte(signed))
		sig, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
		return signed + "." + base64.RawURLEncoding.EncodeToString(sig)
	}

	token := sign(map[string]any{
		"iss":  "cth-iss",
		"exp":  float64(time.Now().Add(time.Hour).Unix()),
		"sub":  "user-42",
		"role": "admin",
	})

	code, _, body := proxyGet(t, "/cth-test", map[string]string{
		"Authorization": "Bearer " + token,
	})
	if code != 200 {
		t.Fatalf("expected 200, got %d: %s", code, body)
	}
	if !strings.Contains(body, "sub=user-42") {
		t.Errorf("X-User-Id not injected: %q", body)
	}
	if !strings.Contains(body, "role=admin") {
		t.Errorf("X-User-Role not injected: %q", body)
	}
}

// TestE2E_Proxy_StreamingFlush verifies that the proxy streams chunked
// responses without buffering the entire body.
func TestE2E_Proxy_StreamingFlush(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		for i := 0; i < 3; i++ {
			fmt.Fprintf(w, "chunk-%d\n", i)
			flusher.Flush()
			time.Sleep(50 * time.Millisecond)
		}
	}))
	t.Cleanup(up.Close)
	addr := up.Listener.Addr().(*net.TCPAddr)

	destID := createDestination(t, "stream-dest", addr.IP.String(), addr.Port)
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "stream-route",
		"match": map[string]any{"pathPrefix": "/stream-test"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(proxyURL + "/stream-test")
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	data, _ := io.ReadAll(resp.Body)
	body := string(data)
	for i := 0; i < 3; i++ {
		want := fmt.Sprintf("chunk-%d", i)
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in streamed response: %q", want, body)
		}
	}
}

// TestE2E_Middleware_ChainOrdering verifies that all middlewares in the chain
// execute. Two header middlewares set different headers — both must be present.
// The first middleware in middlewareIds wraps outermost and runs last on
// response, so its Set overwrites if the key collides. We use distinct keys.
func TestE2E_Middleware_ChainOrdering(t *testing.T) {
	_, mw1 := apiPost(t, "/middlewares", map[string]any{
		"name": "order-first",
		"type": "headers",
		"headers": map[string]any{
			"responseHeadersToAdd": []map[string]any{
				{"key": "X-First", "value": "one"},
			},
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw1))

	_, mw2 := apiPost(t, "/middlewares", map[string]any{
		"name": "order-second",
		"type": "headers",
		"headers": map[string]any{
			"responseHeadersToAdd": []map[string]any{
				{"key": "X-Second", "value": "two"},
			},
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw2))

	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "chain-order",
		"match":          map[string]any{"pathPrefix": "/chain-order"},
		"directResponse": map[string]any{"status": 200, "body": "ok"},
		"middlewareIds":   []string{id(mw1), id(mw2)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	_, hdrs, _ := proxyGet(t, "/chain-order", nil)
	if hdrs.Get("X-First") != "one" {
		t.Errorf("first middleware not applied: X-First=%q", hdrs.Get("X-First"))
	}
	if hdrs.Get("X-Second") != "two" {
		t.Errorf("second middleware not applied: X-Second=%q", hdrs.Get("X-Second"))
	}
}

// TestE2E_ProxyError_DNSFailure verifies that forwarding to an unresolvable
// host produces a structured JSON error with type "dns_failure".
func TestE2E_ProxyError_DNSFailure(t *testing.T) {
	destID := createDestination(t, "dns-fail-dest", "this-host-does-not-exist.invalid", 9999)
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "dns-fail-route",
		"match": map[string]any{"pathPrefix": "/dns-fail"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, headers, body := proxyGet(t, "/dns-fail", nil)
	if code != 502 {
		t.Errorf("expected 502, got %d", code)
	}
	ct := headers.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected application/json, got %q", ct)
	}
	var errResp map[string]any
	if err := json.Unmarshal([]byte(body), &errResp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	errType, _ := errResp["error"].(string)
	if errType != "dns_failure" {
		t.Errorf("expected error type dns_failure, got %q", errType)
	}
}

// TestE2E_FaultIsolation_BadRegex verifies that a route with an invalid regex
// in the match rule does not prevent other routes from working. The broken
// route is skipped during snapshot compilation and the rest function normally.
func TestE2E_FaultIsolation_BadRegex(t *testing.T) {
	_, goodRoute := apiPost(t, "/routes", map[string]any{
		"name":           "good-route",
		"match":          map[string]any{"pathPrefix": "/fault-good"},
		"directResponse": map[string]any{"status": 200, "body": "good"},
	})
	defer apiDelete(t, "/routes/"+id(goodRoute))

	_, badRoute := apiPost(t, "/routes", map[string]any{
		"name": "bad-regex-route",
		"match": map[string]any{
			"pathRegex": "[invalid(regex",
		},
		"directResponse": map[string]any{"status": 200, "body": "bad"},
	})
	defer apiDelete(t, "/routes/"+id(badRoute))

	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, body := proxyGet(t, "/fault-good", nil)
	if code != 200 || body != "good" {
		t.Errorf("good route should work despite bad regex route: %d %q", code, body)
	}
}
