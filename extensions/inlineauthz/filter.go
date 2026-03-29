// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package main is the entrypoint for the inlineauthz Envoy Go filter plugin.
// Compiled as a shared object (.so) and loaded by Envoy at startup.
//
// Build:
//
//	go build -buildmode=plugin -o inlineauthz.so .
//
// Configuration (via environment variables):
//
//	VRATA_AUTHZ_RULES_JSON   JSON-encoded array of rules (see Rule type)
//	VRATA_AUTHZ_DEFAULT      Default action: "allow" or "deny" (default: "deny")
//	VRATA_AUTHZ_DENY_STATUS  HTTP status for deny (default: 403)
//	VRATA_AUTHZ_DENY_BODY    Response body for deny (default: {"error":"access denied"})
//	VRATA_AUTHZ_MAX_BODY     Max bytes to buffer for body access (default: 65536)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/envoyproxy/envoy/contrib/golang/common/go/api"
	"github.com/google/cel-go/cel"
)

// Rule is a single authorization rule with a CEL expression and an action.
type Rule struct {
	// CEL is the expression to evaluate. Has access to request.* map.
	CEL string `json:"cel"`
	// Action is "allow" or "deny".
	Action string `json:"action"`
}

// filterConfig holds the parsed configuration.
type filterConfig struct {
	rules         []compiledRule
	defaultAction string
	denyStatus    int
	denyBody      []byte
	maxBodyBytes  int64
	needsBody     bool // true if any rule references request.body
}

type compiledRule struct {
	program cel.Program
	action  string
}

var globalConfig *filterConfig

func init() {
	cfg, err := loadConfig()
	if err != nil {
		slog.Error("inlineauthz: failed to load config", slog.String("error", err.Error()))
		// Fall back to deny-all.
		globalConfig = &filterConfig{
			defaultAction: "deny",
			denyStatus:    403,
			denyBody:      []byte(`{"error":"access denied"}`),
			maxBodyBytes:  65536,
		}
		return
	}
	globalConfig = cfg
}

func loadConfig() (*filterConfig, error) {
	cfg := &filterConfig{
		defaultAction: envOr("VRATA_AUTHZ_DEFAULT", "deny"),
		denyStatus:    403,
		denyBody:      []byte(envOr("VRATA_AUTHZ_DENY_BODY", `{"error":"access denied"}`)),
		maxBodyBytes:  65536,
	}

	if s, err := strconv.Atoi(envOr("VRATA_AUTHZ_DENY_STATUS", "403")); err == nil {
		cfg.denyStatus = s
	}
	if s, err := strconv.ParseInt(envOr("VRATA_AUTHZ_MAX_BODY", "65536"), 10, 64); err == nil {
		cfg.maxBodyBytes = s
	}

	rulesJSON := os.Getenv("VRATA_AUTHZ_RULES_JSON")
	if rulesJSON == "" {
		return cfg, nil
	}

	var rules []Rule
	if err := json.Unmarshal([]byte(rulesJSON), &rules); err != nil {
		return nil, fmt.Errorf("parsing rules JSON: %w", err)
	}

	env, err := buildCELEnv()
	if err != nil {
		return nil, fmt.Errorf("building CEL environment: %w", err)
	}

	for _, r := range rules {
		ast, iss := env.Compile(r.CEL)
		if iss != nil && iss.Err() != nil {
			return nil, fmt.Errorf("compiling rule %q: %w", r.CEL, iss.Err())
		}
		prog, err := env.Program(ast)
		if err != nil {
			return nil, fmt.Errorf("building program for %q: %w", r.CEL, err)
		}
		cfg.rules = append(cfg.rules, compiledRule{program: prog, action: r.Action})

		// Detect if this rule needs body access.
		if strings.Contains(r.CEL, "request.body") {
			cfg.needsBody = true
		}
	}

	return cfg, nil
}

// filter is the per-request filter instance.
type filter struct {
	api.PassThroughHttpFilter
	callbacks api.FilterCallbackHandler
	config    *filterConfig
	headers   http.Header
	body      []byte
}

// DecodeHeaders is called on request headers.
func (f *filter) DecodeHeaders(header api.RequestHeaderMap, endStream bool) api.StatusType {
	// Snapshot headers for CEL evaluation.
	f.headers = make(http.Header)
	header.Range(func(k, v string) bool {
		f.headers.Add(k, v)
		return true
	})

	// If no rules need body, evaluate immediately.
	if !f.config.needsBody || endStream {
		return f.evaluate("")
	}

	// Buffer body before evaluating.
	return api.StopAndBuffer
}

// DecodeData is called for request body chunks when buffering is needed.
func (f *filter) DecodeData(buffer api.BufferInstance, endStream bool) api.StatusType {
	if !endStream {
		return api.StopAndBuffer
	}

	body := buffer.Bytes()
	if int64(len(body)) > f.config.maxBodyBytes {
		body = body[:f.config.maxBodyBytes]
	}
	f.body = body

	contentType := f.headers.Get("content-type")
	return f.evaluate(contentType)
}

// evaluate runs the CEL rules against the current request state.
func (f *filter) evaluate(contentType string) api.StatusType {
	activation := buildActivation(f.headers, f.body, contentType)

	for _, rule := range f.config.rules {
		out, _, err := rule.program.Eval(activation)
		if err != nil {
			continue // fault isolation: bad CEL doesn't block the request
		}
		if b, ok := out.Value().(bool); ok && b {
			if rule.action == "deny" {
				return f.deny()
			}
			return api.Continue
		}
	}

	if f.config.defaultAction == "deny" {
		return f.deny()
	}
	return api.Continue
}

func (f *filter) deny() api.StatusType {
	f.callbacks.SendLocalReply(f.config.denyStatus, string(f.config.denyBody), nil, 0, "")
	return api.LocalReply
}

// ─────────────────────────────────────────────────────────────────────────────
// CEL environment and activation
// ─────────────────────────────────────────────────────────────────────────────

func buildCELEnv() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("request", cel.MapType(cel.StringType, cel.DynType)),
	)
}

func buildActivation(headers http.Header, body []byte, contentType string) map[string]any {
	reqMap := map[string]any{
		"method": headers.Get(":method"),
		"path":   headers.Get(":path"),
	}

	headersMap := map[string]any{}
	for k, vs := range headers {
		headersMap[strings.ToLower(k)] = strings.Join(vs, ", ")
	}
	reqMap["headers"] = headersMap

	bodyMap := map[string]any{
		"raw": string(body),
	}
	if strings.Contains(contentType, "application/json") && len(body) > 0 {
		var parsed any
		if err := json.Unmarshal(body, &parsed); err == nil {
			bodyMap["json"] = parsed
		}
	}
	reqMap["body"] = bodyMap

	return map[string]any{"request": reqMap}
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// unused import guard for context
var _ = context.Background

// main is required for plugin build mode but does nothing.
func main() {}

func newFilter(callbacks api.FilterCallbackHandler) api.HttpFilter {
	return &filter{
		callbacks: callbacks,
		config:    globalConfig,
	}
}

func init() {
	api.RegisterHttpFilterFactoryAndConfigParser(
		"vrata.inlineauthz",
		func(callbacks api.FilterCallbackHandler) api.HttpFilter {
			return newFilter(callbacks)
		},
		&api.EmptyConfig{},
	)
}
