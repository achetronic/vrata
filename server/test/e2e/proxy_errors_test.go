// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"encoding/json"
	"testing"
)

// TestE2E_ProxyErrors_DefaultJSON verifies that requesting a non-existent
// path returns a structured JSON error with at least "error" and "status".
func TestE2E_ProxyErrors_DefaultJSON(t *testing.T) {
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, headers, body := proxyGet(t, "/nonexistent-path-e2e-proxy-errors", nil)
	if code != 404 {
		t.Fatalf("expected 404, got %d", code)
	}
	if ct := headers.Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("invalid JSON body: %s", body)
	}
	if result["error"] != "no_route" {
		t.Errorf("expected error=no_route, got %v", result["error"])
	}
	if result["status"] == nil {
		t.Error("expected status field in response")
	}
	if result["message"] == nil {
		t.Error("expected message field in standard detail")
	}
	if result["destination"] != nil {
		t.Error("standard detail should not include destination")
	}
}

// TestE2E_ProxyErrors_ConnectionRefused verifies that forwarding to a dead
// upstream returns a structured JSON error with the classified error type.
func TestE2E_ProxyErrors_ConnectionRefused(t *testing.T) {
	deadDestID := createDestination(t, "proxy-errors-dead", "127.0.0.1", 19877)
	defer apiDelete(t, "/destinations/"+deadDestID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":    "proxy-errors-connrefused",
		"match":   map[string]any{"pathPrefix": "/e2e-proxy-errors-connref"},
		"forward": map[string]any{"destinations": []map[string]any{{"destinationId": deadDestID, "weight": 100}}},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyGet(t, "/e2e-proxy-errors-connref", nil)
	if code != 502 {
		t.Fatalf("expected 502, got %d", code)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("invalid JSON: %s", body)
	}
	if result["error"] != "connection_refused" {
		t.Errorf("expected error=connection_refused, got %v", result["error"])
	}
	if result["message"] == nil {
		t.Error("expected message in standard detail")
	}
}

// TestE2E_ProxyErrors_FullDetail creates a listener with proxyErrors.detail=full
// and verifies the error response includes destination, endpoint, and timestamp.
func TestE2E_ProxyErrors_FullDetail(t *testing.T) {
	const fullPort = 19878

	_, listener := apiPost(t, "/listeners", map[string]any{
		"name":    "proxy-errors-full",
		"address": "0.0.0.0",
		"port":    fullPort,
		"proxyErrors": map[string]any{
			"detail": "full",
		},
	})
	defer apiDelete(t, "/listeners/"+id(listener))

	deadDestID := createDestination(t, "proxy-errors-full-dead", "127.0.0.1", 19879)
	defer apiDelete(t, "/destinations/"+deadDestID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":    "proxy-errors-full-route",
		"match":   map[string]any{"pathPrefix": "/e2e-proxy-errors-full"},
		"forward": map[string]any{"destinations": []map[string]any{{"destinationId": deadDestID, "weight": 100}}},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyGetPort(t, fullPort, "/e2e-proxy-errors-full", nil)
	if code != 502 {
		t.Fatalf("expected 502, got %d", code)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("invalid JSON: %s", body)
	}
	if result["error"] != "connection_refused" {
		t.Errorf("expected error=connection_refused, got %v", result["error"])
	}
	if result["destination"] == nil {
		t.Error("full detail should include destination")
	}
	if result["timestamp"] == nil {
		t.Error("full detail should include timestamp")
	}
}

// TestE2E_ProxyErrors_MinimalDetail creates a listener with
// proxyErrors.detail=minimal and verifies the error response includes
// only error and status.
func TestE2E_ProxyErrors_MinimalDetail(t *testing.T) {
	const minPort = 19880

	_, listener := apiPost(t, "/listeners", map[string]any{
		"name":    "proxy-errors-minimal",
		"address": "0.0.0.0",
		"port":    minPort,
		"proxyErrors": map[string]any{
			"detail": "minimal",
		},
	})
	defer apiDelete(t, "/listeners/"+id(listener))

	deadDestID := createDestination(t, "proxy-errors-min-dead", "127.0.0.1", 19881)
	defer apiDelete(t, "/destinations/"+deadDestID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":    "proxy-errors-min-route",
		"match":   map[string]any{"pathPrefix": "/e2e-proxy-errors-min"},
		"forward": map[string]any{"destinations": []map[string]any{{"destinationId": deadDestID, "weight": 100}}},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyGetPort(t, minPort, "/e2e-proxy-errors-min", nil)
	if code != 502 {
		t.Fatalf("expected 502, got %d", code)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("invalid JSON: %s", body)
	}
	if result["error"] != "connection_refused" {
		t.Errorf("expected error=connection_refused, got %v", result["error"])
	}
	if result["status"] == nil {
		t.Error("minimal should include status")
	}
	if result["message"] != nil {
		t.Error("minimal should not include message")
	}
	if result["destination"] != nil {
		t.Error("minimal should not include destination")
	}
}
