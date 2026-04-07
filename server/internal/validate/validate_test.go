// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"strings"
	"testing"

	"github.com/achetronic/vrata/internal/model"
)

func emptySnapshot() *model.Snapshot {
	return &model.Snapshot{}
}

func TestCleanSnapshot(t *testing.T) {
	snap := &model.Snapshot{
		Destinations: []model.Destination{{ID: "d1", Name: "backend", Host: "localhost", Port: 8080}},
		Middlewares:  []model.Middleware{{ID: "m1", Name: "cors", Type: model.MiddlewareTypeCORS, CORS: &model.CORSConfig{}}},
		Routes: []model.Route{{
			ID: "r1", Name: "api",
			Match:   model.MatchRule{PathPrefix: "/api"},
			Forward: &model.ForwardAction{Destinations: []model.DestinationRef{{DestinationID: "d1", Weight: 100}}},
			MiddlewareIDs: []string{"m1"},
		}},
		Groups:    []model.RouteGroup{{ID: "g1", Name: "group", RouteIDs: []string{"r1"}, MiddlewareIDs: []string{"m1"}}},
		Listeners: []model.Listener{{ID: "l1", Name: "http", Port: 8080}},
	}
	warnings := Snapshot(snap)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for clean snapshot, got %d: %v", len(warnings), warnings)
	}
}

// ── Regex tests ─────────────────────────────────────────────────────────────

func TestRoutePathRegex_Invalid(t *testing.T) {
	snap := &model.Snapshot{
		Routes: []model.Route{{ID: "r1", Name: "bad-regex", Match: model.MatchRule{PathRegex: "([invalid"}}},
	}
	w := Snapshot(snap)
	requireWarning(t, w, "route", "r1", "pathRegex does not compile")
}

func TestRoutePathRegex_Valid(t *testing.T) {
	snap := &model.Snapshot{
		Routes: []model.Route{{ID: "r1", Name: "ok-regex", Match: model.MatchRule{PathRegex: "^/api/v[0-9]+"}}},
	}
	w := Snapshot(snap)
	requireNoWarningFor(t, w, "r1")
}

func TestRouteHeaderRegex_Invalid(t *testing.T) {
	snap := &model.Snapshot{
		Routes: []model.Route{{ID: "r1", Name: "bad-hdr", Match: model.MatchRule{
			Headers: []model.HeaderMatcher{{Name: "X-Foo", Value: "[bad", Regex: true}},
		}}},
	}
	w := Snapshot(snap)
	requireWarning(t, w, "route", "r1", "headers[X-Foo] regex does not compile")
}

func TestRouteQueryParamRegex_Invalid(t *testing.T) {
	snap := &model.Snapshot{
		Routes: []model.Route{{ID: "r1", Name: "bad-qp", Match: model.MatchRule{
			QueryParams: []model.QueryParamMatcher{{Name: "q", Value: "(bad", Regex: true}},
		}}},
	}
	w := Snapshot(snap)
	requireWarning(t, w, "route", "r1", "queryParams[q] regex does not compile")
}

func TestGroupPathRegex_Invalid(t *testing.T) {
	snap := &model.Snapshot{
		Groups: []model.RouteGroup{{ID: "g1", Name: "bad-grp", PathRegex: "[invalid"}},
	}
	w := Snapshot(snap)
	requireWarning(t, w, "group", "g1", "pathRegex does not compile")
}

func TestGroupHeaderRegex_Invalid(t *testing.T) {
	snap := &model.Snapshot{
		Groups: []model.RouteGroup{{ID: "g1", Name: "bad-grp-hdr", Headers: []model.HeaderMatcher{{Name: "X-Bar", Value: "(?P<broken", Regex: true}}}},
	}
	w := Snapshot(snap)
	requireWarning(t, w, "group", "g1", "headers[X-Bar] regex does not compile")
}

func TestRewriteRegex_Invalid(t *testing.T) {
	snap := &model.Snapshot{
		Routes: []model.Route{{ID: "r1", Name: "bad-rewrite", Forward: &model.ForwardAction{
			Rewrite: &model.RouteRewrite{PathRegex: &model.RewriteRegex{Pattern: "[bad"}},
		}}},
	}
	w := Snapshot(snap)
	requireWarning(t, w, "route", "r1", "rewrite.pathRegex.pattern does not compile")
}

func TestCORSOriginRegex_Invalid(t *testing.T) {
	snap := &model.Snapshot{
		Middlewares: []model.Middleware{{ID: "m1", Name: "cors-bad", Type: model.MiddlewareTypeCORS, CORS: &model.CORSConfig{
			AllowOrigins: []model.CORSOrigin{{Value: "[bad", Regex: true}},
		}}},
	}
	w := Snapshot(snap)
	requireWarning(t, w, "middleware", "m1", "cors.allowOrigins regex")
}

// ── CEL tests ───────────────────────────────────────────────────────────────

func TestRouteCEL_Invalid(t *testing.T) {
	snap := &model.Snapshot{
		Routes: []model.Route{{ID: "r1", Name: "bad-cel", Match: model.MatchRule{CEL: "this is not cel!!!"}}},
	}
	w := Snapshot(snap)
	requireWarning(t, w, "route", "r1", "match.cel does not compile")
}

func TestRouteCEL_Valid(t *testing.T) {
	snap := &model.Snapshot{
		Routes: []model.Route{{ID: "r1", Name: "ok-cel", Match: model.MatchRule{CEL: "request.path.startsWith(\"/api\")"}}},
	}
	w := Snapshot(snap)
	requireNoWarningFor(t, w, "r1")
}

func TestMiddlewareOverrideCEL_Invalid(t *testing.T) {
	snap := &model.Snapshot{
		Routes: []model.Route{{
			ID: "r1", Name: "bad-override",
			MiddlewareOverrides: map[string]model.MiddlewareOverride{
				"m1": {SkipWhen: []string{"not_valid_cel!!!"}},
			},
		}},
	}
	w := Snapshot(snap)
	requireWarning(t, w, "route", "r1", "skipWhen[0] does not compile")
}

func TestGroupOverrideCEL_OnlyWhen_Invalid(t *testing.T) {
	snap := &model.Snapshot{
		Groups: []model.RouteGroup{{
			ID: "g1", Name: "bad-group-override",
			MiddlewareOverrides: map[string]model.MiddlewareOverride{
				"m1": {OnlyWhen: []string{"bad!!!cel"}},
			},
		}},
	}
	w := Snapshot(snap)
	requireWarning(t, w, "group", "g1", "onlyWhen[0] does not compile")
}

func TestJWTAssertClaims_Invalid(t *testing.T) {
	snap := &model.Snapshot{
		Middlewares: []model.Middleware{{
			ID: "m1", Name: "jwt-bad", Type: model.MiddlewareTypeJWT,
			JWT: &model.JWTConfig{Issuer: "test", AssertClaims: []string{"invalid!!!cel"}},
		}},
	}
	w := Snapshot(snap)
	requireWarning(t, w, "middleware", "m1", "jwt.assertClaims[0] does not compile")
}

func TestJWTClaimToHeaders_Invalid(t *testing.T) {
	snap := &model.Snapshot{
		Middlewares: []model.Middleware{{
			ID: "m1", Name: "jwt-cth", Type: model.MiddlewareTypeJWT,
			JWT: &model.JWTConfig{Issuer: "test", ClaimToHeaders: []model.JWTClaimHeader{{Expr: "not!!!cel", Header: "x-sub"}}},
		}},
	}
	w := Snapshot(snap)
	requireWarning(t, w, "middleware", "m1", "jwt.claimToHeaders[0].expr does not compile")
}

// ── TLS tests ───────────────────────────────────────────────────────────────

func TestListenerTLS_InvalidCert(t *testing.T) {
	snap := &model.Snapshot{
		Listeners: []model.Listener{{
			ID: "l1", Name: "bad-tls", Port: 443,
			TLS: &model.ListenerTLS{Cert: "not-a-cert", Key: "not-a-key"},
		}},
	}
	w := Snapshot(snap)
	requireWarning(t, w, "listener", "l1", "tls cert/key pair is invalid")
}

func TestListenerTLS_InvalidCA(t *testing.T) {
	snap := &model.Snapshot{
		Listeners: []model.Listener{{
			ID: "l1", Name: "bad-ca", Port: 443,
			TLS: &model.ListenerTLS{
				Cert:       testCert,
				Key:        testKey,
				ClientAuth: &model.ListenerClientAuth{Mode: "require", CA: "not-a-pem"},
			},
		}},
	}
	w := Snapshot(snap)
	requireWarning(t, w, "listener", "l1", "clientAuth.ca contains no valid PEM")
}

func TestDestinationTLS_InvalidMTLSCert(t *testing.T) {
	snap := &model.Snapshot{
		Destinations: []model.Destination{{
			ID: "d1", Name: "bad-mtls", Host: "localhost", Port: 443,
			Options: &model.DestinationOptions{TLS: &model.TLSOptions{Mode: model.TLSModeMTLS, Cert: "bad", Key: "bad"}},
		}},
	}
	w := Snapshot(snap)
	requireWarning(t, w, "destination", "d1", "client cert/key pair is invalid")
}

func TestDestinationTLS_InvalidCA(t *testing.T) {
	snap := &model.Snapshot{
		Destinations: []model.Destination{{
			ID: "d1", Name: "bad-ca", Host: "localhost", Port: 443,
			Options: &model.DestinationOptions{TLS: &model.TLSOptions{Mode: model.TLSModeTLS, CA: "not-valid-pem"}},
		}},
	}
	w := Snapshot(snap)
	requireWarning(t, w, "destination", "d1", "tls.ca contains no valid PEM")
}

// ── Referential integrity tests ─────────────────────────────────────────────

func TestRouteDestinationRef_Missing(t *testing.T) {
	snap := &model.Snapshot{
		Routes: []model.Route{{
			ID: "r1", Name: "dangling",
			Forward: &model.ForwardAction{Destinations: []model.DestinationRef{{DestinationID: "nonexistent", Weight: 100}}},
		}},
	}
	w := Snapshot(snap)
	requireWarning(t, w, "route", "r1", "references unknown destination \"nonexistent\"")
}

func TestRouteDestinationRef_Exists(t *testing.T) {
	snap := &model.Snapshot{
		Destinations: []model.Destination{{ID: "d1", Name: "backend", Host: "localhost", Port: 8080}},
		Routes: []model.Route{{
			ID: "r1", Name: "ok",
			Forward: &model.ForwardAction{Destinations: []model.DestinationRef{{DestinationID: "d1", Weight: 100}}},
		}},
	}
	w := Snapshot(snap)
	requireNoWarningFor(t, w, "r1")
}

func TestRouteMirrorRef_Missing(t *testing.T) {
	snap := &model.Snapshot{
		Destinations: []model.Destination{{ID: "d1", Name: "backend", Host: "localhost", Port: 8080}},
		Routes: []model.Route{{
			ID: "r1", Name: "mirror-dangling",
			Forward: &model.ForwardAction{
				Destinations: []model.DestinationRef{{DestinationID: "d1", Weight: 100}},
				Mirror:       &model.RouteMirror{DestinationID: "nonexistent"},
			},
		}},
	}
	w := Snapshot(snap)
	requireWarning(t, w, "route", "r1", "mirror references unknown destination")
}

func TestGroupRouteRef_Missing(t *testing.T) {
	snap := &model.Snapshot{
		Groups: []model.RouteGroup{{ID: "g1", Name: "dangling-grp", RouteIDs: []string{"nonexistent"}}},
	}
	w := Snapshot(snap)
	requireWarning(t, w, "group", "g1", "references unknown route \"nonexistent\"")
}

func TestGroupRouteRef_Exists(t *testing.T) {
	snap := &model.Snapshot{
		Routes: []model.Route{{ID: "r1", Name: "real"}},
		Groups: []model.RouteGroup{{ID: "g1", Name: "ok-grp", RouteIDs: []string{"r1"}}},
	}
	w := Snapshot(snap)
	requireNoWarningFor(t, w, "g1")
}

func TestMiddlewareDestinationRef_ExtAuthz_Missing(t *testing.T) {
	snap := &model.Snapshot{
		Middlewares: []model.Middleware{{
			ID: "m1", Name: "authz-bad", Type: model.MiddlewareTypeExtAuthz,
			ExtAuthz: &model.ExtAuthzConfig{DestinationID: "nonexistent"},
		}},
	}
	w := Snapshot(snap)
	requireWarning(t, w, "middleware", "m1", "extAuthz.destinationId references unknown destination")
}

func TestMiddlewareDestinationRef_ExtProc_Missing(t *testing.T) {
	snap := &model.Snapshot{
		Middlewares: []model.Middleware{{
			ID: "m1", Name: "extproc-bad", Type: model.MiddlewareTypeExtProc,
			ExtProc: &model.ExtProcConfig{DestinationID: "nonexistent"},
		}},
	}
	w := Snapshot(snap)
	requireWarning(t, w, "middleware", "m1", "extProc.destinationId references unknown destination")
}

func TestMiddlewareDestinationRef_JWT_Missing(t *testing.T) {
	snap := &model.Snapshot{
		Middlewares: []model.Middleware{{
			ID: "m1", Name: "jwt-bad", Type: model.MiddlewareTypeJWT,
			JWT: &model.JWTConfig{Issuer: "test", JWKsPath: "/jwks", JWKsDestinationID: "nonexistent"},
		}},
	}
	w := Snapshot(snap)
	requireWarning(t, w, "middleware", "m1", "jwt.jwksDestinationId references unknown destination")
}

func TestMiddlewareDestinationRef_Exists(t *testing.T) {
	snap := &model.Snapshot{
		Destinations: []model.Destination{{ID: "d1", Name: "authz-svc", Host: "localhost", Port: 9090}},
		Middlewares: []model.Middleware{{
			ID: "m1", Name: "authz-ok", Type: model.MiddlewareTypeExtAuthz,
			ExtAuthz: &model.ExtAuthzConfig{DestinationID: "d1"},
		}},
	}
	w := Snapshot(snap)
	requireNoWarningFor(t, w, "m1")
}

func TestRouteMiddlewareRef_Missing(t *testing.T) {
	snap := &model.Snapshot{
		Routes: []model.Route{{ID: "r1", Name: "mw-dangling", MiddlewareIDs: []string{"nonexistent"}}},
	}
	w := Snapshot(snap)
	requireWarning(t, w, "route", "r1", "middlewareIds references unknown middleware")
}

func TestGroupMiddlewareRef_Missing(t *testing.T) {
	snap := &model.Snapshot{
		Groups: []model.RouteGroup{{ID: "g1", Name: "grp-mw-bad", MiddlewareIDs: []string{"nonexistent"}}},
	}
	w := Snapshot(snap)
	requireWarning(t, w, "group", "g1", "middlewareIds references unknown middleware")
}

// ── Multiple warnings ───────────────────────────────────────────────────────

func TestMultipleWarnings(t *testing.T) {
	snap := &model.Snapshot{
		Routes: []model.Route{
			{ID: "r1", Name: "bad-regex", Match: model.MatchRule{PathRegex: "[invalid"}},
			{ID: "r2", Name: "bad-cel", Match: model.MatchRule{CEL: "bad!!!"}},
			{ID: "r3", Name: "dangling-dest", Forward: &model.ForwardAction{
				Destinations: []model.DestinationRef{{DestinationID: "gone", Weight: 100}},
			}},
		},
	}
	w := Snapshot(snap)
	if len(w) < 3 {
		t.Errorf("expected at least 3 warnings, got %d", len(w))
	}
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func requireWarning(t *testing.T, warnings []Warning, entity, id, msgSubstr string) {
	t.Helper()
	for _, w := range warnings {
		if w.Entity == entity && w.ID == id && strings.Contains(w.Message, msgSubstr) {
			return
		}
	}
	t.Errorf("expected warning for %s %q containing %q, got %v", entity, id, msgSubstr, warnings)
}

func requireNoWarningFor(t *testing.T, warnings []Warning, id string) {
	t.Helper()
	for _, w := range warnings {
		if w.ID == id {
			t.Errorf("expected no warning for %q, got: %v", id, w)
		}
	}
}

// Self-signed test certificate for TLS validation tests.
const testCert = `-----BEGIN CERTIFICATE-----
MIIBhTCCASugAwIBAgIQIRi6zePL6mKjOipn+dNuaTAKBggqhkjOPQQDAjASMRAw
DgYDVQQKEwdBY21lIENvMB4XDTE3MTAyMDE5NDMwNloXDTE4MTAyMDE5NDMwNlow
EjEQMA4GA1UEChMHQWNtZSBDbzBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABLU3
jSayKRxQ7I9Z5MkdiTnrQ1+wz3OPf1B/NJXzDQGEb7iV+d//k+B5to53TwLyuFgA
5jEFtmFYkymEmQPN7KmjYzBhMA4GA1UdDwEB/wQEAwICpDATBgNVHSUEDDAKBggr
BgEFBQcDATAPBgNVHRMBAf8EBTADAQH/MCkGA1UdEQQiMCCCDmxvY2FsaG9zdDo1
NDUzgg4xMjcuMC4wLjE6NTQ1MzAKBggqhkjOPQQDAgNIADBFAiEA2wpSek3y75Cb
WnxTv7kvna/vTP3XyFXRPwShQXV0m4oCIQD2x39Q4oVwO5/AO1lOYU1UKYvPaR5v
IYruJzOqE9PCTQ==
-----END CERTIFICATE-----`

const testKey = `-----BEGIN EC PRIVATE KEY-----
MHQCAQEEIIrYSSNQFaA2Hwf583QmKbyavkgoftpCYFbQ2O9bDIJUoAcGBSuBBAAi
oWQDYgAEWwOUNLLFJmOi5Mx0knY8zqlP4wBaxibFxAGfv0TYjbiJBQ7DGLxkHMVq
X9aE1JVIHqL1TIFoL0jEUpVke3MBEuEFnCHMqCR2zJdsC0OGLJPNxHK6G0M+QPSP
PLzjKHDl
-----END EC PRIVATE KEY-----`
