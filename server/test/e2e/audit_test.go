package e2e

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
)

// TestE2E_Proxy_PathExactMatch verifies exact path matching (not prefix).
func TestE2E_Proxy_PathExactMatch(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "healthcheck",
		"match":          map[string]any{"path": "/healthz"},
		"directResponse": map[string]any{"status": 200, "body": "ok"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, body := proxyGet(t, "/healthz", nil)
	if code != 200 || body != "ok" {
		t.Errorf("/healthz: %d %q", code, body)
	}
	code, _, _ = proxyGet(t, "/healthz/extra", nil)
	if code != 404 {
		t.Errorf("/healthz/extra should 404: %d", code)
	}
}

// TestE2E_Proxy_HostRewrite verifies that host rewrite changes the Host header
// sent to the upstream.
func TestE2E_Proxy_HostRewrite(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("host=" + r.Host))
	})
	destID := createDestination(t, "backend-hostrw", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "host-rewrite",
		"match": map[string]any{"pathPrefix": "/api/rewrite-host"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
			"rewrite":      map[string]any{"host": "backend.internal.local"},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	_, _, body := proxyGet(t, "/api/rewrite-host", nil)
	if body != "host=backend.internal.local" {
		t.Errorf("expected host=backend.internal.local, got %q", body)
	}
}

// TestE2E_Proxy_MiddlewareDisablePerRoute verifies that a middleware active on
// a group can be disabled on a specific route via middlewareOverrides.
func TestE2E_Proxy_MiddlewareDisablePerRoute(t *testing.T) {
	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "headers-audit",
		"type": "headers",
		"headers": map[string]any{
			"responseHeadersToAdd": []map[string]any{{"key": "X-Audit", "value": "active"}},
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	_, routeEnabled := apiPost(t, "/routes", map[string]any{
		"name":           "audit-enabled",
		"match":          map[string]any{"pathPrefix": "/app/with-audit"},
		"directResponse": map[string]any{"status": 200, "body": "with"},
		"middlewareIds":   []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(routeEnabled))

	_, routeDisabled := apiPost(t, "/routes", map[string]any{
		"name":           "audit-disabled",
		"match":          map[string]any{"pathPrefix": "/app/no-audit"},
		"directResponse": map[string]any{"status": 200, "body": "without"},
		"middlewareIds":   []string{id(mw)},
		"middlewareOverrides": map[string]any{
			id(mw): map[string]any{"disabled": true},
		},
	})
	defer apiDelete(t, "/routes/"+id(routeDisabled))

	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	_, hdrs, _ := proxyGet(t, "/app/with-audit", nil)
	if hdrs.Get("X-Audit") != "active" {
		t.Errorf("expected X-Audit=active, got %q", hdrs.Get("X-Audit"))
	}

	_, hdrs, _ = proxyGet(t, "/app/no-audit", nil)
	if hdrs.Get("X-Audit") != "" {
		t.Errorf("expected no X-Audit, got %q", hdrs.Get("X-Audit"))
	}
}

// TestE2E_Endpoint_LeastRequest verifies LEAST_REQUEST distributes across
// endpoints. Under concurrent load, endpoints with fewer inflight requests
// are preferred.
func TestE2E_Endpoint_LeastRequest(t *testing.T) {
	ups := startLabeledUpstreams(t, 3)
	destID := createMultiEndpointDest(t, "ep-lr", ups, map[string]any{
		"algorithm": "LEAST_REQUEST",
	})
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "ep-lr",
		"match": map[string]any{"pathPrefix": "/ep-lr"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	var mu sync.Mutex
	counts := map[string]int{}
	const total = 6000
	const workers = 10
	perWorker := total / workers
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				_, _, body := proxyGet(t, "/ep-lr", nil)
				mu.Lock()
				counts[body]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	seen := 0
	for _, u := range ups {
		if counts[u.label] > 0 {
			seen++
		}
		t.Logf("%s: %d (%.1f%%)", u.label, counts[u.label], float64(counts[u.label])*100/float64(total))
	}
	if seen < 2 {
		t.Errorf("least request should use multiple endpoints under concurrency, only saw %d", seen)
	}
}

// TestE2E_API_DestinationWithEndpoints verifies CRUD for destinations
// with static endpoint lists.
func TestE2E_API_DestinationWithEndpoints(t *testing.T) {
	_, dest := apiPost(t, "/destinations", map[string]any{
		"name": "multi-ep",
		"host": "fallback.local",
		"port": 9999,
		"endpoints": []map[string]any{
			{"host": "10.0.0.1", "port": 8080},
			{"host": "10.0.0.2", "port": 8080},
		},
	})
	defer apiDelete(t, "/destinations/"+id(dest))

	if dest["endpoints"] == nil {
		t.Fatal("endpoints not returned on create")
	}
	eps, ok := dest["endpoints"].([]any)
	if !ok || len(eps) != 2 {
		t.Fatalf("expected 2 endpoints, got %v", dest["endpoints"])
	}

	code, body := apiGet(t, "/destinations/"+id(dest))
	if code != 200 {
		t.Fatalf("get: %d", code)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	gotEps, _ := got["endpoints"].([]any)
	if len(gotEps) != 2 {
		t.Errorf("expected 2 endpoints on get, got %d", len(gotEps))
	}
}
