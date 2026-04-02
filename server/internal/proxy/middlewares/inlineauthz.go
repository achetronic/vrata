// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package middlewares

import (
	"log/slog"
	"net/http"

	"github.com/achetronic/vrata/internal/model"
	"github.com/achetronic/vrata/internal/proxy/celeval"
)

// compiledAuthzRule is a pre-compiled inline authorization rule.
type compiledAuthzRule struct {
	program   *celeval.Program
	action    string // "allow" or "deny"
	needsBody bool
}

// InlineAuthzMiddleware creates a middleware that evaluates CEL-based
// authorization rules against the request. First matching rule wins.
// If no rule matches, the defaultAction is applied.
func InlineAuthzMiddleware(cfg *model.InlineAuthzConfig, celBodyMaxSize int) Middleware {
	if cfg == nil {
		return passthrough
	}

	var rules []compiledAuthzRule
	needsBody := false

	for i, r := range cfg.Rules {
		prg, err := celeval.Compile(r.CEL)
		if err != nil {
			slog.Error("inlineAuthz: skipping rule with compile error",
				slog.Int("rule", i),
				slog.String("cel", r.CEL),
				slog.String("error", err.Error()),
			)
			continue
		}
		cr := compiledAuthzRule{
			program:   prg,
			action:    r.Action,
			needsBody: prg.NeedsBody(),
		}
		if cr.needsBody {
			needsBody = true
		}
		rules = append(rules, cr)
	}

	defaultAction := cfg.DefaultAction
	if defaultAction == "" {
		defaultAction = "deny"
	}

	denyStatus := int(cfg.DenyStatus)
	if denyStatus == 0 {
		denyStatus = http.StatusForbidden
	}

	denyBody := cfg.DenyBody
	if denyBody == "" {
		denyBody = `{"error":"access denied"}`
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Buffer body once if any rule needs it.
			if needsBody {
				// Fail-open: body buffering errors are logged by BufferBody; CEL
				// evaluates with an empty body rather than rejecting the request.
				r, _ = celeval.BufferBody(r, celBodyMaxSize)
			}

			for _, rule := range rules {
				if rule.program.Eval(r) {
					if rule.action == "allow" {
						next.ServeHTTP(w, r)
						return
					}
					// deny
					writeDenyResponse(w, denyStatus, denyBody)
					return
				}
			}

			// No rule matched — apply default.
			if defaultAction == "allow" {
				next.ServeHTTP(w, r)
				return
			}
			writeDenyResponse(w, denyStatus, denyBody)
		})
	}
}

// writeDenyResponse writes the deny body directly as JSON.
func writeDenyResponse(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write([]byte(body)); err != nil {
		slog.Warn("inlineAuthz: failed to write deny body", slog.String("error", err.Error()))
	}
}

// InlineAuthzNeedsBody reports whether any compiled rule references
// request.body. Used by table build to set the needsBody flag.
func InlineAuthzNeedsBody(cfg *model.InlineAuthzConfig) bool {
	if cfg == nil {
		return false
	}
	for _, r := range cfg.Rules {
		prg, err := celeval.Compile(r.CEL)
		if err != nil {
			continue
		}
		if prg.NeedsBody() {
			return true
		}
	}
	return false
}
