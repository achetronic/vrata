// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package celeval provides CEL expression compilation and evaluation for
// request matching. Expressions are compiled once at routing table build
// time and evaluated per-request against a flat variable map derived from
// the HTTP request.
package celeval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/google/cel-go/cel"
)

// Program is a pre-compiled CEL program ready for evaluation against an
// HTTP request. It is safe for concurrent use.
type Program struct {
	program   cel.Program
	needsBody bool
}

// NeedsBody reports whether this program references request.body and
// therefore requires the request body to be buffered before evaluation.
func (p *Program) NeedsBody() bool {
	return p.needsBody
}

// Compile parses and type-checks a CEL expression, returning a Program
// that can be evaluated against requests. The expression must return a bool.
func Compile(expr string) (*Program, error) {
	env, err := cel.NewEnv(
		cel.Variable("request", cel.MapType(cel.StringType, cel.DynType)),
	)
	if err != nil {
		return nil, fmt.Errorf("creating CEL env: %w", err)
	}

	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compiling CEL expression: %w", issues.Err())
	}

	if ast.OutputType() != cel.BoolType {
		return nil, fmt.Errorf("CEL expression must return bool, got %s", ast.OutputType())
	}

	prg, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("creating CEL program: %w", err)
	}

	return &Program{
		program:   prg,
		needsBody: exprReferencesBody(expr),
	}, nil
}

// exprReferencesBody checks whether a CEL expression references request.body.
// This is a simple string check — false positives (e.g. the literal in a
// string) are harmless (extra buffering), false negatives are not possible
// because the field name is fixed.
func exprReferencesBody(expr string) bool {
	return strings.Contains(expr, "request.body")
}

// Eval evaluates the compiled CEL program against the given HTTP request.
// Returns true if the expression evaluates to true, false otherwise.
// Evaluation errors are treated as non-match (returns false).
func (p *Program) Eval(r *http.Request) bool {
	vars := map[string]any{
		"request": buildRequestMap(r),
	}

	out, _, err := p.program.Eval(vars)
	if err != nil {
		return false
	}

	result, ok := out.Value().(bool)
	return ok && result
}

// ClaimsProgram is a pre-compiled CEL program for evaluating JWT claims.
type ClaimsProgram struct {
	program cel.Program
}

// CompileClaims parses a CEL expression that receives a `claims` map
// (the decoded JWT payload). The expression must return a bool.
func CompileClaims(expr string) (*ClaimsProgram, error) {
	env, err := cel.NewEnv(
		cel.Variable("claims", cel.MapType(cel.StringType, cel.DynType)),
	)
	if err != nil {
		return nil, fmt.Errorf("creating CEL env: %w", err)
	}

	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compiling CEL expression: %w", issues.Err())
	}

	if ast.OutputType() != cel.BoolType {
		return nil, fmt.Errorf("CEL expression must return bool, got %s", ast.OutputType())
	}

	prg, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("creating CEL program: %w", err)
	}

	return &ClaimsProgram{program: prg}, nil
}

// Eval evaluates the claims program against the given JWT claims map.
func (p *ClaimsProgram) Eval(claims map[string]any) bool {
	vars := map[string]any{
		"claims": claims,
	}

	out, _, err := p.program.Eval(vars)
	if err != nil {
		return false
	}

	result, ok := out.Value().(bool)
	return ok && result
}

// ClaimsStringProgram is a pre-compiled CEL program that extracts a string
// value from JWT claims. Used by claimToHeaders.
type ClaimsStringProgram struct {
	program cel.Program
}

// CompileClaimsString parses a CEL expression that receives a `claims` map
// and returns any value (converted to string at eval time).
func CompileClaimsString(expr string) (*ClaimsStringProgram, error) {
	env, err := cel.NewEnv(
		cel.Variable("claims", cel.MapType(cel.StringType, cel.DynType)),
	)
	if err != nil {
		return nil, fmt.Errorf("creating CEL env: %w", err)
	}

	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compiling CEL expression: %w", issues.Err())
	}

	prg, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("creating CEL program: %w", err)
	}

	return &ClaimsStringProgram{program: prg}, nil
}

// Eval evaluates the expression against the claims and returns the result as a string.
// Returns empty string on error or nil result.
func (p *ClaimsStringProgram) Eval(claims map[string]any) string {
	vars := map[string]any{
		"claims": claims,
	}

	out, _, err := p.program.Eval(vars)
	if err != nil {
		return ""
	}

	return fmt.Sprintf("%v", out.Value())
}

// bodyCtxKey is the context key for cached body data.
type bodyCtxKey struct{}

// BodyData holds the buffered request body in both raw and parsed forms.
type BodyData struct {
	Raw  string
	JSON map[string]any // nil if not JSON or parse failed
}

// BufferBody reads the request body up to maxSize bytes, replaces r.Body
// with a re-readable buffer, and stores the result in the request context.
// Subsequent calls return the cached data without re-reading.
func BufferBody(r *http.Request, maxSize int) (*http.Request, *BodyData) {
	if data, ok := r.Context().Value(bodyCtxKey{}).(*BodyData); ok {
		return r, data
	}

	data := &BodyData{}

	if r.Body != nil && r.Body != http.NoBody {
		reader := io.LimitReader(r.Body, int64(maxSize+1))
		raw, err := io.ReadAll(reader)
		if err != nil {
			slog.Warn("failed to read request body for CEL evaluation", "error", err)
		} else {
			if len(raw) > maxSize {
				slog.Warn("request body exceeds celBodyMaxSize, truncating raw and skipping json parse",
					"size", len(raw), "maxSize", maxSize)
				raw = raw[:maxSize]
			} else {
				ct := r.Header.Get("Content-Type")
				if strings.HasPrefix(ct, "application/json") {
					var parsed map[string]any
					dec := json.NewDecoder(bytes.NewReader(raw))
					dec.UseNumber()
					if err := dec.Decode(&parsed); err != nil {
						slog.Debug("request body is not valid JSON", "error", err)
					} else {
						data.JSON = parsed
					}
				}
			}
			data.Raw = string(raw)

			r.Body = io.NopCloser(bytes.NewReader(raw))
		}
	}

	ctx := context.WithValue(r.Context(), bodyCtxKey{}, data)
	return r.WithContext(ctx), data
}

// BodyFromCtx returns cached body data from a request context, or nil
// if BufferBody has not been called.
func BodyFromCtx(r *http.Request) *BodyData {
	data, _ := r.Context().Value(bodyCtxKey{}).(*BodyData)
	return data
}

// buildRequestMap creates a map representation of the HTTP request for CEL.
func buildRequestMap(r *http.Request) map[string]any {
	headers := make(map[string]any, len(r.Header))
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[strings.ToLower(k)] = v[0]
		}
	}

	queryParams := make(map[string]any, len(r.URL.Query()))
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			queryParams[k] = v[0]
		}
	}

	host := r.Host
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}

	clientIP := r.RemoteAddr
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			clientIP = strings.TrimSpace(xff[:idx])
		} else {
			clientIP = strings.TrimSpace(xff)
		}
	} else if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		clientIP = h
	}

	m := map[string]any{
		"method":      r.Method,
		"path":        r.URL.Path,
		"host":        host,
		"scheme":      scheme,
		"headers":     headers,
		"queryParams": queryParams,
		"clientIp":    clientIP,
	}

	if data := BodyFromCtx(r); data != nil {
		body := map[string]any{
			"raw": data.Raw,
		}
		if data.JSON != nil {
			body["json"] = data.JSON
		}
		m["body"] = body
	}

	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		cert := r.TLS.PeerCertificates[0]
		uris := make([]string, 0, len(cert.URIs))
		for _, u := range cert.URIs {
			uris = append(uris, u.String())
		}
		dnsNames := make([]string, len(cert.DNSNames))
		copy(dnsNames, cert.DNSNames)

		m["tls"] = map[string]any{
			"peerCertificate": map[string]any{
				"uris":     uris,
				"dnsNames": dnsNames,
				"subject":  cert.Subject.String(),
				"serial":   fmt.Sprintf("%x", cert.SerialNumber),
			},
		}
	}

	return m
}
