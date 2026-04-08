// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package validate performs structural and compilation validation on a
// Snapshot before it is distributed to proxies. Every check is something
// the proxy would fail on deterministically — if it cannot compile here,
// it will not compile on any proxy. Runtime checks (DNS resolution,
// upstream reachability, etc.) are intentionally out of scope.
//
// Validators are registered in a slice and executed in order. Each
// validator appends zero or more Warning entries. The snapshot is never
// mutated.
package validate

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"regexp"

	"github.com/achetronic/vrata/internal/model"
	"github.com/achetronic/vrata/internal/proxy/celeval"
)

// Warning describes a single structural or compilation issue found in
// a snapshot. It identifies the entity that is broken and a human-readable
// description of the problem.
type Warning struct {
	// Entity is the type of the broken entity (e.g. "route", "destination").
	Entity string `json:"entity"`

	// ID is the unique identifier of the broken entity.
	ID string `json:"id"`

	// Name is the human-readable name of the broken entity.
	Name string `json:"name"`

	// Message describes what is wrong.
	Message string `json:"message"`
}

// Validator is a function that inspects a snapshot and returns warnings.
// It must be pure (no I/O, no side effects).
type Validator func(snap *model.Snapshot) []Warning

// Snapshot runs all registered validators against the snapshot and returns
// the combined list of warnings. An empty slice means the snapshot is
// structurally sound and will compile cleanly on any proxy.
func Snapshot(snap *model.Snapshot) []Warning {
	var warnings []Warning
	for _, v := range validators() {
		warnings = append(warnings, v(snap)...)
	}
	return warnings
}

// validators returns the ordered list of validation functions. New validators
// are appended here — no other wiring is needed.
func validators() []Validator {
	return []Validator{
		validateRouteRegexes,
		validateRouteCEL,
		validateRouteRewriteRegex,
		validateHeaderRegexes,
		validateQueryParamRegexes,
		validateGroupRegexes,
		validateListenerTLS,
		validateDestinationTLS,
		validateRouteDestinationRefs,
		validateRouteMirrorRefs,
		validateGroupRouteRefs,
		validateMiddlewareDestinationRefs,
		validateRouteMiddlewareRefs,
		validateGroupMiddlewareRefs,
		validateMiddlewareOverrideCEL,
		validateCORSOriginRegexes,
		validateJWTAssertClaimsCEL,
		validateJWTClaimToHeadersCEL,
	}
}

// ── Regex compilation ───────────────────────────────────────────────────────

func validateRouteRegexes(snap *model.Snapshot) []Warning {
	var w []Warning
	for _, r := range snap.Routes {
		if r.Match.PathRegex != "" {
			if _, err := regexp.Compile(r.Match.PathRegex); err != nil {
				w = append(w, Warning{"route", r.ID, r.Name, fmt.Sprintf("match.pathRegex does not compile: %v", err)})
			}
		}
	}
	return w
}

func validateHeaderRegexes(snap *model.Snapshot) []Warning {
	var w []Warning
	for _, r := range snap.Routes {
		for _, hm := range r.Match.Headers {
			if hm.Regex && hm.Value != "" {
				if _, err := regexp.Compile(hm.Value); err != nil {
					w = append(w, Warning{"route", r.ID, r.Name, fmt.Sprintf("match.headers[%s] regex does not compile: %v", hm.Name, err)})
				}
			}
		}
	}
	return w
}

func validateQueryParamRegexes(snap *model.Snapshot) []Warning {
	var w []Warning
	for _, r := range snap.Routes {
		for _, qp := range r.Match.QueryParams {
			if qp.Regex && qp.Value != "" {
				if _, err := regexp.Compile(qp.Value); err != nil {
					w = append(w, Warning{"route", r.ID, r.Name, fmt.Sprintf("match.queryParams[%s] regex does not compile: %v", qp.Name, err)})
				}
			}
		}
	}
	return w
}

func validateGroupRegexes(snap *model.Snapshot) []Warning {
	var w []Warning
	for _, g := range snap.Groups {
		if g.PathRegex != "" {
			if _, err := regexp.Compile(g.PathRegex); err != nil {
				w = append(w, Warning{"group", g.ID, g.Name, fmt.Sprintf("pathRegex does not compile: %v", err)})
			}
		}
		for _, hm := range g.Headers {
			if hm.Regex && hm.Value != "" {
				if _, err := regexp.Compile(hm.Value); err != nil {
					w = append(w, Warning{"group", g.ID, g.Name, fmt.Sprintf("headers[%s] regex does not compile: %v", hm.Name, err)})
				}
			}
		}
	}
	return w
}

func validateRouteRewriteRegex(snap *model.Snapshot) []Warning {
	var w []Warning
	for _, r := range snap.Routes {
		if r.Forward != nil && r.Forward.Rewrite != nil && r.Forward.Rewrite.PathRegex != nil {
			if _, err := regexp.Compile(r.Forward.Rewrite.PathRegex.Pattern); err != nil {
				w = append(w, Warning{"route", r.ID, r.Name, fmt.Sprintf("forward.rewrite.pathRegex.pattern does not compile: %v", err)})
			}
		}
	}
	return w
}

func validateCORSOriginRegexes(snap *model.Snapshot) []Warning {
	var w []Warning
	for _, mw := range snap.Middlewares {
		if mw.Type != model.MiddlewareTypeCORS || mw.CORS == nil {
			continue
		}
		for _, o := range mw.CORS.AllowOrigins {
			if o.Regex && o.Value != "" {
				if _, err := regexp.Compile(o.Value); err != nil {
					w = append(w, Warning{"middleware", mw.ID, mw.Name, fmt.Sprintf("cors.allowOrigins regex %q does not compile: %v", o.Value, err)})
				}
			}
		}
	}
	return w
}

// ── CEL compilation ─────────────────────────────────────────────────────────

func validateRouteCEL(snap *model.Snapshot) []Warning {
	var w []Warning
	for _, r := range snap.Routes {
		if r.Match.CEL != "" {
			if _, err := celeval.Compile(r.Match.CEL); err != nil {
				w = append(w, Warning{"route", r.ID, r.Name, fmt.Sprintf("match.cel does not compile: %v", err)})
			}
		}
	}
	return w
}

func validateMiddlewareOverrideCEL(snap *model.Snapshot) []Warning {
	var w []Warning
	checkOverrides := func(entity, id, name string, overrides map[string]model.MiddlewareOverride) {
		for mwID, ov := range overrides {
			for i, expr := range ov.SkipWhen {
				if _, err := celeval.Compile(expr); err != nil {
					w = append(w, Warning{entity, id, name, fmt.Sprintf("middlewareOverrides[%s].skipWhen[%d] does not compile: %v", mwID, i, err)})
				}
			}
			for i, expr := range ov.OnlyWhen {
				if _, err := celeval.Compile(expr); err != nil {
					w = append(w, Warning{entity, id, name, fmt.Sprintf("middlewareOverrides[%s].onlyWhen[%d] does not compile: %v", mwID, i, err)})
				}
			}
		}
	}
	for _, r := range snap.Routes {
		checkOverrides("route", r.ID, r.Name, r.MiddlewareOverrides)
	}
	for _, g := range snap.Groups {
		checkOverrides("group", g.ID, g.Name, g.MiddlewareOverrides)
	}
	return w
}

func validateJWTAssertClaimsCEL(snap *model.Snapshot) []Warning {
	var w []Warning
	for _, mw := range snap.Middlewares {
		if mw.Type != model.MiddlewareTypeJWT || mw.JWT == nil {
			continue
		}
		for i, expr := range mw.JWT.AssertClaims {
			if _, err := celeval.CompileClaims(expr); err != nil {
				w = append(w, Warning{"middleware", mw.ID, mw.Name, fmt.Sprintf("jwt.assertClaims[%d] does not compile: %v", i, err)})
			}
		}
	}
	return w
}

func validateJWTClaimToHeadersCEL(snap *model.Snapshot) []Warning {
	var w []Warning
	for _, mw := range snap.Middlewares {
		if mw.Type != model.MiddlewareTypeJWT || mw.JWT == nil {
			continue
		}
		for i, ch := range mw.JWT.ClaimToHeaders {
			if ch.Expr != "" {
				if _, err := celeval.CompileClaims(ch.Expr); err != nil {
					w = append(w, Warning{"middleware", mw.ID, mw.Name, fmt.Sprintf("jwt.claimToHeaders[%d].expr does not compile: %v", i, err)})
				}
			}
		}
	}
	return w
}

// ── TLS certificate parsing ─────────────────────────────────────────────────

func validateListenerTLS(snap *model.Snapshot) []Warning {
	var w []Warning
	for _, l := range snap.Listeners {
		if l.TLS == nil || l.TLS.Cert == "" || l.TLS.Key == "" {
			continue
		}
		if _, err := tls.X509KeyPair([]byte(l.TLS.Cert), []byte(l.TLS.Key)); err != nil {
			w = append(w, Warning{"listener", l.ID, l.Name, fmt.Sprintf("tls cert/key pair is invalid: %v", err)})
		}
		if l.TLS.ClientAuth != nil && l.TLS.ClientAuth.CA != "" {
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM([]byte(l.TLS.ClientAuth.CA)) {
				w = append(w, Warning{"listener", l.ID, l.Name, "tls.clientAuth.ca contains no valid PEM certificates"})
			}
		}
	}
	return w
}

func validateDestinationTLS(snap *model.Snapshot) []Warning {
	var w []Warning
	for _, d := range snap.Destinations {
		if d.Options == nil || d.Options.TLS == nil {
			continue
		}
		t := d.Options.TLS
		if t.Mode == model.TLSModeNone || t.Mode == "" {
			continue
		}
		if t.Mode == model.TLSModeMTLS && t.Cert != "" && t.Key != "" {
			if _, err := tls.X509KeyPair([]byte(t.Cert), []byte(t.Key)); err != nil {
				w = append(w, Warning{"destination", d.ID, d.Name, fmt.Sprintf("options.tls client cert/key pair is invalid: %v", err)})
			}
		}
		if t.CA != "" {
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM([]byte(t.CA)) {
				w = append(w, Warning{"destination", d.ID, d.Name, "options.tls.ca contains no valid PEM certificates"})
			}
		}
	}
	return w
}

// ── Referential integrity ───────────────────────────────────────────────────

func validateRouteDestinationRefs(snap *model.Snapshot) []Warning {
	destIDs := make(map[string]bool, len(snap.Destinations))
	for _, d := range snap.Destinations {
		destIDs[d.ID] = true
	}

	var w []Warning
	for _, r := range snap.Routes {
		if r.Forward == nil {
			continue
		}
		for _, ref := range r.Forward.Destinations {
			if !destIDs[ref.DestinationID] {
				w = append(w, Warning{"route", r.ID, r.Name, fmt.Sprintf("forward.destinations references unknown destination %q", ref.DestinationID)})
			}
		}
	}
	return w
}

func validateRouteMirrorRefs(snap *model.Snapshot) []Warning {
	destIDs := make(map[string]bool, len(snap.Destinations))
	for _, d := range snap.Destinations {
		destIDs[d.ID] = true
	}

	var w []Warning
	for _, r := range snap.Routes {
		if r.Forward == nil || r.Forward.Mirror == nil {
			continue
		}
		if !destIDs[r.Forward.Mirror.DestinationID] {
			w = append(w, Warning{"route", r.ID, r.Name, fmt.Sprintf("forward.mirror references unknown destination %q", r.Forward.Mirror.DestinationID)})
		}
	}
	return w
}

func validateGroupRouteRefs(snap *model.Snapshot) []Warning {
	routeIDs := make(map[string]bool, len(snap.Routes))
	for _, r := range snap.Routes {
		routeIDs[r.ID] = true
	}

	var w []Warning
	for _, g := range snap.Groups {
		for _, rid := range g.RouteIDs {
			if !routeIDs[rid] {
				w = append(w, Warning{"group", g.ID, g.Name, fmt.Sprintf("routeIds references unknown route %q", rid)})
			}
		}
	}
	return w
}

func validateMiddlewareDestinationRefs(snap *model.Snapshot) []Warning {
	destIDs := make(map[string]bool, len(snap.Destinations))
	for _, d := range snap.Destinations {
		destIDs[d.ID] = true
	}

	var w []Warning
	for _, mw := range snap.Middlewares {
		switch {
		case mw.Type == model.MiddlewareTypeExtAuthz && mw.ExtAuthz != nil:
			if mw.ExtAuthz.DestinationID != "" && !destIDs[mw.ExtAuthz.DestinationID] {
				w = append(w, Warning{"middleware", mw.ID, mw.Name, fmt.Sprintf("extAuthz.destinationId references unknown destination %q", mw.ExtAuthz.DestinationID)})
			}
		case mw.Type == model.MiddlewareTypeExtProc && mw.ExtProc != nil:
			if mw.ExtProc.DestinationID != "" && !destIDs[mw.ExtProc.DestinationID] {
				w = append(w, Warning{"middleware", mw.ID, mw.Name, fmt.Sprintf("extProc.destinationId references unknown destination %q", mw.ExtProc.DestinationID)})
			}
		case mw.Type == model.MiddlewareTypeJWT && mw.JWT != nil:
			if mw.JWT.JWKsDestinationID != "" && !destIDs[mw.JWT.JWKsDestinationID] {
				w = append(w, Warning{"middleware", mw.ID, mw.Name, fmt.Sprintf("jwt.jwksDestinationId references unknown destination %q", mw.JWT.JWKsDestinationID)})
			}
		}
	}
	return w
}

func validateRouteMiddlewareRefs(snap *model.Snapshot) []Warning {
	mwIDs := make(map[string]bool, len(snap.Middlewares))
	for _, mw := range snap.Middlewares {
		mwIDs[mw.ID] = true
	}

	var w []Warning
	for _, r := range snap.Routes {
		for _, mid := range r.MiddlewareIDs {
			if !mwIDs[mid] {
				w = append(w, Warning{"route", r.ID, r.Name, fmt.Sprintf("middlewareIds references unknown middleware %q", mid)})
			}
		}
	}
	return w
}

func validateGroupMiddlewareRefs(snap *model.Snapshot) []Warning {
	mwIDs := make(map[string]bool, len(snap.Middlewares))
	for _, mw := range snap.Middlewares {
		mwIDs[mw.ID] = true
	}

	var w []Warning
	for _, g := range snap.Groups {
		for _, mid := range g.MiddlewareIDs {
			if !mwIDs[mid] {
				w = append(w, Warning{"group", g.ID, g.Name, fmt.Sprintf("middlewareIds references unknown middleware %q", mid)})
			}
		}
	}
	return w
}
