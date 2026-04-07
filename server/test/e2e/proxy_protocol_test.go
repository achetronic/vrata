// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"net"
	"testing"
	"time"
)

// TestE2E_ProxyProtocol_V1 verifies that a listener with proxyProtocol
// enabled correctly parses a PROXY protocol v1 header and uses the real
// client IP from the header (not the TCP peer) as r.RemoteAddr.
func TestE2E_ProxyProtocol_V1(t *testing.T) {
	port := 3110

	_, listener := apiPost(t, "/listeners", map[string]any{
		"name":    "pp-v1-listener",
		"port":    port,
		"address": "0.0.0.0",
		"proxyProtocol": map[string]any{
			"trustedCidrs": []string{"127.0.0.0/8"},
		},
		"clientIp": map[string]any{
			"source": "direct",
		},
	})
	defer apiDelete(t, "/listeners/"+id(listener))

	_, mwAuthz := apiPost(t, "/middlewares", map[string]any{
		"name": "pp-v1-authz",
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
		"name":           "pp-v1-route",
		"match":          map[string]any{"pathPrefix": "/pp-v1-test"},
		"directResponse": map[string]any{"status": 200, "body": "ok"},
		"middlewareIds":   []string{id(mwAuthz)},
	})
	defer apiDelete(t, "/routes/"+id(route))

	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)
	time.Sleep(500 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err != nil {
		t.Fatalf("connecting to proxy: %v", err)
	}
	defer conn.Close()

	ppHeader := "PROXY TCP4 203.0.113.50 127.0.0.1 12345 " + fmt.Sprintf("%d", port) + "\r\n"
	if _, err := conn.Write([]byte(ppHeader)); err != nil {
		t.Fatalf("writing PROXY protocol header: %v", err)
	}

	httpReq := "GET /pp-v1-test HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n"
	if _, err := conn.Write([]byte(httpReq)); err != nil {
		t.Fatalf("writing HTTP request: %v", err)
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil && n == 0 {
		t.Fatalf("reading response: %v", err)
	}
	resp := string(buf[:n])

	if len(resp) < 12 {
		t.Fatalf("response too short: %q", resp)
	}
	statusLine := resp[:12]
	if statusLine != "HTTP/1.1 200" {
		t.Errorf("expected HTTP/1.1 200, got %q (full response: %q)", statusLine, resp)
	}
}

// TestE2E_ProxyProtocol_Validation verifies API validation for
// proxyProtocol configuration on listeners.
func TestE2E_ProxyProtocol_Validation(t *testing.T) {
	code, _ := apiPost(t, "/listeners", map[string]any{
		"name":          "bad-pp",
		"port":          3199,
		"proxyProtocol": map[string]any{},
	})
	if code != 400 {
		t.Errorf("missing trustedCidrs: expected 400, got %d", code)
	}
}
