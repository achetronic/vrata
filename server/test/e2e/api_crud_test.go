// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"encoding/json"
	"testing"
)

func TestE2E_API_DestinationCRUD(t *testing.T) {
	code, created := apiPost(t, "/destinations", map[string]any{"name": "e2e-dest", "host": "127.0.0.1", "port": 9999})
	if code != 201 {
		t.Fatalf("create: %d", code)
	}
	defer apiDelete(t, "/destinations/"+id(created))

	code, _ = apiGet(t, "/destinations/"+id(created))
	if code != 200 {
		t.Errorf("get: %d", code)
	}

	code, updated := apiPut(t, "/destinations/"+id(created), map[string]any{"name": "e2e-dest-updated", "host": "127.0.0.1", "port": 8888})
	if code != 200 || updated["name"] != "e2e-dest-updated" {
		t.Errorf("update: %d", code)
	}

	if apiDelete(t, "/destinations/"+id(created)) != 204 {
		t.Error("delete failed")
	}
	if c, _ := apiGet(t, "/destinations/"+id(created)); c != 404 {
		t.Errorf("get after delete: %d", c)
	}
}

func TestE2E_API_ListenerCRUD(t *testing.T) {
	code, created := apiPost(t, "/listeners", map[string]any{"name": "e2e-listener", "port": 19999})
	if code != 201 {
		t.Fatalf("create: %d", code)
	}
	defer apiDelete(t, "/listeners/"+id(created))
	if created["address"] != "0.0.0.0" {
		t.Errorf("expected default address")
	}
	apiDelete(t, "/listeners/"+id(created))
}

func TestE2E_API_RouteCRUD(t *testing.T) {
	code, created := apiPost(t, "/routes", map[string]any{
		"name": "e2e-route", "match": map[string]any{"pathPrefix": "/e2e-test"},
		"directResponse": map[string]any{"status": 200, "body": "e2e-ok"},
	})
	if code != 201 {
		t.Fatalf("create: %d", code)
	}
	defer apiDelete(t, "/routes/"+id(created))
	apiDelete(t, "/routes/"+id(created))
}

func TestE2E_API_GroupCRUD(t *testing.T) {
	code, created := apiPost(t, "/groups", map[string]any{"name": "e2e-group", "pathPrefix": "/e2e"})
	if code != 201 {
		t.Fatalf("create: %d", code)
	}
	defer apiDelete(t, "/groups/"+id(created))
	apiDelete(t, "/groups/"+id(created))
}

func TestE2E_API_MiddlewareCRUD(t *testing.T) {
	code, created := apiPost(t, "/middlewares", map[string]any{
		"name": "e2e-headers", "type": "headers",
		"headers": map[string]any{"requestHeadersToAdd": []map[string]any{{"key": "X-E2E", "value": "true"}}},
	})
	if code != 201 {
		t.Fatalf("create: %d", code)
	}
	defer apiDelete(t, "/middlewares/"+id(created))
	apiDelete(t, "/middlewares/"+id(created))
}

func TestE2E_API_ConfigDump(t *testing.T) {
	code, body := apiGet(t, "/debug/config")
	if code != 200 {
		t.Fatalf("config dump: %d", code)
	}
	var dump map[string]json.RawMessage
	json.Unmarshal(body, &dump)
	for _, key := range []string{"listeners", "routes", "groups", "destinations", "middlewares"} {
		if _, ok := dump[key]; !ok {
			t.Errorf("missing %q", key)
		}
	}
}
