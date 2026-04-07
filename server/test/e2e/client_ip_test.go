// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"testing"
	"time"
)

// TestE2E_ClientIP_Direct verifies that clientIp on a listener with source
// "direct" ignores X-Forwarded-For and uses the TCP peer address. The
// resolved IP is available in CEL expressions at both route-matching and
// middleware (inlineAuthz) time.
func TestE2E_ClientIP_Direct(t *testing.T) {
	port := 3100

	_, listener := apiPost(t, "/listeners", map[string]any{
		"name":    "clientip-direct-listener",
		"port":    port,
		"address": "0.0.0.0",
		"clientIp": map[string]any{
			"source": "direct",
		},
	})
	defer apiDelete(t, "/listeners/"+id(listener))

	_, mwAuthz := apiPost(t, "/middlewares", map[string]any{
		"name": "clientip-direct-authz",
		"type": "inlineAuthz",
		"inlineAuthz": map[string]any{
			"rules": []map[string]any{
				{"cel": `request.clientIp == "127.0.0.1"`, "action": "allow"},
			},
			"defaultAction": "deny",
			"denyStatus":    403,
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mwAuthz))

	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "clientip-direct-route",
		"match":          map[string]any{"pathPrefix": "/clientip-direct-test"},
		"directResponse": map[string]any{"status": 200, "body": "ok"},
		"middlewareIds":   []string{id(mwAuthz)},
	})
	defer apiDelete(t, "/routes/"+id(route))

	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)
	time.Sleep(300 * time.Millisecond)

	code, _, _ := proxyGetPort(t, port, "/clientip-direct-test", map[string]string{
		"X-Forwarded-For": "203.0.113.50",
	})
	if code != 200 {
		t.Errorf("direct mode: expected 200, got %d", code)
	}
}

// TestE2E_ClientIP_XFF_TrustedCidrs verifies that clientIp on a listener
// with source "xff" + trustedCidrs correctly resolves the client IP.
func TestE2E_ClientIP_XFF_TrustedCidrs(t *testing.T) {
	port := 3101

	_, listener := apiPost(t, "/listeners", map[string]any{
		"name":    "clientip-xff-cidrs-listener",
		"port":    port,
		"address": "0.0.0.0",
		"clientIp": map[string]any{
			"source":       "xff",
			"trustedCidrs": []string{"127.0.0.0/8", "10.0.0.0/8"},
		},
	})
	defer apiDelete(t, "/listeners/"+id(listener))

	_, mwAuthz := apiPost(t, "/middlewares", map[string]any{
		"name": "clientip-xff-cidrs-authz",
		"type": "inlineAuthz",
		"inlineAuthz": map[string]any{
			"rules": []map[string]any{
				{"cel": `request.clientIp == "203.0.113.50"`, "action": "allow"},
			},
			"defaultAction": "deny",
			"denyStatus":    403,
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mwAuthz))

	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "clientip-xff-cidrs-route",
		"match":          map[string]any{"pathPrefix": "/clientip-xff-cidrs-test"},
		"directResponse": map[string]any{"status": 200, "body": "ok"},
		"middlewareIds":   []string{id(mwAuthz)},
	})
	defer apiDelete(t, "/routes/"+id(route))

	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)
	time.Sleep(300 * time.Millisecond)

	code, _, _ := proxyGetPort(t, port, "/clientip-xff-cidrs-test", map[string]string{
		"X-Forwarded-For": "203.0.113.50, 10.0.0.1",
	})
	if code != 200 {
		t.Errorf("xff+cidrs: expected 200, got %d", code)
	}

	code, _, _ = proxyGetPort(t, port, "/clientip-xff-cidrs-test", map[string]string{
		"X-Forwarded-For": "198.51.100.7, 10.0.0.1",
	})
	if code != 403 {
		t.Errorf("xff+cidrs mismatch: expected 403, got %d", code)
	}
}

// TestE2E_ClientIP_XFF_NumTrustedHops verifies that clientIp on a listener
// with source "xff" + numTrustedHops correctly counts hops from the right.
func TestE2E_ClientIP_XFF_NumTrustedHops(t *testing.T) {
	port := 3102

	_, listener := apiPost(t, "/listeners", map[string]any{
		"name":    "clientip-xff-hops-listener",
		"port":    port,
		"address": "0.0.0.0",
		"clientIp": map[string]any{
			"source":         "xff",
			"numTrustedHops": 2,
		},
	})
	defer apiDelete(t, "/listeners/"+id(listener))

	_, mwAuthz := apiPost(t, "/middlewares", map[string]any{
		"name": "clientip-xff-hops-authz",
		"type": "inlineAuthz",
		"inlineAuthz": map[string]any{
			"rules": []map[string]any{
				{"cel": `request.clientIp == "203.0.113.50"`, "action": "allow"},
			},
			"defaultAction": "deny",
			"denyStatus":    403,
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mwAuthz))

	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "clientip-xff-hops-route",
		"match":          map[string]any{"pathPrefix": "/clientip-xff-hops-test"},
		"directResponse": map[string]any{"status": 200, "body": "ok"},
		"middlewareIds":   []string{id(mwAuthz)},
	})
	defer apiDelete(t, "/routes/"+id(route))

	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)
	time.Sleep(300 * time.Millisecond)

	code, _, _ := proxyGetPort(t, port, "/clientip-xff-hops-test", map[string]string{
		"X-Forwarded-For": "203.0.113.50, 10.0.0.5, 10.0.0.1",
	})
	if code != 200 {
		t.Errorf("xff+hops: expected 200, got %d", code)
	}
}

// TestE2E_ClientIP_Header verifies that clientIp on a listener with source
// "header" reads the client IP from a custom header.
func TestE2E_ClientIP_Header(t *testing.T) {
	port := 3103

	_, listener := apiPost(t, "/listeners", map[string]any{
		"name":    "clientip-header-listener",
		"port":    port,
		"address": "0.0.0.0",
		"clientIp": map[string]any{
			"source": "header",
			"header": "CF-Connecting-IP",
		},
	})
	defer apiDelete(t, "/listeners/"+id(listener))

	_, mwAuthz := apiPost(t, "/middlewares", map[string]any{
		"name": "clientip-header-authz",
		"type": "inlineAuthz",
		"inlineAuthz": map[string]any{
			"rules": []map[string]any{
				{"cel": `request.clientIp == "198.51.100.99"`, "action": "allow"},
			},
			"defaultAction": "deny",
			"denyStatus":    403,
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mwAuthz))

	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "clientip-header-route",
		"match":          map[string]any{"pathPrefix": "/clientip-header-test"},
		"directResponse": map[string]any{"status": 200, "body": "ok"},
		"middlewareIds":   []string{id(mwAuthz)},
	})
	defer apiDelete(t, "/routes/"+id(route))

	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)
	time.Sleep(300 * time.Millisecond)

	code, _, _ := proxyGetPort(t, port, "/clientip-header-test", map[string]string{
		"CF-Connecting-IP": "198.51.100.99",
	})
	if code != 200 {
		t.Errorf("header mode: expected 200, got %d", code)
	}

	code, _, _ = proxyGetPort(t, port, "/clientip-header-test", nil)
	if code != 403 {
		t.Errorf("header missing: expected 403, got %d", code)
	}
}

// TestE2E_ClientIP_CEL_RouteMatch verifies that the resolved client IP
// is available in route-matching CEL expressions (match.cel), not just
// in middleware-phase CEL.
func TestE2E_ClientIP_CEL_RouteMatch(t *testing.T) {
	port := 3104

	_, listener := apiPost(t, "/listeners", map[string]any{
		"name":    "clientip-cel-match-listener",
		"port":    port,
		"address": "0.0.0.0",
		"clientIp": map[string]any{
			"source": "direct",
		},
	})
	defer apiDelete(t, "/listeners/"+id(listener))

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "clientip-cel-match-route",
		"match": map[string]any{
			"pathPrefix": "/clientip-cel-match-test",
			"cel":        `request.clientIp == "127.0.0.1"`,
		},
		"directResponse": map[string]any{"status": 200, "body": "matched"},
	})
	defer apiDelete(t, "/routes/"+id(route))

	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)
	time.Sleep(300 * time.Millisecond)

	code, _, body := proxyGetPort(t, port, "/clientip-cel-match-test", map[string]string{
		"X-Forwarded-For": "203.0.113.50",
	})
	if code != 200 {
		t.Errorf("CEL route match: expected 200, got %d (body: %s)", code, body)
	}
}

// TestE2E_ClientIP_Validation verifies API validation for the clientIp
// listener configuration.
func TestE2E_ClientIP_Validation(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
		want int
	}{
		{
			name: "missing source",
			body: map[string]any{
				"name":     "bad-clientip",
				"port":     3200,
				"clientIp": map[string]any{},
			},
			want: 400,
		},
		{
			name: "header source without header field",
			body: map[string]any{
				"name": "bad-clientip-header",
				"port": 3201,
				"clientIp": map[string]any{
					"source": "header",
				},
			},
			want: 400,
		},
		{
			name: "mutually exclusive cidrs and hops",
			body: map[string]any{
				"name": "bad-clientip-both",
				"port": 3202,
				"clientIp": map[string]any{
					"source":         "xff",
					"trustedCidrs":   []string{"10.0.0.0/8"},
					"numTrustedHops": 1,
				},
			},
			want: 400,
		},
		{
			name: "invalid source value",
			body: map[string]any{
				"name": "bad-clientip-source",
				"port": 3203,
				"clientIp": map[string]any{
					"source": "magic",
				},
			},
			want: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, result := apiPost(t, "/listeners", tt.body)
			if code != tt.want {
				t.Errorf("expected %d, got %d: %v", tt.want, code, result)
			}
		})
	}
}

// TestE2E_ClientIP_HotReload verifies that changing clientIp config on a
// listener does not restart it (no port release/rebind), but the new
// resolver takes effect immediately.
func TestE2E_ClientIP_HotReload(t *testing.T) {
	port := 3105

	_, listener := apiPost(t, "/listeners", map[string]any{
		"name":    "clientip-hotreload-listener",
		"port":    port,
		"address": "0.0.0.0",
		"clientIp": map[string]any{
			"source": "direct",
		},
	})
	listenerID := id(listener)
	defer apiDelete(t, "/listeners/"+listenerID)

	_, mwAuthz := apiPost(t, "/middlewares", map[string]any{
		"name": "clientip-hotreload-authz",
		"type": "inlineAuthz",
		"inlineAuthz": map[string]any{
			"rules": []map[string]any{
				{"cel": `request.clientIp == "203.0.113.50"`, "action": "allow"},
			},
			"defaultAction": "deny",
			"denyStatus":    403,
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mwAuthz))

	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "clientip-hotreload-route",
		"match":          map[string]any{"pathPrefix": "/clientip-hotreload-test"},
		"directResponse": map[string]any{"status": 200, "body": "ok"},
		"middlewareIds":   []string{id(mwAuthz)},
	})
	defer apiDelete(t, "/routes/"+id(route))

	snap1 := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap1)
	time.Sleep(300 * time.Millisecond)

	// With "direct" mode, request.clientIp == "127.0.0.1" → denied (authz wants 203.0.113.50).
	code, _, _ := proxyGetPort(t, port, "/clientip-hotreload-test", map[string]string{
		"X-Forwarded-For": "203.0.113.50, 10.0.0.1",
	})
	if code != 403 {
		t.Fatalf("before hot-reload: expected 403, got %d", code)
	}

	// Hot-reload: change to XFF with trusted CIDRs → resolves 203.0.113.50 → allowed.
	apiPut(t, fmt.Sprintf("/listeners/%s", listenerID), map[string]any{
		"name":    "clientip-hotreload-listener",
		"port":    port,
		"address": "0.0.0.0",
		"clientIp": map[string]any{
			"source":       "xff",
			"trustedCidrs": []string{"127.0.0.0/8", "10.0.0.0/8"},
		},
	})

	snap2 := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap2)
	time.Sleep(300 * time.Millisecond)

	code, _, _ = proxyGetPort(t, port, "/clientip-hotreload-test", map[string]string{
		"X-Forwarded-For": "203.0.113.50, 10.0.0.1",
	})
	if code != 200 {
		t.Errorf("after hot-reload: expected 200, got %d", code)
	}
}
