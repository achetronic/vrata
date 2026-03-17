package middlewares

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/achetronic/rutoso/internal/model"
)

// ExtAuthzMiddleware creates a middleware that sends a check request to an
// external authorization service before forwarding to the upstream.
func ExtAuthzMiddleware(cfg *model.ExtAuthzConfig, services map[string]Service) Middleware {
	if cfg == nil || cfg.DestinationID == "" {
		return func(next http.Handler) http.Handler { return next }
	}

	svc, ok := services[cfg.DestinationID]
	if !ok {
		return func(next http.Handler) http.Handler { return next }
	}

	timeout := 5 * time.Second
	if cfg.Timeout != "" {
		if d, err := time.ParseDuration(cfg.Timeout); err == nil {
			timeout = d
		}
	}

	baseURL := svc.BaseURL
	if cfg.Path != "" {
		baseURL += cfg.Path
	}

	// Headers to forward from client to authz (always include these).
	forwardHeaders := map[string]bool{
		"host":           true,
		"content-length": true,
	}
	if cfg.OnCheck != nil {
		for _, h := range cfg.OnCheck.ForwardHeaders {
			forwardHeaders[strings.ToLower(h)] = true
		}
	}

	// Patterns for onAllow (authz response -> upstream).
	var allowPatterns []string
	if cfg.OnAllow != nil {
		allowPatterns = cfg.OnAllow.CopyToUpstream
	}

	// Patterns for onDeny (authz response -> client).
	// Always include these.
	denyPatterns := []string{"location", "set-cookie", "www-authenticate", "content-type"}
	if cfg.OnDeny != nil {
		denyPatterns = append(denyPatterns, cfg.OnDeny.CopyToClient...)
	}

	client := &http.Client{
		Transport: svc.Transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Build check request.
			checkReq, err := http.NewRequestWithContext(r.Context(), r.Method, baseURL, nil)
			if err != nil {
				handleAuthzError(w, r, next, cfg.FailureModeAllow, "failed to create check request")
				return
			}

			// Forward matching headers from client.
			for key, values := range r.Header {
				if forwardHeaders[strings.ToLower(key)] {
					for _, v := range values {
						checkReq.Header.Add(key, v)
					}
				}
			}

			// Inject extra headers with interpolation.
			if cfg.OnCheck != nil {
				for _, h := range cfg.OnCheck.InjectHeaders {
					value := Interpolate(h.Value, r)
					checkReq.Header.Set(h.Key, value)
				}
			}

			// Send check request.
			resp, err := client.Do(checkReq)
			if err != nil {
				handleAuthzError(w, r, next, cfg.FailureModeAllow, "authz service unreachable")
				return
			}
			defer resp.Body.Close()

			// 2xx = allowed.
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				if len(allowPatterns) > 0 {
					copyMatchingHeaders(r.Header, resp.Header, allowPatterns)
				}
				next.ServeHTTP(w, r)
				return
			}

			// Non-2xx = denied. Copy matching headers to client.
			copyMatchingHeaders(w.Header(), resp.Header, denyPatterns)
			w.WriteHeader(resp.StatusCode)
			io.Copy(w, resp.Body)
		})
	}
}

func handleAuthzError(w http.ResponseWriter, r *http.Request, next http.Handler, allow bool, msg string) {
	if allow {
		next.ServeHTTP(w, r)
		return
	}
	http.Error(w, "ext_authz: "+msg, http.StatusForbidden)
}
