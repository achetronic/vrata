// Package proxy implements a programmable HTTP reverse proxy for Rutoso.
// Rutoso is a programmable reverse proxy. Configuration is applied in real
// time via atomic swap of the routing table — no restarts, no file generation.
package proxy

import (
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/achetronic/rutoso/internal/model"
)

// Router holds the current routing table and dispatches incoming requests
// to the appropriate handler. The table is swapped atomically on config reload.
type Router struct {
	table atomic.Pointer[RoutingTable]
}

// RoutingTable is an immutable snapshot of the current proxy configuration.
type RoutingTable struct {
	routes       []compiledRoute
	destinations map[string]*Upstream
	middlewares  map[string]model.Middleware
}

// compiledRoute is a pre-compiled route ready for matching.
type compiledRoute struct {
	model       model.Route
	group       *model.RouteGroup
	pathRegex   *regexp.Regexp // nil if not regex match
	pathPrefix  string
	pathExact   string
	methods     map[string]bool // nil = match all
	headers     []model.HeaderMatcher
	queryParams []model.QueryParamMatcher
	grpcOnly    bool
	hostnames   []string
	handler     http.Handler
}

// NewRouter creates a new Router with an empty routing table.
func NewRouter() *Router {
	r := &Router{}
	r.table.Store(&RoutingTable{
		destinations: make(map[string]*Upstream),
		middlewares:  make(map[string]model.Middleware),
	})
	return r
}

// SwapTable atomically replaces the routing table. Active requests on the
// old table are not affected — they complete with the old config.
func (r *Router) SwapTable(t *RoutingTable) {
	r.table.Store(t)
}

// ServeHTTP dispatches the request to the matching route.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	t := r.table.Load()

	for i := range t.routes {
		cr := &t.routes[i]
		if cr.match(req) {
			cr.handler.ServeHTTP(w, req)
			return
		}
	}

	http.Error(w, "no matching route", http.StatusNotFound)
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
		val := req.Header.Get(hm.Name)
		if val == "" {
			return false
		}
		if hm.Value != "" {
			if hm.Regex {
				re, err := regexp.Compile(hm.Value)
				if err != nil || !re.MatchString(val) {
					return false
				}
			} else if val != hm.Value {
				return false
			}
		}
	}

	// Query param check.
	for _, qp := range cr.queryParams {
		val := req.URL.Query().Get(qp.Name)
		if val == "" {
			return false
		}
		if qp.Value != "" {
			if qp.Regex {
				re, err := regexp.Compile(qp.Value)
				if err != nil || !re.MatchString(val) {
					return false
				}
			} else if val != qp.Value {
				return false
			}
		}
	}

	return true
}
