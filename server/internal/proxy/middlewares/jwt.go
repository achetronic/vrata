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
	"hash"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/achetronic/rutoso/internal/model"
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
	if cfg == nil || len(cfg.Providers) == 0 {
		return passthrough, nil
	}

	validators := make(map[string]*jwtValidator)
	for name, p := range cfg.Providers {
		v := &jwtValidator{
			issuer:         p.Issuer,
			audiences:      p.Audiences,
			forward:        p.ForwardJWT,
			claimToHeaders: p.ClaimToHeaders,
			stop:           make(chan struct{}),
		}

		if p.JWKsInline != "" {
			keys, err := parseJWKS([]byte(p.JWKsInline))
			if err != nil {
				slog.Error("jwt: failed to parse inline JWKS",
					slog.String("provider", name),
					slog.String("error", err.Error()),
				)
			}
			v.keys = keys
		} else if p.JWKsURI != "" && p.JWKsDestinationID != "" {
			if svc, ok := services[p.JWKsDestinationID]; ok {
				v.jwksURL = svc.BaseURL + p.JWKsURI
				v.transport = svc.Transport
				v.refreshKeys()
				go v.refreshLoop()
			}
		}

		validators[name] = v
	}

	mw := Middleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if token == "" {
				for _, rule := range cfg.Rules {
					if strings.HasPrefix(r.URL.Path, rule.Match) && rule.AllowMissing {
						next.ServeHTTP(w, r)
						return
					}
				}
				http.Error(w, "missing authorization token", http.StatusUnauthorized)
				return
			}

			parts := strings.Split(token, ".")
			if len(parts) != 3 {
				http.Error(w, "invalid token format", http.StatusUnauthorized)
				return
			}

			headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
			if err != nil {
				http.Error(w, "invalid token header", http.StatusUnauthorized)
				return
			}
			var header jwtHeader
			if err := json.Unmarshal(headerJSON, &header); err != nil {
				http.Error(w, "invalid token header", http.StatusUnauthorized)
				return
			}

			claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
			if err != nil {
				http.Error(w, "invalid token claims", http.StatusUnauthorized)
				return
			}
			var claims map[string]interface{}
			if err := json.Unmarshal(claimsJSON, &claims); err != nil {
				http.Error(w, "invalid token claims", http.StatusUnauthorized)
				return
			}

			signature, err := base64.RawURLEncoding.DecodeString(parts[2])
			if err != nil {
				http.Error(w, "invalid token signature", http.StatusUnauthorized)
				return
			}

			signedContent := parts[0] + "." + parts[1]

			validated := false
			for _, v := range validators {
				if v.validateSignatureAndClaims(header, claims, signedContent, signature) {
					validated = true
					for _, cth := range v.claimToHeaders {
						if val, ok := claims[cth.Claim]; ok {
							r.Header.Set(cth.Header, fmt.Sprintf("%v", val))
						}
					}
					if !v.forward {
						r.Header.Del("Authorization")
					}
					break
				}
			}

			if !validated {
				http.Error(w, "token validation failed", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	})

	stop := func() {
		for _, v := range validators {
			select {
			case v.stop <- struct{}{}:
			default:
			}
		}
	}

	return mw, stop
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

type jwtValidator struct {
	issuer         string
	audiences      []string
	forward        bool
	claimToHeaders []model.JWTClaimHeader
	keys           []verifierKey
	jwksURL        string
	transport      http.RoundTripper
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
func (vk verifierKey) verify(signedContent, signature []byte) bool {
	switch k := vk.key.(type) {
	case *rsa.PublicKey:
		h := sha256.Sum256(signedContent)
		return rsa.VerifyPKCS1v15(k, crypto.SHA256, h[:], signature) == nil

	case *ecdsa.PublicKey:
		var h []byte
		switch k.Curve {
		case elliptic.P384():
			s := sha512.Sum384(signedContent)
			h = s[:]
		case elliptic.P521():
			s := sha512.Sum512(signedContent)
			h = s[:]
		default:
			s := sha256.Sum256(signedContent)
			h = s[:]
		}
		return ecdsa.VerifyASN1(k, h, signature)

	case ed25519.PublicKey:
		return ed25519.Verify(k, signedContent, signature)
	}
	return false
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
		if k.verify(content, signature) {
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

	if exp, ok := claims["exp"].(float64); ok {
		if time.Now().Unix() > int64(exp) {
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

	client := &http.Client{Transport: v.transport, Timeout: 10 * time.Second}
	resp, err := client.Get(v.jwksURL)
	if err != nil {
		slog.Warn("jwt: failed to refresh JWKS", slog.String("url", v.jwksURL), slog.String("error", err.Error()))
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
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

// suppress unused import warning for hash
var _ hash.Hash
