package proxy

import (
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/achetronic/rutoso/internal/model"
)

// buildRouteHandler creates the http.Handler for a route, including middleware
// chain, forwarding, redirect, or direct response.
func buildRouteHandler(
	route model.Route,
	group *model.RouteGroup,
	upstreams map[string]*Upstream,
	allMiddlewares map[string]model.Middleware,
) http.Handler {
	var handler http.Handler

	switch {
	case route.DirectResponse != nil:
		handler = directResponseHandler(route.DirectResponse)
	case route.Redirect != nil:
		handler = redirectHandler(route.Redirect)
	case route.Forward != nil:
		handler = forwardHandler(route.Forward, upstreams)
	default:
		handler = http.NotFoundHandler()
	}

	// Collect active middleware IDs from group + route.
	mwIDs := collectMiddlewareIDs(route, group)

	// Build middleware chain.
	var mws []Middleware
	for _, mwID := range mwIDs {
		mw, ok := allMiddlewares[mwID]
		if !ok {
			continue
		}
		if m := buildMiddleware(mw, upstreams); m != nil {
			mws = append(mws, m)
		}
	}

	if len(mws) > 0 {
		handler = Chain(handler, mws...)
	}

	return handler
}

// collectMiddlewareIDs merges group + route middleware IDs. Group first, route
// after (route additions come on top).
func collectMiddlewareIDs(route model.Route, group *model.RouteGroup) []string {
	seen := make(map[string]bool)
	var ids []string

	if group != nil {
		for _, id := range group.MiddlewareIDs {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	for _, id := range route.MiddlewareIDs {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// buildMiddleware creates a Middleware function from a model.Middleware.
func buildMiddleware(mw model.Middleware, upstreams map[string]*Upstream) Middleware {
	switch mw.Type {
	case model.MiddlewareTypeCORS:
		return CORSMiddleware(mw.CORS)
	case model.MiddlewareTypeHeaders:
		return HeadersMiddleware(mw.Headers)
	case model.MiddlewareTypeExtAuthz:
		return ExtAuthzMiddleware(mw.ExtAuthz, upstreams)
	case model.MiddlewareTypeRateLimit:
		return RateLimitMiddleware(mw.RateLimit)
	case model.MiddlewareTypeJWT:
		return JWTMiddleware(mw.JWT, upstreams)
	default:
		return nil
	}
}

// directResponseHandler returns a fixed response.
func directResponseHandler(dr *model.RouteDirectResponse) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(int(dr.Status))
		if dr.Body != "" {
			w.Write([]byte(dr.Body))
		}
	})
}

// redirectHandler returns an HTTP redirect.
func redirectHandler(rd *model.RouteRedirect) http.Handler {
	code := int(rd.Code)
	if code == 0 {
		code = http.StatusMovedPermanently
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := rd.URL
		if target == "" {
			u := *r.URL
			if rd.Scheme != "" {
				u.Scheme = rd.Scheme
			}
			if rd.Host != "" {
				u.Host = rd.Host
			}
			if rd.Path != "" {
				u.Path = rd.Path
			}
			if rd.StripQuery {
				u.RawQuery = ""
			}
			target = u.String()
		}
		http.Redirect(w, r, target, code)
	})
}

// forwardHandler creates a handler that proxies to upstream destinations.
func forwardHandler(fwd *model.ForwardAction, upstreams map[string]*Upstream) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream := SelectBackend(fwd.Backends, upstreams)
		if upstream == nil {
			http.Error(w, "no upstream available", http.StatusBadGateway)
			return
		}

		proxy := upstream.ReverseProxy()

		// Retry.
		if fwd.Retry != nil {
			proxy.Transport = newRetryTransport(proxy.Transport, fwd.Retry)
		}

		// Path rewrite.
		if fwd.Rewrite != nil {
			applyRewrite(r, fwd.Rewrite)
		}

		// Request mirror (fire-and-forget).
		if fwd.Mirror != nil {
			go mirrorRequest(r, fwd.Mirror, upstreams)
		}

		// WebSocket upgrade — ReverseProxy handles it natively.

		// Timeouts.
		if fwd.Timeouts != nil && fwd.Timeouts.Request != "" {
			if d, err := time.ParseDuration(fwd.Timeouts.Request); err == nil {
				http.TimeoutHandler(proxy, d, "request timeout").ServeHTTP(w, r)
				return
			}
		}

		proxy.ServeHTTP(w, r)
	})
}

// mirrorRequest sends a copy of the request to the mirror destination.
// Runs in a goroutine — fire-and-forget, response is discarded.
func mirrorRequest(original *http.Request, mirror *model.RouteMirror, upstreams map[string]*Upstream) {
	if mirror.Percentage > 0 && mirror.Percentage < 100 {
		if rand.Uint32()%100 >= mirror.Percentage {
			return
		}
	}

	upstream, ok := upstreams[mirror.DestinationID]
	if !ok {
		return
	}

	proxy := upstream.ReverseProxy()
	// Create a no-op response writer.
	proxy.ServeHTTP(&discardResponseWriter{}, original)
}

// discardResponseWriter discards all output.
type discardResponseWriter struct{}

func (discardResponseWriter) Header() http.Header        { return http.Header{} }
func (discardResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (discardResponseWriter) WriteHeader(int)             {}

// applyRewrite modifies the request URL based on RouteRewrite config.
func applyRewrite(r *http.Request, rw *model.RouteRewrite) {
	if rw.Path != "" {
		// Prefix rewrite: replace the matched prefix with the new path.
		r.URL.Path = rw.Path + strings.TrimPrefix(r.URL.Path, r.URL.Path)
		if r.URL.RawPath != "" {
			r.URL.RawPath = rw.Path
		}
	}
	if rw.Host != "" {
		r.Host = rw.Host
	}
	if rw.HostFromHeader != "" {
		if val := r.Header.Get(rw.HostFromHeader); val != "" {
			r.Host = val
		}
	}
	if rw.AutoHost {
		// Host will be set by the reverse proxy to the upstream host.
		r.Host = ""
	}
}

// formatAddr returns host:port from a destination.
func formatAddr(d model.Destination) string {
	return fmt.Sprintf("%s:%d", d.Host, d.Port)
}
