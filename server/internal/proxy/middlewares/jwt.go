// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package middlewares

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/achetronic/vrata/internal/model"
	"github.com/achetronic/vrata/internal/proxy/celeval"
)

// JWTMiddleware creates a JWT validation middleware.
func JWTMiddleware(cfg *model.JWTConfig, services map[string]Service) Middleware {
	m, _ := JWTMiddlewareWithStop(cfg, services)
	return m
}

// JWTMiddlewareWithStop creates a JWT validation middleware and returns a
// stop function that shuts down background JWKS refresh goroutines. The
// stop function must be called when the routing table is replaced to
// prevent goroutine leaks.
func JWTMiddlewareWithStop(cfg *model.JWTConfig, services map[string]Service) (Middleware, func()) {
	if cfg == nil || cfg.Issuer == "" {
		return passthrough, func() {}
	}

	v := &jwtValidator{
		issuer:    cfg.Issuer,
		audiences: cfg.Audiences,
		forward:   cfg.ForwardJWT,
		stop:      make(chan struct{}),
	}

	if cfg.JWKsInline != "" {
		keys, err := parseJWKS([]byte(cfg.JWKsInline))
		if err != nil {
			slog.Error("jwt: failed to parse inline JWKS",
				slog.String("error", err.Error()),
			)
		}
		v.keys = keys
	} else if cfg.JWKsPath != "" && cfg.JWKsDestinationID != "" {
		if svc, ok := services[cfg.JWKsDestinationID]; ok {
			v.jwksURL = svc.BaseURL + cfg.JWKsPath
			v.transport = svc.Transport
			v.jwksTimeout = 10 * time.Second
			if cfg.JWKsRetrievalTimeout != "" {
				if d, err := time.ParseDuration(cfg.JWKsRetrievalTimeout); err == nil {
					v.jwksTimeout = d
				}
			}
			v.refreshKeys()
			go v.refreshLoop()
		}
	}

	// Pre-compile assertClaims CEL expressions.
	var claimsPrograms []*celeval.ClaimsProgram
	for _, expr := range cfg.AssertClaims {
		prg, err := celeval.CompileClaims(expr)
		if err != nil {
			slog.Error("jwt: invalid assertClaims expression, skipping",
				slog.String("expr", expr),
				slog.String("error", err.Error()),
			)
			continue
		}
		claimsPrograms = append(claimsPrograms, prg)
	}

	// Pre-compile claimToHeaders CEL expressions.
	type compiledClaimHeader struct {
		program *celeval.ClaimsStringProgram
		header  string
	}
	var claimHeaders []compiledClaimHeader
	for _, cth := range cfg.ClaimToHeaders {
		prg, err := celeval.CompileClaimsString(cth.Expr)
		if err != nil {
			slog.Error("jwt: invalid claimToHeaders expression, skipping",
				slog.String("expr", cth.Expr),
				slog.String("error", err.Error()),
			)
			continue
		}
		claimHeaders = append(claimHeaders, compiledClaimHeader{program: prg, header: cth.Header})
	}

	mw := Middleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if token == "" {
				writeJSONError(w, http.StatusUnauthorized, "missing authorization token")
				return
			}

			parts := strings.Split(token, ".")
			if len(parts) != 3 {
				writeJSONError(w, http.StatusUnauthorized, "invalid token format")
				return
			}

			headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
			if err != nil {
				writeJSONError(w, http.StatusUnauthorized, "invalid token header")
				return
			}
			var header jwtHeader
			if err := json.Unmarshal(headerJSON, &header); err != nil {
				writeJSONError(w, http.StatusUnauthorized, "invalid token header")
				return
			}

			claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
			if err != nil {
				writeJSONError(w, http.StatusUnauthorized, "invalid token claims")
				return
			}
			var claims map[string]interface{}
			if err := json.Unmarshal(claimsJSON, &claims); err != nil {
				writeJSONError(w, http.StatusUnauthorized, "invalid token claims")
				return
			}

			signature, err := base64.RawURLEncoding.DecodeString(parts[2])
			if err != nil {
				writeJSONError(w, http.StatusUnauthorized, "invalid token signature")
				return
			}

			signedContent := parts[0] + "." + parts[1]

			if !v.validateSignatureAndClaims(header, claims, signedContent, signature) {
				writeJSONError(w, http.StatusUnauthorized, "token validation failed")
				return
			}

			for _, ch := range claimHeaders {
				if val := ch.program.Eval(claims); val != "" {
					r.Header.Set(ch.header, val)
				}
			}
			if !v.forward {
				r.Header.Del("Authorization")
			}

			// Evaluate assertClaims expressions against the decoded claims.
			for _, prg := range claimsPrograms {
				if !prg.Eval(claims) {
					writeJSONError(w, http.StatusForbidden, "claim assertion failed")
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	})

	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() { close(v.stop) })
	}

	return mw, stop
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

type jwtValidator struct {
	issuer    string
	audiences []string
	forward   bool
	keys      []verifierKey
	jwksURL            string
	jwksTimeout        time.Duration
	transport          http.RoundTripper
	mu             sync.RWMutex
	stop           chan struct{}
}

// verifierKey is a parsed public key that can verify JWT signatures.
type verifierKey struct {
	kid string
	alg string
	key crypto.PublicKey
}

// verify checks the signature over signedContent using this key.
func (vk verifierKey) verify(alg string, signedContent, signature []byte) bool {
	switch k := vk.key.(type) {
	case *rsa.PublicKey:
		hash, hashFunc := rsaHashForAlg(alg)
		h := hash(signedContent)
		if strings.HasPrefix(alg, "PS") {
			return rsa.VerifyPSS(k, hashFunc, h, signature, nil) == nil
		}
		return rsa.VerifyPKCS1v15(k, hashFunc, h, signature) == nil

	case *ecdsa.PublicKey:
		hash := ecHashForCurve(k.Curve)
		h := hash(signedContent)
		sig := ecConvertP1363ToASN1(signature, k.Curve)
		if sig == nil {
			return false
		}
		return ecdsa.VerifyASN1(k, h, sig)

	case ed25519.PublicKey:
		return ed25519.Verify(k, signedContent, signature)
	}
	return false
}

// rsaHashForAlg returns the hash function and crypto.Hash for the given RSA alg.
func rsaHashForAlg(alg string) (func([]byte) []byte, crypto.Hash) {
	switch alg {
	case "RS384", "PS384":
		return func(b []byte) []byte { h := sha512.Sum384(b); return h[:] }, crypto.SHA384
	case "RS512", "PS512":
		return func(b []byte) []byte { h := sha512.Sum512(b); return h[:] }, crypto.SHA512
	default:
		return func(b []byte) []byte { h := sha256.Sum256(b); return h[:] }, crypto.SHA256
	}
}

// ecHashForCurve returns the hash function matching the EC curve.
func ecHashForCurve(curve elliptic.Curve) func([]byte) []byte {
	switch curve {
	case elliptic.P384():
		return func(b []byte) []byte { h := sha512.Sum384(b); return h[:] }
	case elliptic.P521():
		return func(b []byte) []byte { h := sha512.Sum512(b); return h[:] }
	default:
		return func(b []byte) []byte { h := sha256.Sum256(b); return h[:] }
	}
}

// ecConvertP1363ToASN1 converts a JWT-style R||S signature to ASN.1 DER
// format for use with ecdsa.VerifyASN1.
func ecConvertP1363ToASN1(sig []byte, curve elliptic.Curve) []byte {
	byteLen := (curve.Params().BitSize + 7) / 8
	if len(sig) != 2*byteLen {
		return nil
	}
	r := new(big.Int).SetBytes(sig[:byteLen])
	s := new(big.Int).SetBytes(sig[byteLen:])

	// ASN.1 DER encoding: SEQUENCE { INTEGER r, INTEGER s }
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	// Pad with 0x00 if high bit set
	if len(rBytes) > 0 && rBytes[0]&0x80 != 0 {
		rBytes = append([]byte{0}, rBytes...)
	}
	if len(sBytes) > 0 && sBytes[0]&0x80 != 0 {
		sBytes = append([]byte{0}, sBytes...)
	}
	der := []byte{0x30, byte(len(rBytes) + len(sBytes) + 4),
		0x02, byte(len(rBytes))}
	der = append(der, rBytes...)
	der = append(der, 0x02, byte(len(sBytes)))
	der = append(der, sBytes...)
	return der
}

func (v *jwtValidator) validateSignatureAndClaims(
	header jwtHeader,
	claims map[string]interface{},
	signedContent string,
	signature []byte,
) bool {
	if !v.verifySignature(header, signedContent, signature) {
		return false
	}
	return v.validateClaims(claims)
}

func (v *jwtValidator) verifySignature(header jwtHeader, signedContent string, signature []byte) bool {
	v.mu.RLock()
	keys := v.keys
	v.mu.RUnlock()

	if len(keys) == 0 {
		return false
	}

	content := []byte(signedContent)
	for _, k := range keys {
		if header.Kid != "" && k.kid != "" && header.Kid != k.kid {
			continue
		}
		if k.verify(header.Alg, content, signature) {
			return true
		}
	}
	return false
}

func (v *jwtValidator) validateClaims(claims map[string]interface{}) bool {
	if v.issuer != "" {
		iss, ok := claims["iss"].(string)
		if !ok || iss != v.issuer {
			return false
		}
	}

	if len(v.audiences) > 0 {
		aud := extractAudience(claims)
		found := false
		for _, a := range v.audiences {
			for _, ca := range aud {
				if a == ca {
					found = true
					break
				}
			}
		}
		if !found {
			return false
		}
	}

	exp, ok := claims["exp"].(float64)
	if !ok {
		return false
	}
	now := time.Now().Unix()
	if now > int64(exp) {
		return false
	}

	if nbf, ok := claims["nbf"].(float64); ok {
		if now < int64(nbf) {
			return false
		}
	}

	return true
}

func extractAudience(claims map[string]interface{}) []string {
	switch v := claims["aud"].(type) {
	case string:
		return []string{v}
	case []interface{}:
		var auds []string
		for _, a := range v {
			if s, ok := a.(string); ok {
				auds = append(auds, s)
			}
		}
		return auds
	}
	return nil
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return auth[7:]
	}
	return ""
}

func (v *jwtValidator) refreshLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			v.refreshKeys()
		case <-v.stop:
			return
		}
	}
}

func (v *jwtValidator) refreshKeys() {
	if v.jwksURL == "" {
		return
	}

	client := &http.Client{Transport: v.transport, Timeout: v.jwksTimeout}
	resp, err := client.Get(v.jwksURL)
	if err != nil {
		slog.Warn("jwt: failed to refresh JWKS", slog.String("url", v.jwksURL), slog.String("error", err.Error()))
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Warn("jwt: failed to read JWKS response", slog.String("url", v.jwksURL), slog.String("error", err.Error()))
		return
	}

	keys, err := parseJWKS(body)
	if err != nil {
		slog.Warn("jwt: failed to parse JWKS", slog.String("error", err.Error()))
		return
	}

	v.mu.Lock()
	v.keys = keys
	v.mu.Unlock()
}

// parseJWKS parses a JSON Web Key Set and extracts public keys.
// Supports RSA, EC (P-256, P-384, P-521), and OKP (Ed25519) key types.
// Also supports raw PEM-encoded public keys as fallback.
func parseJWKS(data []byte) ([]verifierKey, error) {
	var jwks struct {
		Keys []jwkEntry `json:"keys"`
	}

	if err := json.Unmarshal(data, &jwks); err != nil {
		return parsePEM(data)
	}

	var keys []verifierKey
	for _, k := range jwks.Keys {
		vk, err := parseJWK(k)
		if err != nil {
			slog.Warn("jwt: skipping unparseable JWK",
				slog.String("kid", k.Kid),
				slog.String("error", err.Error()),
			)
			continue
		}
		keys = append(keys, vk)
	}

	return keys, nil
}

type jwkEntry struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	// RSA
	N string `json:"n"`
	E string `json:"e"`
	// EC
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	// OKP (Ed25519)
	// X is reused for the public key bytes
}

func parseJWK(k jwkEntry) (verifierKey, error) {
	switch k.Kty {
	case "RSA":
		return parseRSAJWK(k)
	case "EC":
		return parseECJWK(k)
	case "OKP":
		return parseOKPJWK(k)
	}
	return verifierKey{}, fmt.Errorf("unsupported key type: %s", k.Kty)
}

func parseRSAJWK(k jwkEntry) (verifierKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return verifierKey{}, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return verifierKey{}, err
	}

	return verifierKey{
		kid: k.Kid,
		alg: k.Alg,
		key: &rsa.PublicKey{
			N: new(big.Int).SetBytes(nBytes),
			E: int(new(big.Int).SetBytes(eBytes).Int64()),
		},
	}, nil
}

func parseECJWK(k jwkEntry) (verifierKey, error) {
	xBytes, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return verifierKey{}, err
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(k.Y)
	if err != nil {
		return verifierKey{}, err
	}

	var curve elliptic.Curve
	switch k.Crv {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return verifierKey{}, fmt.Errorf("unsupported EC curve: %s", k.Crv)
	}

	return verifierKey{
		kid: k.Kid,
		alg: k.Alg,
		key: &ecdsa.PublicKey{
			Curve: curve,
			X:     new(big.Int).SetBytes(xBytes),
			Y:     new(big.Int).SetBytes(yBytes),
		},
	}, nil
}

func parseOKPJWK(k jwkEntry) (verifierKey, error) {
	if k.Crv != "Ed25519" {
		return verifierKey{}, fmt.Errorf("unsupported OKP curve: %s", k.Crv)
	}
	xBytes, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return verifierKey{}, err
	}
	if len(xBytes) != ed25519.PublicKeySize {
		return verifierKey{}, fmt.Errorf("invalid Ed25519 key size: %d", len(xBytes))
	}

	return verifierKey{
		kid: k.Kid,
		alg: k.Alg,
		key: ed25519.PublicKey(xBytes),
	}, nil
}

func parsePEM(data []byte) ([]verifierKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	switch k := pub.(type) {
	case *rsa.PublicKey:
		return []verifierKey{{key: k}}, nil
	case *ecdsa.PublicKey:
		return []verifierKey{{key: k}}, nil
	case ed25519.PublicKey:
		return []verifierKey{{key: k}}, nil
	}
	return nil, fmt.Errorf("unsupported PEM key type")
}
