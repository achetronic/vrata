package middlewares

import (
	"net/http"
	"strings"
)

// Interpolate replaces ${request.*} placeholders in a string with values
// from the HTTP request.
//
// Supported placeholders:
//   - ${request.host}        — the Host header (without port)
//   - ${request.path}        — the URL path
//   - ${request.method}      — the HTTP method
//   - ${request.scheme}      — "https" if TLS, "http" otherwise
//   - ${request.authority}   — the full Host header (with port if present)
//   - ${request.header.NAME} — value of the named request header
func Interpolate(template string, r *http.Request) string {
	if !strings.Contains(template, "${") {
		return template
	}

	result := template

	result = strings.ReplaceAll(result, "${request.host}", requestHost(r))
	result = strings.ReplaceAll(result, "${request.path}", r.URL.Path)
	result = strings.ReplaceAll(result, "${request.method}", r.Method)
	result = strings.ReplaceAll(result, "${request.authority}", r.Host)

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	result = strings.ReplaceAll(result, "${request.scheme}", scheme)

	// Handle ${request.header.NAME} patterns. Track position to avoid
	// re-matching replaced content (prevents infinite loop with adversarial
	// header values containing "${request.header.").
	pos := 0
	for {
		idx := strings.Index(result[pos:], "${request.header.")
		if idx == -1 {
			break
		}
		start := pos + idx
		end := strings.Index(result[start:], "}")
		if end == -1 {
			break
		}
		end += start
		headerName := result[start+len("${request.header.") : end]
		headerValue := r.Header.Get(headerName)
		result = result[:start] + headerValue + result[end+1:]
		pos = start + len(headerValue)
	}

	return result
}

func requestHost(r *http.Request) string {
	host := r.Host
	if idx := strings.Index(host, ":"); idx != -1 {
		return host[:idx]
	}
	return host
}
