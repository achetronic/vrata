// Package celeval provides CEL expression compilation and evaluation for
// request matching. Expressions are compiled once at routing table build
// time and evaluated per-request against a flat variable map derived from
// the HTTP request.
package celeval

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/google/cel-go/cel"
)

// Program is a pre-compiled CEL program ready for evaluation against an
// HTTP request. It is safe for concurrent use.
type Program struct {
	program cel.Program
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

	return &Program{program: prg}, nil
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

	return map[string]any{
		"method":      r.Method,
		"path":        r.URL.Path,
		"host":        host,
		"scheme":      scheme,
		"headers":     headers,
		"queryParams": queryParams,
		"clientIp":    clientIP,
	}
}
