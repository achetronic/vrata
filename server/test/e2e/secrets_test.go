// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestE2E_SecretCRUD(t *testing.T) {
	code, result := apiPost(t, "/secrets", map[string]any{
		"name":  "e2e-test-secret",
		"value": "super-secret-value",
	})
	if code != 201 {
		t.Fatalf("create secret: %d %v", code, result)
	}
	secretID := id(result)
	defer apiDelete(t, "/secrets/"+secretID)

	if result["name"] != "e2e-test-secret" {
		t.Errorf("expected name in response, got %v", result["name"])
	}
	if result["value"] != nil {
		t.Error("create response should not include value (returns SecretSummary)")
	}

	code, body := apiGet(t, "/secrets/"+secretID)
	if code != 200 {
		t.Fatalf("get secret: %d", code)
	}
	var sec map[string]any
	json.Unmarshal(body, &sec)
	if sec["value"] != "super-secret-value" {
		t.Errorf("expected value, got %v", sec["value"])
	}

	code, body = apiGet(t, "/secrets")
	if code != 200 {
		t.Fatalf("list secrets: %d", code)
	}
	var list []map[string]any
	json.Unmarshal(body, &list)
	found := false
	for _, s := range list {
		if s["id"] == secretID {
			found = true
			if s["value"] != nil {
				t.Error("list should not include value")
			}
		}
	}
	if !found {
		t.Error("secret not in list")
	}

	code = apiDelete(t, "/secrets/"+secretID)
	if code != 204 {
		t.Errorf("delete secret: expected 204, got %d", code)
	}

	code, _ = apiGet(t, "/secrets/"+secretID)
	if code != 404 {
		t.Errorf("get deleted secret: expected 404, got %d", code)
	}
}

func TestE2E_SecretResolutionInSnapshot(t *testing.T) {
	_, secResult := apiPost(t, "/secrets", map[string]any{
		"name":  "e2e-resolve-test",
		"value": "resolved-cert-pem",
	})
	secretID := id(secResult)
	defer apiDelete(t, "/secrets/"+secretID)

	upstream := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	})
	destID := createDestination(t, "e2e-resolve-dest", upstream.host(), upstream.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "e2e-resolve-route",
		"match": map[string]any{"pathPrefix": "/e2e-resolve"},
		"forward": map[string]any{
			"destinations": []map[string]any{
				{"destinationId": destID, "weight": 100},
			},
		},
	})
	routeID := id(route)
	defer apiDelete(t, "/routes/"+routeID)

	_, group := apiPost(t, "/groups", map[string]any{
		"name":     "e2e-resolve-group",
		"routeIds": []string{routeID},
	})
	groupID := id(group)
	defer apiDelete(t, "/groups/"+groupID)

	_, listener := apiPost(t, "/listeners", map[string]any{
		"name":    "e2e-resolve-listener",
		"address": "0.0.0.0",
		"port":    17392,
		"tls": map[string]any{
			"cert": "{{secret:value:" + secretID + "}}",
			"key":  "{{secret:value:" + secretID + "}}",
		},
	})
	listenerID := id(listener)
	defer apiDelete(t, "/listeners/"+listenerID)

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, body := apiGet(t, "/snapshots/"+snapID)
	if code != 200 {
		t.Fatalf("get snapshot: %d", code)
	}

	var snap map[string]any
	json.Unmarshal(body, &snap)
	snapshot := snap["snapshot"].(map[string]any)
	listeners := snapshot["listeners"].([]any)

	found := false
	for _, l := range listeners {
		lMap := l.(map[string]any)
		if lMap["name"] == "e2e-resolve-listener" {
			found = true
			tlsMap := lMap["tls"].(map[string]any)
			if tlsMap["cert"] != "resolved-cert-pem" {
				t.Errorf("expected resolved cert, got %v", tlsMap["cert"])
			}
			if tlsMap["key"] != "resolved-cert-pem" {
				t.Errorf("expected resolved key, got %v", tlsMap["key"])
			}
		}
	}
	if !found {
		t.Error("listener not found in snapshot")
	}
}

func TestE2E_SnapshotFailsOnMissingSecret(t *testing.T) {
	_, listener := apiPost(t, "/listeners", map[string]any{
		"name":    "e2e-missing-secret-listener",
		"address": "0.0.0.0",
		"port":    17393,
		"tls": map[string]any{
			"cert": "{{secret:value:nonexistent-id}}",
			"key":  "literal-key",
		},
	})
	listenerID := id(listener)
	defer apiDelete(t, "/listeners/"+listenerID)

	code, result := apiPost(t, "/snapshots", map[string]string{
		"name": "e2e-should-fail",
	})
	if code == 201 {
		apiDelete(t, "/snapshots/"+id(result))
		t.Fatal("snapshot should have failed with unresolved secret reference")
	}
	if code != 400 {
		t.Errorf("expected 400 for unresolved secret, got %d", code)
	}
}
