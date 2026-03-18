package middlewares

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/achetronic/rutoso/internal/model"
)

func generateTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func makeJWKS(t *testing.T, pub *rsa.PublicKey, kid string) string {
	t.Helper()
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
	jwks := fmt.Sprintf(`{"keys":[{"kty":"RSA","kid":"%s","n":"%s","e":"%s"}]}`, kid, n, e)
	return jwks
}

func signJWT(t *testing.T, key *rsa.PrivateKey, header, claims map[string]interface{}) string {
	t.Helper()
	hJSON, _ := json.Marshal(header)
	cJSON, _ := json.Marshal(claims)
	hEnc := base64.RawURLEncoding.EncodeToString(hJSON)
	cEnc := base64.RawURLEncoding.EncodeToString(cJSON)
	signed := hEnc + "." + cEnc
	hash := sha256.Sum256([]byte(signed))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	return signed + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestJWTValidSignature(t *testing.T) {
	key := generateTestKey(t)
	jwks := makeJWKS(t, &key.PublicKey, "test-kid")

	cfg := &model.JWTConfig{
		Providers: map[string]model.JWTProvider{
			"default": {
				Issuer:     "test-issuer",
				Audiences:  []string{"test-aud"},
				JWKsInline: jwks,
			},
		},
	}

	mw := JWTMiddleware(cfg, nil)
	var reached bool
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(200)
	}))

	token := signJWT(t, key, map[string]interface{}{
		"alg": "RS256",
		"kid": "test-kid",
	}, map[string]interface{}{
		"iss": "test-issuer",
		"aud": "test-aud",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !reached {
		t.Errorf("expected handler to be reached, got status %d", w.Code)
	}
}

func TestJWTInvalidSignature(t *testing.T) {
	key := generateTestKey(t)
	wrongKey := generateTestKey(t)
	jwks := makeJWKS(t, &key.PublicKey, "test-kid")

	cfg := &model.JWTConfig{
		Providers: map[string]model.JWTProvider{
			"default": {
				Issuer:     "test-issuer",
				JWKsInline: jwks,
			},
		},
	}

	mw := JWTMiddleware(cfg, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	token := signJWT(t, wrongKey, map[string]interface{}{
		"alg": "RS256",
		"kid": "test-kid",
	}, map[string]interface{}{
		"iss": "test-issuer",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("expected 401 for invalid signature, got %d", w.Code)
	}
}

func TestJWTExpired(t *testing.T) {
	key := generateTestKey(t)
	jwks := makeJWKS(t, &key.PublicKey, "kid1")

	cfg := &model.JWTConfig{
		Providers: map[string]model.JWTProvider{
			"default": {
				Issuer:     "iss",
				JWKsInline: jwks,
			},
		},
	}

	mw := JWTMiddleware(cfg, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	token := signJWT(t, key, map[string]interface{}{"alg": "RS256", "kid": "kid1"},
		map[string]interface{}{"iss": "iss", "exp": float64(time.Now().Add(-time.Hour).Unix())})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("expected 401 for expired token, got %d", w.Code)
	}
}

func TestJWTMissingToken(t *testing.T) {
	cfg := &model.JWTConfig{
		Providers: map[string]model.JWTProvider{"default": {Issuer: "iss"}},
	}

	mw := JWTMiddleware(cfg, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))

	if w.Code != 401 {
		t.Errorf("expected 401 for missing token, got %d", w.Code)
	}
}

func TestJWTAllowMissing(t *testing.T) {
	cfg := &model.JWTConfig{
		Providers: map[string]model.JWTProvider{"default": {Issuer: "iss"}},
		Rules:     []model.JWTRule{{Match: "/public", AllowMissing: true}},
	}

	mw := JWTMiddleware(cfg, nil)
	var reached bool
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(200)
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/public/page", nil))

	if !reached {
		t.Errorf("expected handler reached for allow-missing path, got %d", w.Code)
	}
}

func TestJWTClaimToHeaders(t *testing.T) {
	key := generateTestKey(t)
	jwks := makeJWKS(t, &key.PublicKey, "kid1")

	cfg := &model.JWTConfig{
		Providers: map[string]model.JWTProvider{
			"default": {
				Issuer:     "iss",
				JWKsInline: jwks,
				ClaimToHeaders: []model.JWTClaimHeader{
					{Claim: "sub", Header: "X-User-ID"},
				},
			},
		},
	}

	mw := JWTMiddleware(cfg, nil)
	var userID string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID = r.Header.Get("X-User-ID")
		w.WriteHeader(200)
	}))

	token := signJWT(t, key, map[string]interface{}{"alg": "RS256", "kid": "kid1"},
		map[string]interface{}{"iss": "iss", "sub": "user-42", "exp": float64(time.Now().Add(time.Hour).Unix())})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if userID != "user-42" {
		t.Errorf("expected X-User-ID=user-42, got %q", userID)
	}
}

func TestJWTForwardJWTFalse(t *testing.T) {
	key := generateTestKey(t)
	jwks := makeJWKS(t, &key.PublicKey, "kid1")

	cfg := &model.JWTConfig{
		Providers: map[string]model.JWTProvider{
			"default": {
				Issuer:     "iss",
				JWKsInline: jwks,
				ForwardJWT: false,
			},
		},
	}

	mw := JWTMiddleware(cfg, nil)
	var hasAuth bool
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hasAuth = r.Header.Get("Authorization") != ""
		w.WriteHeader(200)
	}))

	token := signJWT(t, key, map[string]interface{}{"alg": "RS256", "kid": "kid1"},
		map[string]interface{}{"iss": "iss", "exp": float64(time.Now().Add(time.Hour).Unix())})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if hasAuth {
		t.Error("expected Authorization header to be stripped")
	}
}

func TestJWTNilConfig(t *testing.T) {
	mw := JWTMiddleware(nil, nil)
	w := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))

	if w.Code != 200 {
		t.Errorf("expected passthrough 200, got %d", w.Code)
	}
}

func TestJWTRemoteJWKS(t *testing.T) {
	key := generateTestKey(t)
	jwks := makeJWKS(t, &key.PublicKey, "remote-kid")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(jwks))
	}))
	defer srv.Close()

	cfg := &model.JWTConfig{
		Providers: map[string]model.JWTProvider{
			"default": {
				Issuer:            "iss",
				JWKsURI:           "/.well-known/jwks.json",
				JWKsDestinationID: "jwks-svc",
			},
		},
	}

	services := map[string]Service{
		"jwks-svc": {BaseURL: srv.URL},
	}

	mw := JWTMiddleware(cfg, services)
	var reached bool
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(200)
	}))

	token := signJWT(t, key, map[string]interface{}{"alg": "RS256", "kid": "remote-kid"},
		map[string]interface{}{"iss": "iss", "exp": float64(time.Now().Add(time.Hour).Unix())})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !reached {
		t.Errorf("expected handler reached with remote JWKS, got %d", w.Code)
	}
}

// ─── EC (P-256) ─────────────────────────────────────────────────────────────

func makeECJWKS(t *testing.T, pub *ecdsa.PublicKey, kid, crv string) string {
	t.Helper()
	x := base64.RawURLEncoding.EncodeToString(pub.X.Bytes())
	y := base64.RawURLEncoding.EncodeToString(pub.Y.Bytes())
	return fmt.Sprintf(`{"keys":[{"kty":"EC","kid":"%s","crv":"%s","x":"%s","y":"%s"}]}`, kid, crv, x, y)
}

func signECJWT(t *testing.T, key *ecdsa.PrivateKey, header, claims map[string]interface{}) string {
	t.Helper()
	hJSON, _ := json.Marshal(header)
	cJSON, _ := json.Marshal(claims)
	hEnc := base64.RawURLEncoding.EncodeToString(hJSON)
	cEnc := base64.RawURLEncoding.EncodeToString(cJSON)
	signed := hEnc + "." + cEnc
	hash := sha256.Sum256([]byte(signed))
	r, s, err := ecdsa.Sign(rand.Reader, key, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	byteLen := (key.Curve.Params().BitSize + 7) / 8
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	sig := make([]byte, 2*byteLen)
	copy(sig[byteLen-len(rBytes):byteLen], rBytes)
	copy(sig[2*byteLen-len(sBytes):], sBytes)
	return signed + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestJWTECSignature(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	jwks := makeECJWKS(t, &key.PublicKey, "ec-kid", "P-256")

	cfg := &model.JWTConfig{
		Providers: map[string]model.JWTProvider{
			"default": {Issuer: "iss", JWKsInline: jwks},
		},
	}

	mw := JWTMiddleware(cfg, nil)
	var reached bool
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(200)
	}))

	token := signECJWT(t, key, map[string]interface{}{"alg": "ES256", "kid": "ec-kid"},
		map[string]interface{}{"iss": "iss", "exp": float64(time.Now().Add(time.Hour).Unix())})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !reached {
		t.Errorf("expected handler reached with EC key, got %d", w.Code)
	}
}

func TestJWTECInvalidSignature(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	wrongKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	jwks := makeECJWKS(t, &key.PublicKey, "ec-kid", "P-256")

	cfg := &model.JWTConfig{
		Providers: map[string]model.JWTProvider{
			"default": {Issuer: "iss", JWKsInline: jwks},
		},
	}

	mw := JWTMiddleware(cfg, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	token := signECJWT(t, wrongKey, map[string]interface{}{"alg": "ES256", "kid": "ec-kid"},
		map[string]interface{}{"iss": "iss", "exp": float64(time.Now().Add(time.Hour).Unix())})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("expected 401 for wrong EC key, got %d", w.Code)
	}
}

// ─── Ed25519 ────────────────────────────────────────────────────────────────

func makeEd25519JWKS(t *testing.T, pub ed25519.PublicKey, kid string) string {
	t.Helper()
	x := base64.RawURLEncoding.EncodeToString(pub)
	return fmt.Sprintf(`{"keys":[{"kty":"OKP","kid":"%s","crv":"Ed25519","x":"%s"}]}`, kid, x)
}

func signEd25519JWT(t *testing.T, key ed25519.PrivateKey, header, claims map[string]interface{}) string {
	t.Helper()
	hJSON, _ := json.Marshal(header)
	cJSON, _ := json.Marshal(claims)
	hEnc := base64.RawURLEncoding.EncodeToString(hJSON)
	cEnc := base64.RawURLEncoding.EncodeToString(cJSON)
	signed := hEnc + "." + cEnc
	sig := ed25519.Sign(key, []byte(signed))
	return signed + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestJWTEd25519Signature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	jwks := makeEd25519JWKS(t, pub, "ed-kid")

	cfg := &model.JWTConfig{
		Providers: map[string]model.JWTProvider{
			"default": {Issuer: "iss", JWKsInline: jwks},
		},
	}

	mw := JWTMiddleware(cfg, nil)
	var reached bool
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(200)
	}))

	token := signEd25519JWT(t, priv, map[string]interface{}{"alg": "EdDSA", "kid": "ed-kid"},
		map[string]interface{}{"iss": "iss", "exp": float64(time.Now().Add(time.Hour).Unix())})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !reached {
		t.Errorf("expected handler reached with Ed25519 key, got %d", w.Code)
	}
}

func TestJWTEd25519InvalidSignature(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	_, wrongPriv, _ := ed25519.GenerateKey(rand.Reader)
	jwks := makeEd25519JWKS(t, pub, "ed-kid")

	cfg := &model.JWTConfig{
		Providers: map[string]model.JWTProvider{
			"default": {Issuer: "iss", JWKsInline: jwks},
		},
	}

	mw := JWTMiddleware(cfg, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	token := signEd25519JWT(t, wrongPriv, map[string]interface{}{"alg": "EdDSA", "kid": "ed-kid"},
		map[string]interface{}{"iss": "iss", "exp": float64(time.Now().Add(time.Hour).Unix())})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("expected 401 for wrong Ed25519 key, got %d", w.Code)
	}
}

func TestJWTMissingExpClaim(t *testing.T) {
	key := generateTestKey(t)
	jwks := makeJWKS(t, &key.PublicKey, "kid1")

	cfg := &model.JWTConfig{
		Providers: map[string]model.JWTProvider{
			"default": {Issuer: "iss", JWKsInline: jwks},
		},
	}

	mw := JWTMiddleware(cfg, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	token := signJWT(t, key, map[string]interface{}{"alg": "RS256", "kid": "kid1"},
		map[string]interface{}{"iss": "iss"})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("expected 401 for token without exp, got %d", w.Code)
	}
}
