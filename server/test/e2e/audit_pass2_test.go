// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestE2E_TimeoutFallback_RouteOverridesDestination verifies the request
// timeout fallback: route-level timeout takes precedence over destination-level.
func TestE2E_TimeoutFallback_RouteOverridesDestination(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(800 * time.Millisecond)
		w.Write([]byte("slow"))
	})

	destID := createDestination(t, "tf-dest", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)
	apiPut(t, "/destinations/"+destID, map[string]any{
		"name": "tf-dest", "host": up.host(), "port": up.port(),
		"options": map[string]any{
			"timeouts": map[string]any{
				"request": "5s",
			},
		},
	})

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "tf-route",
		"match": map[string]any{"pathPrefix": "/tf-test"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
			"timeouts":     map[string]any{"request": "200ms"},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, body := proxyGet(t, "/tf-test", nil)
	if code != 504 {
		t.Errorf("route timeout should override dest timeout: expected 504, got %d; body=%q", code, body)
	}
}

// TestE2E_TimeoutFallback_DestinationUsedWhenRouteUnset verifies that when no
// route-level timeout is set, the destination-level timeout applies.
func TestE2E_TimeoutFallback_DestinationUsedWhenRouteUnset(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(800 * time.Millisecond)
		w.Write([]byte("slow"))
	})

	destID := createDestination(t, "tfd-dest", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)
	apiPut(t, "/destinations/"+destID, map[string]any{
		"name": "tfd-dest", "host": up.host(), "port": up.port(),
		"options": map[string]any{
			"timeouts": map[string]any{
				"request": "200ms",
			},
		},
	})

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "tfd-route",
		"match": map[string]any{"pathPrefix": "/tfd-test"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, body := proxyGet(t, "/tfd-test", nil)
	if code != 504 {
		t.Errorf("dest timeout should apply when route unset: expected 504, got %d; body=%q", code, body)
	}
}

// TestE2E_ExtProc_PerRoutePhaseOverride verifies that a per-route middleware
// override can change ExtProc phases. The base middleware processes
// requestHeaders (injecting X-Processed). The override skips requestHeaders,
// so the header must NOT be injected on the overridden route.
func TestE2E_ExtProc_PerRoutePhaseOverride(t *testing.T) {
	procSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		phase, _ := req["phase"].(string)
		if phase == "requestHeaders" {
			json.NewEncoder(w).Encode(map[string]any{
				"action":     "requestHeaders",
				"status":     "continue",
				"setHeaders": []map[string]string{{"key": "x-phase-injected", "value": "yes"}},
			})
		} else {
			json.NewEncoder(w).Encode(map[string]any{"action": phase, "status": "continue"})
		}
	}))
	defer procSrv.Close()

	procAddr := procSrv.Listener.Addr().(*net.TCPAddr)
	procDestID := createDestination(t, "ep-override-proc", procAddr.IP.String(), procAddr.Port)
	defer apiDelete(t, "/destinations/"+procDestID)

	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "ep-override-mw", "type": "extProc",
		"extProc": map[string]any{
			"destinationId": procDestID,
			"mode":          "http",
			"phaseTimeout":  "2s",
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))
	mwID := id(mw)

	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("phase-hdr=" + r.Header.Get("X-Phase-Injected")))
	})
	upDestID := createDestination(t, "ep-override-up", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+upDestID)

	_, routeNormal := apiPost(t, "/routes", map[string]any{
		"name":  "ep-override-normal",
		"match": map[string]any{"pathPrefix": "/ep-normal"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": upDestID, "weight": 100}},
		},
		"middlewareIds": []string{mwID},
	})
	defer apiDelete(t, "/routes/"+id(routeNormal))

	_, routeSkipped := apiPost(t, "/routes", map[string]any{
		"name":  "ep-override-skipped",
		"match": map[string]any{"pathPrefix": "/ep-skipped"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": upDestID, "weight": 100}},
		},
		"middlewareIds": []string{mwID},
		"middlewareOverrides": map[string]any{
			mwID: map[string]any{
				"extProc": map[string]any{
					"phases": map[string]any{
						"requestHeaders": "skip",
					},
				},
			},
		},
	})
	defer apiDelete(t, "/routes/"+id(routeSkipped))

	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, body := proxyGet(t, "/ep-normal", nil)
	if code != 200 {
		t.Fatalf("normal route: expected 200, got %d", code)
	}
	if !strings.Contains(body, "phase-hdr=yes") {
		t.Errorf("normal route should have injected header: %q", body)
	}

	code, _, body = proxyGet(t, "/ep-skipped", nil)
	if code != 200 {
		t.Fatalf("skipped route: expected 200, got %d", code)
	}
	if strings.Contains(body, "phase-hdr=yes") {
		t.Errorf("skipped route should NOT have injected header (phase was skipped): %q", body)
	}
}

// TestE2E_ExtProc_PerRouteAllowOnErrorOverride verifies that a per-route
// override can change allowOnError. Base ExtProc has allowOnError=false.
// The override sets allowOnError=true, so when the processor is unreachable
// the request passes through on the overridden route but fails on the normal one.
func TestE2E_ExtProc_PerRouteAllowOnErrorOverride(t *testing.T) {
	procDestID := createDestination(t, "ep-aoe-proc", "127.0.0.1", 1)
	defer apiDelete(t, "/destinations/"+procDestID)

	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "ep-aoe-mw", "type": "extProc",
		"extProc": map[string]any{
			"destinationId": procDestID,
			"mode":          "http",
			"phaseTimeout":  "200ms",
			"allowOnError":  false,
			"statusOnError": 503,
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))
	mwID := id(mw)

	_, routeStrict := apiPost(t, "/routes", map[string]any{
		"name":           "ep-aoe-strict",
		"match":          map[string]any{"pathPrefix": "/ep-aoe-strict"},
		"directResponse": map[string]any{"status": 200, "body": "ok-strict"},
		"middlewareIds":   []string{mwID},
	})
	defer apiDelete(t, "/routes/"+id(routeStrict))

	_, routePermissive := apiPost(t, "/routes", map[string]any{
		"name":           "ep-aoe-permissive",
		"match":          map[string]any{"pathPrefix": "/ep-aoe-permissive"},
		"directResponse": map[string]any{"status": 200, "body": "ok-permissive"},
		"middlewareIds":   []string{mwID},
		"middlewareOverrides": map[string]any{
			mwID: map[string]any{
				"extProc": map[string]any{
					"allowOnError": true,
				},
			},
		},
	})
	defer apiDelete(t, "/routes/"+id(routePermissive))

	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, _ := proxyGet(t, "/ep-aoe-strict", nil)
	if code != 503 {
		t.Errorf("strict route should fail (503) when processor unreachable: got %d", code)
	}

	code, _, body := proxyGet(t, "/ep-aoe-permissive", nil)
	if code != 200 {
		t.Errorf("permissive route should pass (200) with allowOnError override: got %d; body=%q", code, body)
	}
	if body != "ok-permissive" {
		t.Errorf("permissive body: expected 'ok-permissive', got %q", body)
	}
}

// TestE2E_CircuitBreaker_MaxPendingRequests verifies that when concurrent
// requests exceed maxConnections, excess requests beyond maxPendingRequests
// get rejected with 503.
func TestE2E_CircuitBreaker_MaxPendingRequests(t *testing.T) {
	var activeConns atomic.Int64
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		activeConns.Add(1)
		defer activeConns.Add(-1)
		time.Sleep(500 * time.Millisecond)
		w.Write([]byte("ok"))
	})

	destID := createDestination(t, "cb-mp-dest", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)
	apiPut(t, "/destinations/"+destID, map[string]any{
		"name": "cb-mp-dest", "host": up.host(), "port": up.port(),
		"options": map[string]any{
			"circuitBreaker": map[string]any{
				"failureThreshold":   100,
				"openDuration":       "30s",
				"maxConnections":     2,
				"maxPendingRequests": 1,
			},
		},
	})

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "cb-mp-route",
		"match": map[string]any{"pathPrefix": "/cb-mp-test"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	var wg sync.WaitGroup
	results := make([]int, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Get(proxyURL + "/cb-mp-test")
			if err != nil {
				results[idx] = 0
				return
			}
			resp.Body.Close()
			results[idx] = resp.StatusCode
		}(i)
	}
	wg.Wait()

	var got200, got503 int
	for _, code := range results {
		switch code {
		case 200:
			got200++
		case 503:
			got503++
		}
	}

	if got503 == 0 {
		t.Errorf("expected some 503s from maxPendingRequests overflow, got %d×200 %d×503", got200, got503)
	}
	t.Logf("maxPendingRequests results: 200=%d, 503=%d", got200, got503)
}

// TestE2E_ListenerMetrics_Connections verifies that listener-level Prometheus
// metrics for connections are emitted when enabled.
func TestE2E_ListenerMetrics_Connections(t *testing.T) {
	code, result := apiPost(t, "/listeners", map[string]any{
		"name":    "metrics-conn-listener",
		"address": "0.0.0.0",
		"port":    3005,
		"metrics": map[string]any{
			"path": "/metrics",
			"collect": map[string]any{
				"route":       true,
				"destination": true,
				"listener":    true,
			},
		},
	})
	if code != 201 {
		t.Fatalf("create listener: %d %v", code, result)
	}
	listenerID := id(result)
	defer apiDelete(t, "/listeners/"+listenerID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "metrics-conn-route",
		"match":          map[string]any{"pathPrefix": "/metrics-conn"},
		"directResponse": map[string]any{"status": 200, "body": "ok"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	time.Sleep(500 * time.Millisecond)

	for i := 0; i < 3; i++ {
		proxyGetPort(t, 3005, "/metrics-conn", nil)
	}

	metricsCode, _, metricsBody := proxyGetPort(t, 3005, "/metrics", nil)
	if metricsCode != 200 {
		t.Fatalf("metrics endpoint: %d", metricsCode)
	}

	if !strings.Contains(metricsBody, "vrata_listener_connections_total") {
		t.Errorf("missing vrata_listener_connections_total metric in:\n%s", truncate(metricsBody, 2000))
	}
}

// TestE2E_RouteActionValidation_E2E verifies that the API rejects routes with
// conflicting actions (both forward + directResponse).
func TestE2E_RouteActionValidation_E2E(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	destID := createDestination(t, "rav-dest", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	code, result := apiPost(t, "/routes", map[string]any{
		"name":  "rav-both",
		"match": map[string]any{"pathPrefix": "/rav-both"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
		},
		"directResponse": map[string]any{"status": 200, "body": "nope"},
	})
	if code == 201 {
		apiDelete(t, "/routes/"+id(result))
		t.Error("API should reject route with both forward + directResponse, but accepted it")
	}
	if code != 400 && code != 409 && code != 422 {
		t.Errorf("expected 400/409/422 for conflicting actions, got %d", code)
	}

	code, result = apiPost(t, "/routes", map[string]any{
		"name":  "rav-none",
		"match": map[string]any{"pathPrefix": "/rav-none"},
	})
	if code == 201 {
		apiDelete(t, "/routes/"+id(result))
		t.Error("API should reject route with no action, but accepted it")
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("... (%d more bytes)", len(s)-n)
}
