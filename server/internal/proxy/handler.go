package proxy

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"fmt"
	"hash/crc32"
	"io"
	"math/rand"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/felixge/httpsnoop"

	"github.com/achetronic/rutoso/internal/model"
	"github.com/achetronic/rutoso/internal/proxy/middlewares"
)

// buildRouteHandler creates the http.Handler for a route, including middleware
// chain, forwarding, redirect, or direct response. The onCleanup function
// registers a callback to be invoked when the routing table is replaced.
func buildRouteHandler(
	route model.Route,
	group *model.RouteGroup,
	upstreams map[string]*Upstream,
	allMiddlewares map[string]model.Middleware,
	onCleanup func(func()),
) http.Handler {
	var handler http.Handler

	switch {
	case route.DirectResponse != nil:
		handler = directResponseHandler(route.DirectResponse)
	case route.Redirect != nil:
		handler = redirectHandler(route.Redirect)
	case route.Forward != nil:
		handler = forwardHandler(route.Forward, upstreams, group, route.ID)
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
		m, cleanup := buildMiddleware(mw, upstreams)
		if m != nil {
			mws = append(mws, m)
			if cleanup != nil {
				onCleanup(cleanup)
			}
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
// Returns the middleware and an optional cleanup function to call when
// the routing table is replaced.
func buildMiddleware(mw model.Middleware, upstreams map[string]*Upstream) (middlewares.Middleware, func()) {
	services := upstreamsToServices(upstreams)
	switch mw.Type {
	case model.MiddlewareTypeCORS:
		return middlewares.CORSMiddleware(mw.CORS), nil
	case model.MiddlewareTypeHeaders:
		return middlewares.HeadersMiddleware(mw.Headers), nil
	case model.MiddlewareTypeExtAuthz:
		return middlewares.ExtAuthzMiddleware(mw.ExtAuthz, services), nil
	case model.MiddlewareTypeRateLimit:
		m, stop := middlewares.RateLimitMiddlewareWithStop(mw.RateLimit)
		return m, stop
	case model.MiddlewareTypeJWT:
		m, stop := middlewares.JWTMiddlewareWithStop(mw.JWT, services)
		return m, stop
	case model.MiddlewareTypeAccessLog:
		return middlewares.AccessLogMiddleware(mw.AccessLog), nil
	case model.MiddlewareTypeExtProc:
		return middlewares.ExtProcMiddleware(mw.ExtProc, services), nil
	default:
		return nil, nil
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
func forwardHandler(fwd *model.ForwardAction, upstreams map[string]*Upstream, group *model.RouteGroup, routeID string) http.Handler {
	var pinRing *destinationRing
	if fwd.DestinationPinning != nil {
		pinRing = buildDestinationRing(fwd.Destinations)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream := pickDestination(fwd, upstreams, r, routeID, pinRing, w)
		if upstream == nil {
			http.Error(w, "no upstream available", http.StatusBadGateway)
			return
		}

		if upstream.CircuitBreaker != nil && !upstream.CircuitBreaker.Allow() {
			http.Error(w, "circuit breaker open", http.StatusServiceUnavailable)
			return
		}

		if upstream.CircuitBreaker != nil {
			upstream.CircuitBreaker.OnRequest()
			defer upstream.CircuitBreaker.OnComplete()
		}

		if b, ok := upstream.Balancer.(interface{ Done(string) }); ok {
			defer b.Done(upstream.Destination.ID)
		}

		proxy := upstream.ReverseProxy()

		if fwd.Retry != nil {
			proxy.Transport = newRetryTransport(proxy.Transport, fwd.Retry)
		}

		if fwd.Rewrite != nil {
			applyRewrite(r, fwd.Rewrite)
		}

		if fwd.Mirror != nil {
			mirrorRequest(r, fwd.Mirror, upstreams)
		}

		if group != nil && group.IncludeAttemptCount {
			r.Header.Set("X-Request-Attempt-Count", "1")
		}

		if fwd.Retry == nil && group != nil && group.RetryDefault != nil {
			proxy.Transport = newRetryTransport(proxy.Transport, group.RetryDefault)
		}

		if fwd.MaxGRPCTimeout != "" {
			if maxDur, err := time.ParseDuration(fwd.MaxGRPCTimeout); err == nil {
				if grpcTimeout := r.Header.Get("grpc-timeout"); grpcTimeout != "" {
					if clientDur, err := parseGRPCTimeout(grpcTimeout); err == nil {
						if clientDur > maxDur {
							r.Header.Set("grpc-timeout", formatGRPCTimeout(maxDur))
						}
					}
				}
			}
		}

		if fwd.Timeouts != nil && fwd.Timeouts.Idle != "" {
			if d, err := time.ParseDuration(fwd.Timeouts.Idle); err == nil {
				if t, ok := unwrapHTTPTransport(proxy.Transport); ok {
					t.IdleConnTimeout = d
				}
			}
		}

		capturedStatus := http.StatusOK
		wrappedW := httpsnoop.Wrap(w, httpsnoop.Hooks{
			WriteHeader: func(next httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
				return func(code int) {
					capturedStatus = code
					next(code)
				}
			},
		})

		if fwd.Timeouts != nil && fwd.Timeouts.Request != "" {
			if d, err := time.ParseDuration(fwd.Timeouts.Request); err == nil {
				http.TimeoutHandler(proxy, d, "request timeout").ServeHTTP(wrappedW, r)
				recordUpstreamResult(upstream, capturedStatus)
				return
			}
		}

		proxy.ServeHTTP(wrappedW, r)
		recordUpstreamResult(upstream, capturedStatus)
	})
}

// recordUpstreamResult records success or failure on the circuit breaker
// and notifies the outlier detector based on the upstream response status.
func recordUpstreamResult(upstream *Upstream, status int) {
	if upstream.CircuitBreaker != nil {
		if status >= 500 {
			upstream.CircuitBreaker.RecordFailure()
		} else {
			upstream.CircuitBreaker.RecordSuccess()
		}
	}
	if upstream.OnResponse != nil {
		upstream.OnResponse(upstream.Destination.ID, status)
	}
}

// unwrapHTTPTransport safely extracts the underlying *http.Transport from
// a potentially wrapped transport chain (e.g. retryTransport).
func unwrapHTTPTransport(rt http.RoundTripper) (*http.Transport, bool) {
	for {
		switch t := rt.(type) {
		case *http.Transport:
			return t, true
		case interface{ Unwrap() http.RoundTripper }:
			rt = t.Unwrap()
		default:
			return nil, false
		}
	}
}

// pickDestination selects a destination from the dests list.
// Level 1: destination selection (weights or pinning).
// Level 2 (endpoint selection within a destination) is handled by the
// balancer inside ReverseProxy, not here.
func pickDestination(
	fwd *model.ForwardAction,
	upstreams map[string]*Upstream,
	r *http.Request,
	routeID string,
	pinRing *destinationRing,
	w http.ResponseWriter,
) *Upstream {
	if len(fwd.Destinations) == 0 {
		return nil
	}
	if len(fwd.Destinations) == 1 {
		u := upstreams[fwd.Destinations[0].DestinationID]
		if u != nil && !isHealthy(u) {
			return nil
		}
		return u
	}

	// Filter healthy dests.
	healthy := filterHealthy(fwd.Destinations, upstreams)
	if len(healthy) == 0 {
		return nil
	}

	// Destination pinning: use weighted consistent hash with session cookie.
	if pinRing != nil && fwd.DestinationPinning != nil {
		return pickPinned(fwd, upstreams, r, w, routeID, pinRing, healthy)
	}

	// Default: weighted random.
	return SelectDestination(healthy, upstreams)
}

// pickPinned selects a destination using the weighted consistent hash ring
// and a session cookie. If the client has no cookie, one is generated.
func pickPinned(
	fwd *model.ForwardAction,
	upstreams map[string]*Upstream,
	r *http.Request,
	w http.ResponseWriter,
	routeID string,
	ring *destinationRing,
	healthy []model.DestinationRef,
) *Upstream {
	cfg := fwd.DestinationPinning
	cookieName := cfg.CookieName
	if cookieName == "" {
		cookieName = "_rutoso_pin"
	}

	sid := ""
	if c, err := r.Cookie(cookieName); err == nil {
		sid = c.Value
	}

	if sid == "" {
		sid = generateSessionID()
		ttl := parseTTL(cfg.TTL, time.Hour)
		http.SetCookie(w, &http.Cookie{
			Name:     cookieName,
			Value:    sid,
			Path:     "/",
			MaxAge:   int(ttl.Seconds()),
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
	}

	hashKey := crc32.ChecksumIEEE([]byte(sid + ":" + routeID))

	validSet := make(map[string]bool, len(healthy))
	for _, b := range healthy {
		validSet[b.DestinationID] = true
	}

	destID := ring.PickValid(hashKey, validSet)
	if destID == "" {
		return SelectDestination(healthy, upstreams)
	}
	return upstreams[destID]
}

// filterHealthy returns only dests whose upstream is healthy.
func filterHealthy(dests []model.DestinationRef, upstreams map[string]*Upstream) []model.DestinationRef {
	var healthy []model.DestinationRef
	for _, b := range dests {
		u, ok := upstreams[b.DestinationID]
		if ok && isHealthy(u) {
			healthy = append(healthy, b)
		}
	}
	return healthy
}

// generateSessionID creates a random session identifier.
func generateSessionID() string {
	b := make([]byte, 16)
	cryptorand.Read(b)
	return fmt.Sprintf("%x", b)
}

// parseTTL parses a duration string with a fallback default.
func parseTTL(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}

func isHealthy(u *Upstream) bool {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.Healthy
}

// mirrorRequest clones the request and sends it to the mirror destination
// in a background goroutine. The original request is not affected.
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

	var bodyBytes []byte
	if original.Body != nil {
		bodyBytes, _ = io.ReadAll(original.Body)
		original.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	clone := original.Clone(context.Background())
	if bodyBytes != nil {
		clone.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	go func() {
		proxy := upstream.ReverseProxy()
		proxy.ServeHTTP(newDiscardResponseWriter(), clone)
	}()
}

type discardResponseWriter struct {
	header http.Header
}

func newDiscardResponseWriter() *discardResponseWriter {
	return &discardResponseWriter{header: make(http.Header)}
}

func (d *discardResponseWriter) Header() http.Header        { return d.header }
func (d *discardResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (d *discardResponseWriter) WriteHeader(int)             {}

// applyRewrite modifies the request URL based on RouteRewrite config.
func applyRewrite(r *http.Request, rw *model.RouteRewrite) {
	if rw.PathRegex != nil {
		// Regex rewrite.
		re, err := cachedCompile(rw.PathRegex.Pattern)
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

// formatGRPCTimeout formats a duration as a grpc-timeout header value,
// using the most precise unit that avoids truncation.
func formatGRPCTimeout(d time.Duration) string {
	if us := d.Microseconds(); us < 1000 {
		return fmt.Sprintf("%du", us)
	}
	return fmt.Sprintf("%dm", d.Milliseconds())
}

// regexCache caches compiled regular expressions to avoid recompilation
// on every request.
var regexCache sync.Map

// cachedCompile returns a compiled regex, using a cache to avoid
// recompilation on every request.
func cachedCompile(pattern string) (*regexp.Regexp, error) {
	if v, ok := regexCache.Load(pattern); ok {
		return v.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	regexCache.Store(pattern, re)
	return re, nil
}
