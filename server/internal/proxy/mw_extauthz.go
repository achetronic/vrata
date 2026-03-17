package proxy

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/achetronic/rutoso/internal/model"
)

// ExtAuthzMiddleware creates a middleware that sends a check request to an
// external authorization service before forwarding to the upstream.
//
// The middleware is designed to work with any HTTP authz service (oauth2-proxy,
// OPA, custom). It gives the user full control over:
//   - Which client headers are sent to the authz service
//   - Which extra headers are injected into the check request
//   - Which authz response headers are passed to the upstream on allow
//   - Which authz response headers are returned to the client on deny
func ExtAuthzMiddleware(cfg *model.ExtAuthzConfig, upstreams map[string]*Upstream) Middleware {
	if cfg == nil || cfg.DestinationID == "" {
		return func(next http.Handler) http.Handler { return next }
	}

	upstream, ok := upstreams[cfg.DestinationID]
	if !ok {
		return func(next http.Handler) http.Handler { return next }
	}

	timeout := 5 * time.Second
	if cfg.Timeout != "" {
		if d, err := time.ParseDuration(cfg.Timeout); err == nil {
			timeout = d
		}
	}

	d := upstream.Destination
	scheme := "http"
	if d.Options != nil && d.Options.TLS != nil &&
		d.Options.TLS.Mode != model.TLSModeNone && d.Options.TLS.Mode != "" {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s:%d", scheme, d.Host, d.Port)
	if cfg.Path != "" {
		baseURL += cfg.Path
	}

	// Build the set of allowed headers to forward (lowercase for comparison).
	allowedHeaders := make(map[string]bool)
	// Always include these.
	for _, h := range []string{"host", "method", "authorization", "cookie", "content-length"} {
		allowedHeaders[h] = true
	}
	for _, h := range cfg.AllowedHeaders {
		allowedHeaders[strings.ToLower(h)] = true
	}

	// Allowed upstream headers (from authz response -> upstream request).
	allowedUpstream := make(map[string]bool)
	for _, h := range cfg.AllowedUpstreamHeaders {
		allowedUpstream[strings.ToLower(h)] = true
	}

	// Allowed client headers (from authz response -> client on deny).
	allowedClient := make(map[string]bool)
	// Always include these on deny.
	for _, h := range []string{"location", "set-cookie", "www-authenticate", "content-type"} {
		allowedClient[h] = true
	}
	for _, h := range cfg.AllowedClientHeaders {
		allowedClient[strings.ToLower(h)] = true
	}

	client := &http.Client{
		Transport: upstream.Transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Don't follow redirects — return them to the caller.
			return http.ErrUseLastResponse
		},
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Build the check request.
			checkReq, err := http.NewRequestWithContext(r.Context(), r.Method, baseURL, nil)
			if err != nil {
				if cfg.FailureModeAllow {
					next.ServeHTTP(w, r)
					return
				}
				http.Error(w, "ext_authz: failed to create check request", http.StatusForbidden)
				return
			}

			// Copy allowed headers from client request.
			for key, values := range r.Header {
				if allowedHeaders[strings.ToLower(key)] {
					for _, v := range values {
						checkReq.Header.Add(key, v)
					}
				}
			}

			// Inject extra headers.
			for _, h := range cfg.HeadersToAdd {
				checkReq.Header.Set(h.Key, h.Value)
			}

			// Send check request.
			resp, err := client.Do(checkReq)
			if err != nil {
				if cfg.FailureModeAllow {
					next.ServeHTTP(w, r)
					return
				}
				http.Error(w, "ext_authz: authz service unreachable", http.StatusForbidden)
				return
			}
			defer resp.Body.Close()

			// Auth allowed (2xx).
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				// Copy allowed headers from authz response to upstream request.
				for key, values := range resp.Header {
					if allowedUpstream[strings.ToLower(key)] {
						for _, v := range values {
							r.Header.Set(key, v)
						}
					}
				}
				next.ServeHTTP(w, r)
				return
			}

			// Auth denied — return authz response to client.
			for key, values := range resp.Header {
				if allowedClient[strings.ToLower(key)] {
					for _, v := range values {
						w.Header().Add(key, v)
					}
				}
			}
			w.WriteHeader(resp.StatusCode)
			io.Copy(w, resp.Body)
		})
	}
}
