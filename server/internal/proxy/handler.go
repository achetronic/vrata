package proxy

import (
	"fmt"
	"math/rand"
	"net/http"
	"regexp"
	"time"

	"github.com/achetronic/rutoso/internal/model"
	"github.com/achetronic/rutoso/internal/proxy/middlewares"
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
		handler = forwardHandler(route.Forward, upstreams, group)
	default:
		handler = http.NotFoundHandler()
	}

	// Collect active middleware IDs from group + route.
	mwIDs := collectMiddlewareIDs(route, group)

	// Build middleware chain.
	var mws []middlewares.Middleware
	for _, mwID := range mwIDs {
		mw, ok := allMiddlewares[mwID]
		if !ok {
			continue
		}
		// Check if route has an override that disables this middleware.
		if ov, hasOv := route.MiddlewareOverrides[mwID]; hasOv && ov.Disabled {
			continue
		}
		// Check group override too.
		if group != nil {
			if ov, hasOv := group.MiddlewareOverrides[mwID]; hasOv && ov.Disabled {
				// But route can re-enable by NOT having disabled.
				if _, routeHasOv := route.MiddlewareOverrides[mwID]; !routeHasOv {
					continue
				}
			}
		}
		if m := buildMiddleware(mw, upstreams); m != nil {
			mws = append(mws, m)
		}
	}

	if len(mws) > 0 {
		handler = middlewares.Chain(handler, mws...)
	}

	return handler
}

// collectMiddlewareIDs merges group + route middleware IDs.
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

// buildMiddleware creates a middleware function from a model.Middleware.
func buildMiddleware(mw model.Middleware, upstreams map[string]*Upstream) middlewares.Middleware {
	services := upstreamsToServices(upstreams)
	switch mw.Type {
	case model.MiddlewareTypeCORS:
		return middlewares.CORSMiddleware(mw.CORS)
	case model.MiddlewareTypeHeaders:
		return middlewares.HeadersMiddleware(mw.Headers)
	case model.MiddlewareTypeExtAuthz:
		return middlewares.ExtAuthzMiddleware(mw.ExtAuthz, services)
	case model.MiddlewareTypeRateLimit:
		return middlewares.RateLimitMiddleware(mw.RateLimit)
	case model.MiddlewareTypeJWT:
		return middlewares.JWTMiddleware(mw.JWT, services)
	case model.MiddlewareTypeAccessLog:
		return middlewares.AccessLogMiddleware(mw.AccessLog)
	default:
		return nil
	}
}

// upstreamsToServices converts the Upstream map to a Service map for middlewares.
func upstreamsToServices(upstreams map[string]*Upstream) map[string]middlewares.Service {
	services := make(map[string]middlewares.Service, len(upstreams))
	for id, u := range upstreams {
		d := u.Destination
		scheme := "http"
		if d.Options != nil && d.Options.TLS != nil &&
			d.Options.TLS.Mode != model.TLSModeNone && d.Options.TLS.Mode != "" {
			scheme = "https"
		}
		services[id] = middlewares.Service{
			BaseURL:   fmt.Sprintf("%s://%s:%d", scheme, d.Host, d.Port),
			Transport: u.Transport,
		}
	}
	return services
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
func forwardHandler(fwd *model.ForwardAction, upstreams map[string]*Upstream, group *model.RouteGroup) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Select backend.
		upstream := pickUpstream(fwd, upstreams, r)
		if upstream == nil {
			http.Error(w, "no upstream available", http.StatusBadGateway)
			return
		}

		// Circuit breaker check.
		if upstream.CircuitBreaker != nil && !upstream.CircuitBreaker.Allow() {
			http.Error(w, "circuit breaker open", http.StatusServiceUnavailable)
			return
		}

		if upstream.CircuitBreaker != nil {
			upstream.CircuitBreaker.OnRequest()
			defer upstream.CircuitBreaker.OnComplete()
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

		// WebSocket: Go's ReverseProxy handles Upgrade headers natively
		// for HTTP/1.1. No explicit config needed.

		// Include attempt count header (from group config).
		if group != nil && group.IncludeAttemptCount {
			r.Header.Set("X-Request-Attempt-Count", "1")
		}

		// Apply group default retry if route has none.
		if fwd.Retry == nil && group != nil && group.RetryDefault != nil {
			proxy.Transport = newRetryTransport(proxy.Transport, group.RetryDefault)
		}

		// Max gRPC timeout.
		if fwd.MaxGRPCTimeout != "" {
			if maxDur, err := time.ParseDuration(fwd.MaxGRPCTimeout); err == nil {
				if grpcTimeout := r.Header.Get("grpc-timeout"); grpcTimeout != "" {
					if clientDur, err := parseGRPCTimeout(grpcTimeout); err == nil {
						if clientDur > maxDur {
							r.Header.Set("grpc-timeout", fmt.Sprintf("%dm", int(maxDur.Milliseconds())))
						}
					}
				}
			}
		}

		// Idle timeout.
		if fwd.Timeouts != nil && fwd.Timeouts.Idle != "" {
			if d, err := time.ParseDuration(fwd.Timeouts.Idle); err == nil {
				proxy.Transport.(*http.Transport).IdleConnTimeout = d
			}
		}

		// Request timeout.
		if fwd.Timeouts != nil && fwd.Timeouts.Request != "" {
			if d, err := time.ParseDuration(fwd.Timeouts.Request); err == nil {
				http.TimeoutHandler(proxy, d, "request timeout").ServeHTTP(w, r)
				if upstream.CircuitBreaker != nil {
					upstream.CircuitBreaker.RecordSuccess()
				}
				return
			}
		}

		proxy.ServeHTTP(w, r)
		if upstream.CircuitBreaker != nil {
			upstream.CircuitBreaker.RecordSuccess()
		}
	})
}

// pickUpstream selects the right upstream based on balancing config and hash policy.
func pickUpstream(fwd *model.ForwardAction, upstreams map[string]*Upstream, r *http.Request) *Upstream {
	if len(fwd.Backends) == 0 {
		return nil
	}
	if len(fwd.Backends) == 1 {
		u := upstreams[fwd.Backends[0].DestinationID]
		if u != nil && !isHealthy(u) {
			return nil
		}
		return u
	}

	// Filter healthy backends.
	var healthy []model.BackendRef
	for _, b := range fwd.Backends {
		u, ok := upstreams[b.DestinationID]
		if ok && isHealthy(u) {
			healthy = append(healthy, b)
		}
	}
	if len(healthy) == 0 {
		return nil
	}

	// Use consistent hashing if destination is configured for it.
	for _, b := range healthy {
		u := upstreams[b.DestinationID]
		if u != nil && u.Balancer != nil {
			if len(fwd.HashPolicy) > 0 {
				h := hashRequestWithPolicy(r, fwd.HashPolicy)
				r.Header.Set("X-Rutoso-Hash", fmt.Sprintf("%d", h))
			}
			return u.Balancer.Pick(r, healthy, upstreams)
		}
	}

	return SelectBackend(healthy, upstreams)
}

func isHealthy(u *Upstream) bool {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.Healthy
}

// mirrorRequest sends a copy of the request to the mirror destination.
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
	proxy.ServeHTTP(&discardResponseWriter{}, original)
}

type discardResponseWriter struct{}

func (discardResponseWriter) Header() http.Header        { return http.Header{} }
func (discardResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (discardResponseWriter) WriteHeader(int)             {}

// applyRewrite modifies the request URL based on RouteRewrite config.
func applyRewrite(r *http.Request, rw *model.RouteRewrite) {
	if rw.PathRegex != nil {
		// Regex rewrite.
		re, err := compileOnce(rw.PathRegex.Pattern)
		if err == nil {
			r.URL.Path = re.ReplaceAllString(r.URL.Path, rw.PathRegex.Substitution)
		}
	} else if rw.Path != "" {
		// Prefix rewrite: replace the path with the new prefix.
		r.URL.Path = rw.Path
	}

	if rw.Host != "" {
		r.Host = rw.Host
		r.Header.Set("Host", rw.Host)
	}
	if rw.HostFromHeader != "" {
		if val := r.Header.Get(rw.HostFromHeader); val != "" {
			r.Host = val
			r.Header.Set("Host", val)
		}
	}
	if rw.AutoHost {
		r.Host = ""
	}
}

// parseGRPCTimeout parses a grpc-timeout header value.
func parseGRPCTimeout(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid grpc-timeout: %s", s)
	}
	val := s[:len(s)-1]
	unit := s[len(s)-1]
	var d time.Duration
	var n int
	if _, err := fmt.Sscanf(val, "%d", &n); err != nil {
		return 0, err
	}
	switch unit {
	case 'H':
		d = time.Duration(n) * time.Hour
	case 'M':
		d = time.Duration(n) * time.Minute
	case 'S':
		d = time.Duration(n) * time.Second
	case 'm':
		d = time.Duration(n) * time.Millisecond
	case 'u':
		d = time.Duration(n) * time.Microsecond
	case 'n':
		d = time.Duration(n) * time.Nanosecond
	default:
		return 0, fmt.Errorf("unknown grpc-timeout unit: %c", unit)
	}
	return d, nil
}

// compileOnce compiles a regex (should be cached in production).
func compileOnce(pattern string) (*regexp.Regexp, error) {
	return regexp.Compile(pattern)
}
