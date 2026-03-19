package proxy

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"fmt"
	"hash/crc32"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/felixge/httpsnoop"

	"github.com/achetronic/vrata/internal/model"
	"github.com/achetronic/vrata/internal/proxy/celeval"
	"github.com/achetronic/vrata/internal/proxy/middlewares"
)

// buildRouteHandler creates the http.Handler for a route, including middleware
// chain, forwarding, redirect, or direct response. The onCleanup function
// registers a callback to be invoked when the routing table is replaced.
func buildRouteHandler(
	route model.Route,
	group *model.RouteGroup,
	pools map[string]*DestinationPool,
	allMiddlewares map[string]model.Middleware,
	onCleanup func(func()),
	sessStore SessionStore,
) http.Handler {
	var handler http.Handler

	switch {
	case route.DirectResponse != nil:
		handler = directResponseHandler(route.DirectResponse)
	case route.Redirect != nil:
		handler = redirectHandler(route.Redirect)
	case route.Forward != nil:
		handler = forwardHandler(route.Forward, pools, group, route.ID, route.Name, sessStore)
	default:
		handler = http.NotFoundHandler()
	}

	mwIDs := collectMiddlewareIDs(route, group)

	var mws []middlewares.Middleware
	for _, mwID := range mwIDs {
		mw, ok := allMiddlewares[mwID]
		if !ok {
			continue
		}

		// Resolve override: route wins over group.
		ov := resolveOverride(mwID, route, group)

		if ov != nil && ov.Disabled {
			continue
		}

		m, cleanup := buildMiddleware(mw, pools)
		if m == nil {
			continue
		}
		if cleanup != nil {
			onCleanup(cleanup)
		}

		// Wrap with skipWhen/onlyWhen if configured.
		if ov != nil {
			m = wrapWithConditions(m, ov)
		}

		m = wrapWithMetrics(m, mw.Name, string(mw.Type))

		mws = append(mws, m)
	}

	if len(mws) > 0 {
		handler = middlewares.Chain(handler, mws...)
	}

	return handler
}

// resolveOverride returns the effective override for a middleware, with route
// winning over group. Returns nil if no override exists.
func resolveOverride(mwID string, route model.Route, group *model.RouteGroup) *model.MiddlewareOverride {
	if ov, ok := route.MiddlewareOverrides[mwID]; ok {
		return &ov
	}
	if group != nil {
		if ov, ok := group.MiddlewareOverrides[mwID]; ok {
			return &ov
		}
	}
	return nil
}

// wrapWithMetrics wraps a middleware with timing and rejection tracking.
// It records duration, pass/reject status into any MetricsCollectors present
// on the request context.
func wrapWithMetrics(m middlewares.Middleware, name, mwType string) middlewares.Middleware {
	return func(next http.Handler) http.Handler {
		inner := m(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			collectors := metricsFromCtx(r.Context())
			if len(collectors) == 0 {
				inner.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			captured := httpsnoop.CaptureMetrics(inner, w, r)
			dur := time.Since(start)

			passed := captured.Code < 400 || captured.Code == 0
			for _, mc := range collectors {
				mc.RecordMiddleware(name, mwType, dur, captured.Code, passed)
			}
		})
	}
}

// wrapWithConditions wraps a middleware with skipWhen/onlyWhen CEL evaluation.
// skipWhen: if ANY expression matches, skip the middleware.
// onlyWhen: if NO expression matches, skip the middleware.
func wrapWithConditions(m middlewares.Middleware, ov *model.MiddlewareOverride) middlewares.Middleware {
	var skipProgs []*celeval.Program
	for _, expr := range ov.SkipWhen {
		prg, err := celeval.Compile(expr)
		if err != nil {
			slog.Error("middleware: invalid skipWhen expression",
				slog.String("expr", expr),
				slog.String("error", err.Error()),
			)
			continue
		}
		skipProgs = append(skipProgs, prg)
	}

	var onlyProgs []*celeval.Program
	for _, expr := range ov.OnlyWhen {
		prg, err := celeval.Compile(expr)
		if err != nil {
			slog.Error("middleware: invalid onlyWhen expression",
				slog.String("expr", expr),
				slog.String("error", err.Error()),
			)
			continue
		}
		onlyProgs = append(onlyProgs, prg)
	}

	if len(skipProgs) == 0 && len(onlyProgs) == 0 {
		return m
	}

	return func(next http.Handler) http.Handler {
		inner := m(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, prg := range skipProgs {
				if prg.Eval(r) {
					next.ServeHTTP(w, r)
					return
				}
			}
			if len(onlyProgs) > 0 {
				matched := false
				for _, prg := range onlyProgs {
					if prg.Eval(r) {
						matched = true
						break
					}
				}
				if !matched {
					next.ServeHTTP(w, r)
					return
				}
			}
			inner.ServeHTTP(w, r)
		})
	}
}

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

func buildMiddleware(mw model.Middleware, pools map[string]*DestinationPool) (middlewares.Middleware, func()) {
	services := poolsToServices(pools)
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
		m, stop := middlewares.AccessLogMiddlewareWithStop(mw.AccessLog)
		return m, stop
	case model.MiddlewareTypeExtProc:
		m, stop := middlewares.ExtProcMiddlewareWithStop(mw.ExtProc, services)
		return m, stop
	default:
		return nil, nil
	}
}

func poolsToServices(pools map[string]*DestinationPool) map[string]middlewares.Service {
	services := make(map[string]middlewares.Service, len(pools))
	for id, pool := range pools {
		d := pool.Destination
		scheme := "http"
		if d.Options != nil && d.Options.TLS != nil &&
			d.Options.TLS.Mode != model.TLSModeNone && d.Options.TLS.Mode != "" {
			scheme = "https"
		}
		var transport *http.Transport
		if len(pool.Endpoints) > 0 {
			transport = pool.Endpoints[0].Transport
		}
		services[id] = middlewares.Service{
			BaseURL:   fmt.Sprintf("%s://%s:%d", scheme, d.Host, d.Port),
			Transport: transport,
		}
	}
	return services
}

func directResponseHandler(dr *model.RouteDirectResponse) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(int(dr.Status))
		if dr.Body != "" {
			w.Write([]byte(dr.Body))
		}
	})
}

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
func forwardHandler(fwd *model.ForwardAction, pools map[string]*DestinationPool, group *model.RouteGroup, routeID string, routeName string, sessStore SessionStore) http.Handler {
	var pinRing *destinationRing
	if fwd.DestinationBalancing != nil &&
		(fwd.DestinationBalancing.Algorithm == model.DestinationLBWeightedConsistentHash ||
			fwd.DestinationBalancing.Algorithm == model.DestinationLBSticky) {
		pinRing = buildDestinationRing(fwd.Destinations)
	}

	groupName := ""
	if group != nil {
		groupName = group.Name
	}
	retryCallback := func(req *http.Request, attempt int) {
		for _, mc := range metricsFromCtx(req.Context()) {
			mc.RecordRetry(routeName, groupName, attempt)
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Level 1: pick destination pool.
		pool := pickDestinationPool(fwd, pools, r, routeID, pinRing, w, sessStore)
		if pool == nil {
			http.Error(w, "no destination available", http.StatusBadGateway)
			return
		}

		// Level 2: pick endpoint within the pool.
		var endpoint *Endpoint
		if policies := pool.EndpointHashPolicies(); len(policies) > 0 {
			h := hashRequestWithPolicy(r, w, policies, pool.Destination.ID)
			endpoint = pool.PickByHash(h, r, w)
		} else {
			endpoint = pool.Pick(r, w)
		}
		if endpoint == nil {
			http.Error(w, "no healthy endpoint", http.StatusBadGateway)
			return
		}

		if pool.CircuitBreaker != nil && !pool.CircuitBreaker.Allow() {
			http.Error(w, "circuit breaker open", http.StatusServiceUnavailable)
			return
		}
		if pool.CircuitBreaker != nil {
			pool.CircuitBreaker.OnRequest()
			defer pool.CircuitBreaker.OnComplete()
		}
		if b, ok := pool.Balancer.(interface{ Done(string) }); ok {
			defer b.Done(endpoint.ID)
		}

		proxy := pool.ReverseProxyFor(endpoint)

		// Metrics: destination inflight tracking.
		destID := pool.Destination.ID
		collectors := metricsFromCtx(r.Context())
		for _, mc := range collectors {
			mc.DestInflightInc(destID)
		}
		defer func() {
			for _, mc := range collectors {
				mc.DestInflightDec(destID)
			}
		}()
		upstreamStart := time.Now()

		if fwd.Retry != nil {
			proxy.Transport = newRetryTransport(proxy.Transport, fwd.Retry, retryCallback)
		}
		if fwd.Rewrite != nil {
			applyRewrite(r, fwd.Rewrite)
		}
		if fwd.Mirror != nil {
			mirrorRequest(r, fwd.Mirror, pools)
			for _, mc := range collectors {
				mc.RecordMirror(routeID, fwd.Mirror.DestinationID)
			}
		}
		if group != nil && group.IncludeAttemptCount {
			r.Header.Set("X-Request-Attempt-Count", "1")
		}
		if fwd.Retry == nil && group != nil && group.RetryDefault != nil {
			proxy.Transport = newRetryTransport(proxy.Transport, group.RetryDefault, retryCallback)
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
				recordEndpointResult(endpoint, pool, capturedStatus, collectors, time.Since(upstreamStart))
				return
			}
		}

		proxy.ServeHTTP(wrappedW, r)
		recordEndpointResult(endpoint, pool, capturedStatus, collectors, time.Since(upstreamStart))
	})
}

func recordEndpointResult(ep *Endpoint, pool *DestinationPool, status int, collectors []*MetricsCollector, upstreamDur time.Duration) {
	if pool.CircuitBreaker != nil {
		if status >= 500 {
			pool.CircuitBreaker.RecordFailure()
		} else {
			pool.CircuitBreaker.RecordSuccess()
		}
	}
	if ep.OnResponse != nil {
		ep.OnResponse(pool.Destination.ID, status)
	}

	destID := pool.Destination.ID
	epID := ep.ID
	for _, mc := range collectors {
		mc.RecordDestination(destID, status, upstreamDur)
		mc.RecordEndpoint(destID, epID, status, upstreamDur)
	}
}

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

// ─── Level 1: Destination selection ─────────────────────────────────────────

func pickDestinationPool(
	fwd *model.ForwardAction,
	pools map[string]*DestinationPool,
	r *http.Request,
	routeID string,
	pinRing *destinationRing,
	w http.ResponseWriter,
	sessStore SessionStore,
) *DestinationPool {
	if len(fwd.Destinations) == 0 {
		return nil
	}
	if len(fwd.Destinations) == 1 {
		return pools[fwd.Destinations[0].DestinationID]
	}

	healthy := filterHealthyPools(fwd.Destinations, pools)
	if len(healthy) == 0 {
		return nil
	}

	if fwd.DestinationBalancing != nil {
		switch fwd.DestinationBalancing.Algorithm {
		case model.DestinationLBWeightedConsistentHash:
			if pinRing != nil {
				return pickPinnedPool(fwd, pools, r, w, routeID, pinRing, healthy)
			}
		case model.DestinationLBSticky:
			if sessStore != nil {
				return pickStickyPool(fwd, pools, r, w, routeID, healthy, sessStore)
			}
			if pinRing != nil {
				return pickPinnedPool(fwd, pools, r, w, routeID, pinRing, healthy)
			}
		}
	}

	return SelectDestination(healthy, pools)
}

func pickPinnedPool(
	fwd *model.ForwardAction,
	pools map[string]*DestinationPool,
	r *http.Request,
	w http.ResponseWriter,
	routeID string,
	ring *destinationRing,
	healthy []model.DestinationRef,
) *DestinationPool {
	cookieName := "_vrata_destination_pin"
	var ttlStr string
	if wch := fwd.DestinationBalancing.WeightedConsistentHash; wch != nil && wch.Cookie != nil {
		if wch.Cookie.Name != "" {
			cookieName = wch.Cookie.Name
		}
		ttlStr = wch.Cookie.TTL
	}

	sid := ""
	if c, err := r.Cookie(cookieName); err == nil {
		sid = c.Value
	}
	if sid == "" {
		sid = generateSessionID()
		ttl := parseTTL(ttlStr, time.Hour)
		http.SetCookie(w, &http.Cookie{
			Name: cookieName, Value: sid, Path: "/",
			MaxAge: int(ttl.Seconds()), HttpOnly: true, SameSite: http.SameSiteLaxMode,
		})
	}

	hashKey := crc32.ChecksumIEEE([]byte(sid + ":" + routeID))
	validSet := make(map[string]bool, len(healthy))
	for _, b := range healthy {
		validSet[b.DestinationID] = true
	}
	destID := ring.PickValid(hashKey, validSet)
	if destID == "" {
		return SelectDestination(healthy, pools)
	}
	return pools[destID]
}

func pickStickyPool(
	fwd *model.ForwardAction,
	pools map[string]*DestinationPool,
	r *http.Request,
	w http.ResponseWriter,
	routeID string,
	healthy []model.DestinationRef,
	store SessionStore,
) *DestinationPool {
	cookieName := "_vrata_destination_pin"
	var ttlStr string
	if fwd.DestinationBalancing.Sticky != nil && fwd.DestinationBalancing.Sticky.Cookie != nil {
		if fwd.DestinationBalancing.Sticky.Cookie.Name != "" {
			cookieName = fwd.DestinationBalancing.Sticky.Cookie.Name
		}
		ttlStr = fwd.DestinationBalancing.Sticky.Cookie.TTL
	}

	sid := ""
	if c, err := r.Cookie(cookieName); err == nil {
		sid = c.Value
	}
	isNew := sid == ""
	if isNew {
		sid = generateSessionID()
		ttl := parseTTL(ttlStr, time.Hour)
		http.SetCookie(w, &http.Cookie{
			Name: cookieName, Value: sid, Path: "/",
			MaxAge: int(ttl.Seconds()), HttpOnly: true, SameSite: http.SameSiteLaxMode,
		})
	}

	validSet := make(map[string]bool, len(healthy))
	for _, b := range healthy {
		validSet[b.DestinationID] = true
	}

	if !isNew {
		if destID, err := store.Get(r.Context(), sid, routeID); err == nil && destID != "" {
			if validSet[destID] {
				return pools[destID]
			}
		}
	}

	pool := SelectDestination(healthy, pools)
	if pool == nil {
		return nil
	}
	ttlSec := int(parseTTL(ttlStr, time.Hour).Seconds())
	_ = store.Set(r.Context(), sid, routeID, pool.Destination.ID, ttlSec)
	return pool
}

func filterHealthyPools(dests []model.DestinationRef, pools map[string]*DestinationPool) []model.DestinationRef {
	var healthy []model.DestinationRef
	for _, b := range dests {
		pool, ok := pools[b.DestinationID]
		if !ok {
			continue
		}
		for _, ep := range pool.Endpoints {
			if isHealthy(ep) {
				healthy = append(healthy, b)
				break
			}
		}
	}
	return healthy
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func generateSessionID() string {
	b := make([]byte, 16)
	cryptorand.Read(b)
	return fmt.Sprintf("%x", b)
}

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

func isHealthy(u *Endpoint) bool {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.Healthy
}

func mirrorRequest(original *http.Request, mirror *model.RouteMirror, pools map[string]*DestinationPool) {
	if mirror.Percentage > 0 && mirror.Percentage < 100 {
		if rand.Uint32()%100 >= mirror.Percentage {
			return
		}
	}
	pool, ok := pools[mirror.DestinationID]
	if !ok || len(pool.Endpoints) == 0 {
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
	ep := pool.Endpoints[0]
	go func() {
		proxy := pool.ReverseProxyFor(ep)
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

func applyRewrite(r *http.Request, rw *model.RouteRewrite) {
	if rw.PathRegex != nil {
		re, err := cachedCompile(rw.PathRegex.Pattern)
		if err == nil {
			r.URL.Path = re.ReplaceAllString(r.URL.Path, rw.PathRegex.Substitution)
		}
	} else if rw.Path != "" {
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

func formatGRPCTimeout(d time.Duration) string {
	if us := d.Microseconds(); us < 1000 {
		return fmt.Sprintf("%du", us)
	}
	return fmt.Sprintf("%dm", d.Milliseconds())
}

var regexCache sync.Map

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
