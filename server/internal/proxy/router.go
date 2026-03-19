// Package proxy implements a programmable HTTP reverse proxy for Vrata.
// Vrata is a programmable reverse proxy. Configuration is applied in real
// time via atomic swap of the routing table — no restarts, no file generation.
package proxy

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/felixge/httpsnoop"

	"github.com/achetronic/vrata/internal/model"
	"github.com/achetronic/vrata/internal/proxy/celeval"
)

// Router holds the current routing table and dispatches incoming requests
// to the appropriate handler. The table is swapped atomically on config reload.
type Router struct {
	table      atomic.Pointer[RoutingTable]
	metricsMu  sync.RWMutex
	collectors []*MetricsCollector
}

// RoutingTable is an immutable snapshot of the current proxy configuration.
type RoutingTable struct {
	routes       []compiledRoute
	pools        map[string]*DestinationPool
	middlewares  map[string]model.Middleware
	cleanups     []func()
	sessionStore SessionStore
}

// AddCleanup registers a function to be called when this table is replaced.
func (t *RoutingTable) AddCleanup(fn func()) {
	t.cleanups = append(t.cleanups, fn)
}

// compiledRoute is a pre-compiled route ready for matching.
type compiledRoute struct {
	model            model.Route
	group            *model.RouteGroup
	pathRegex        *regexp.Regexp
	pathPrefix       string
	pathExact        string
	methods          map[string]bool
	headers          []compiledHeaderMatcher
	queryParams      []compiledQueryParamMatcher
	grpcOnly         bool
	hostnames        []string
	celProgram       *celeval.Program
	handler          http.Handler
}

// compiledHeaderMatcher is a pre-compiled header matcher.
type compiledHeaderMatcher struct {
	name  string
	value string
	regex *regexp.Regexp // nil if exact match
}

// compiledQueryParamMatcher is a pre-compiled query param matcher.
type compiledQueryParamMatcher struct {
	name  string
	value string
	regex *regexp.Regexp // nil if exact match
}

// NewRouter creates a new Router with an empty routing table.
func NewRouter() *Router {
	r := &Router{}
	r.table.Store(&RoutingTable{
		pools:       make(map[string]*DestinationPool),
		middlewares: make(map[string]model.Middleware),
	})
	return r
}

// SwapTable atomically replaces the routing table. Active requests on the
// old table are not affected — they complete with the old config.
// Cleanup functions registered on the old table are called asynchronously.
func (r *Router) SwapTable(t *RoutingTable) {
	old := r.table.Swap(t)
	if old != nil {
		for _, fn := range old.cleanups {
			fn()
		}
	}
}

type metricsCtxKey struct{}

// metricsFromCtx extracts MetricsCollectors from a request context.
func metricsFromCtx(ctx context.Context) []*MetricsCollector {
	if v, ok := ctx.Value(metricsCtxKey{}).([]*MetricsCollector); ok {
		return v
	}
	return nil
}

// SetMetricsCollectors replaces the set of active metrics collectors.
func (r *Router) SetMetricsCollectors(mcs []*MetricsCollector) {
	r.metricsMu.Lock()
	r.collectors = mcs
	r.metricsMu.Unlock()
}

// ServeHTTP dispatches the request to the matching route.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	t := r.table.Load()

	for i := range t.routes {
		cr := &t.routes[i]
		if cr.match(req) {
			r.metricsMu.RLock()
			collectors := r.collectors
			r.metricsMu.RUnlock()

			if len(collectors) == 0 {
				cr.handler.ServeHTTP(w, req)
				return
			}

			routeName := cr.model.Name
			groupName := ""
			if cr.group != nil {
				groupName = cr.group.Name
			}

			for _, mc := range collectors {
				mc.RouteInflightInc(routeName, groupName)
			}

			reqSize := req.ContentLength
			if reqSize < 0 {
				reqSize = 0
			}

			ctx := context.WithValue(req.Context(), metricsCtxKey{}, collectors)
			req = req.WithContext(ctx)

			start := time.Now()
			m := httpsnoop.CaptureMetrics(cr.handler, w, req)
			dur := time.Since(start)

			for _, mc := range collectors {
				mc.RouteInflightDec(routeName, groupName)
				mc.RecordRoute(routeName, groupName, req.Method, m.Code, dur, reqSize, int64(m.Written))
			}
			return
		}
	}

	writeProxyError(w, http.StatusNotFound, "no matching route")
}

// match checks if the request matches this compiled route.
func (cr *compiledRoute) match(req *http.Request) bool {
	// Hostname check.
	if len(cr.hostnames) > 0 {
		host := req.Host
		if idx := strings.Index(host, ":"); idx != -1 {
			host = host[:idx]
		}
		found := false
		for _, h := range cr.hostnames {
			if h == "*" || h == host {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Path check.
	path := req.URL.Path
	switch {
	case cr.pathRegex != nil:
		if !cr.pathRegex.MatchString(path) {
			return false
		}
	case cr.pathExact != "":
		if path != cr.pathExact {
			return false
		}
	case cr.pathPrefix != "":
		if !strings.HasPrefix(path, cr.pathPrefix) {
			return false
		}
	}

	// Method check.
	if cr.methods != nil && !cr.methods[req.Method] {
		return false
	}

	// gRPC check.
	if cr.grpcOnly {
		ct := req.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "application/grpc") {
			return false
		}
	}

	// Header check.
	for _, hm := range cr.headers {
		val := req.Header.Get(hm.name)
		if val == "" {
			return false
		}
		if hm.value != "" {
			if hm.regex != nil {
				if !hm.regex.MatchString(val) {
					return false
				}
			} else if val != hm.value {
				return false
			}
		}
	}

	// Query param check.
	for _, qp := range cr.queryParams {
		val := req.URL.Query().Get(qp.name)
		if val == "" {
			return false
		}
		if qp.value != "" {
			if qp.regex != nil {
				if !qp.regex.MatchString(val) {
					return false
				}
			} else if val != qp.value {
				return false
			}
		}
	}

	// CEL expression (evaluated last — most expensive check).
	if cr.celProgram != nil && !cr.celProgram.Eval(req) {
		return false
	}

	return true
}
