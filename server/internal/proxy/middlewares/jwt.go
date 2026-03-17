package middlewares

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/achetronic/rutoso/internal/model"
)

// JWTMiddleware creates a middleware that validates JWT tokens.
// Supports remote JWKS (fetched from a Destination) and inline JWKS.
func JWTMiddleware(cfg *model.JWTConfig, services map[string]Service) Middleware {
	if cfg == nil || len(cfg.Providers) == 0 {
		return func(next http.Handler) http.Handler { return next }
	}

	validators := make(map[string]*jwtValidator)
	for name, p := range cfg.Providers {
		v := &jwtValidator{
			issuer:    p.Issuer,
			audiences: p.Audiences,
			forward:   p.ForwardJWT,
			claimToHeaders: p.ClaimToHeaders,
		}

		if p.JWKsInline != "" {
			keys, _ := parseJWKS([]byte(p.JWKsInline))
			v.keys = keys
		} else if p.JWKsURI != "" && p.JWKsDestinationID != "" {
			if svc, ok := services[p.JWKsDestinationID]; ok {
				v.jwksURL = svc.BaseURL + p.JWKsURI
				v.transport = svc.Transport
				// Fetch keys on startup.
				v.refreshKeys()
				// Refresh periodically.
				go v.refreshLoop()
			}
		}

		validators[name] = v
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if token == "" {
				// Check rules for allow-missing.
				for _, rule := range cfg.Rules {
					if strings.HasPrefix(r.URL.Path, rule.Match) && rule.AllowMissing {
						next.ServeHTTP(w, r)
						return
					}
				}
				http.Error(w, "missing authorization token", http.StatusUnauthorized)
				return
			}

			// Parse the JWT header to get kid.
			parts := strings.Split(token, ".")
			if len(parts) != 3 {
				http.Error(w, "invalid token format", http.StatusUnauthorized)
				return
			}

			// Decode claims.
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

			// Validate against each provider.
			validated := false
			for _, v := range validators {
				if v.validate(claims) {
					validated = true
					// Apply claim-to-header mappings.
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

type jwtValidator struct {
	issuer         string
	audiences      []string
	forward        bool
	claimToHeaders []model.JWTClaimHeader
	keys           []rsaKey
	jwksURL        string
	transport      http.RoundTripper
	mu             sync.RWMutex
}

type rsaKey struct {
	kid string
	key *rsa.PublicKey
}

func (v *jwtValidator) validate(claims map[string]interface{}) bool {
	// Check issuer.
	if v.issuer != "" {
		iss, ok := claims["iss"].(string)
		if !ok || iss != v.issuer {
			return false
		}
	}

	// Check audience.
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

	// Check expiry.
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
	for range ticker.C {
		v.refreshKeys()
	}
}

func (v *jwtValidator) refreshKeys() {
	if v.jwksURL == "" {
		return
	}

	client := &http.Client{Transport: v.transport, Timeout: 10 * time.Second}
	resp, err := client.Get(v.jwksURL)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}

	keys, err := parseJWKS(body)
	if err != nil {
		return
	}

	v.mu.Lock()
	v.keys = keys
	v.mu.Unlock()
}

// parseJWKS parses a JSON Web Key Set document and extracts RSA public keys.
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
		// Try as PEM.
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
