package middlewares

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
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

	"github.com/achetronic/rutoso/internal/model"
)

// JWTMiddleware creates a middleware that validates JWT tokens by verifying
// the RSA signature against keys from JWKS (remote or inline) and checking
// standard claims (issuer, audience, expiry).
func JWTMiddleware(cfg *model.JWTConfig, services map[string]Service) Middleware {
	if cfg == nil || len(cfg.Providers) == 0 {
		return passthrough
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

	return func(next http.Handler) http.Handler {
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
	}
}

// jwtHeader is the decoded JWT header.
type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

type jwtValidator struct {
	issuer         string
	audiences      []string
	forward        bool
	claimToHeaders []model.JWTClaimHeader
	keys           []rsaKey
	jwksURL        string
	transport      http.RoundTripper
	mu             sync.RWMutex
	stop           chan struct{}
}

type rsaKey struct {
	kid string
	key *rsa.PublicKey
}

// validateSignatureAndClaims verifies the RSA signature and checks claims.
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

// verifySignature checks the RSA signature of the JWT.
func (v *jwtValidator) verifySignature(header jwtHeader, signedContent string, signature []byte) bool {
	v.mu.RLock()
	keys := v.keys
	v.mu.RUnlock()

	if len(keys) == 0 {
		return false
	}

	hash := sha256Hash([]byte(signedContent))

	for _, k := range keys {
		if header.Kid != "" && k.kid != "" && header.Kid != k.kid {
			continue
		}
		if err := rsa.VerifyPKCS1v15(k.key, crypto.SHA256, hash, signature); err == nil {
			return true
		}
	}
	return false
}

// validateClaims checks issuer, audience, and expiry.
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

func sha256Hash(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
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

// parseJWKS parses a JSON Web Key Set document and extracts RSA public keys.
// Also supports raw PEM-encoded public keys as fallback.
func parseJWKS(data []byte) ([]rsaKey, error) {
	var jwks struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}

	if err := json.Unmarshal(data, &jwks); err != nil {
		block, _ := pem.Decode(data)
		if block != nil {
			pub, err := x509.ParsePKIXPublicKey(block.Bytes)
			if err == nil {
				if rsaPub, ok := pub.(*rsa.PublicKey); ok {
					return []rsaKey{{key: rsaPub}}, nil
				}
			}
		}
		return nil, err
	}

	var keys []rsaKey
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" {
			continue
		}

		nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}

		n := new(big.Int).SetBytes(nBytes)
		e := int(new(big.Int).SetBytes(eBytes).Int64())

		keys = append(keys, rsaKey{
			kid: k.Kid,
			key: &rsa.PublicKey{N: n, E: e},
		})
	}

	return keys, nil
}
