package e2e

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestE2E_OnError_DefaultJSONResponse(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name":           "onerror-default",
		"match":          map[string]any{"pathPrefix": "/e2e-onerror-default"},
		"directResponse": map[string]any{"status": 200, "body": "ok"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, headers, body := proxyGet(t, "/e2e-nonexistent-path-xyz", nil)
	if code != 404 {
		t.Errorf("expected 404, got %d", code)
	}
	if ct := headers.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("expected JSON content-type, got %q", ct)
	}
	var errBody map[string]string
	if err := json.Unmarshal([]byte(body), &errBody); err != nil {
		t.Fatalf("expected JSON body, got %q", body)
	}
	if errBody["error"] == "" {
		t.Error("expected error field in JSON body")
	}
}

func TestE2E_OnError_ConnectionRefused_DirectResponse(t *testing.T) {
	destID := createDestination(t, "onerror-dead", "127.0.0.1", 19876)
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "onerror-connref",
		"match": map[string]any{"pathPrefix": "/e2e-onerror-connref"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
		},
		"onError": []map[string]any{
			{
				"on":             []string{"connection_refused"},
				"directResponse": map[string]any{"status": 503, "body": `{"fallback":"connection_refused"}`},
			},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyGet(t, "/e2e-onerror-connref", nil)
	if code != 503 {
		t.Errorf("expected 503, got %d", code)
	}
	var result map[string]string
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("expected JSON, got %q", body)
	}
	if result["fallback"] != "connection_refused" {
		t.Errorf("expected connection_refused fallback, got %v", result)
	}
}

func TestE2E_OnError_ConnectionRefused_ForwardToFallback(t *testing.T) {
	fallback := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		errType := r.Header.Get("X-Vrata-Error")
		errDest := r.Header.Get("X-Vrata-Error-Destination")
		origPath := r.Header.Get("X-Vrata-Original-Path")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]string{
			"source":    "fallback",
			"error":     errType,
			"failedDst": errDest,
			"origPath":  origPath,
		})
	})

	deadDestID := createDestination(t, "onerror-dead-fwd", "127.0.0.1", 19877)
	defer apiDelete(t, "/destinations/"+deadDestID)
	fallbackDestID := createDestination(t, "onerror-fallback", fallback.host(), fallback.port())
	defer apiDelete(t, "/destinations/"+fallbackDestID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "onerror-fwd",
		"match": map[string]any{"pathPrefix": "/e2e-onerror-fwd"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": deadDestID, "weight": 100}},
		},
		"onError": []map[string]any{
			{
				"on": []string{"connection_refused"},
				"forward": map[string]any{
					"destinations": []map[string]any{{"destinationId": fallbackDestID, "weight": 100}},
				},
			},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyGet(t, "/e2e-onerror-fwd", nil)
	if code != 200 {
		t.Fatalf("expected 200 from fallback, got %d body=%s", code, body)
	}
	var result map[string]string
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("expected JSON, got %q", body)
	}
	if result["source"] != "fallback" {
		t.Errorf("expected fallback source, got %v", result)
	}
	if result["error"] != "connection_refused" {
		t.Errorf("expected connection_refused error header, got %q", result["error"])
	}
	if result["origPath"] != "/e2e-onerror-fwd" {
		t.Errorf("expected original path, got %q", result["origPath"])
	}
}

func TestE2E_OnError_Redirect(t *testing.T) {
	destID := createDestination(t, "onerror-dead-redir", "127.0.0.1", 19878)
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "onerror-redirect",
		"match": map[string]any{"pathPrefix": "/e2e-onerror-redirect"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
		},
		"onError": []map[string]any{
			{
				"on":       []string{"infrastructure"},
				"redirect": map[string]any{"url": "https://status.example.com/down", "code": 302},
			},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, headers, _ := proxyGet(t, "/e2e-onerror-redirect", nil)
	if code != 302 {
		t.Errorf("expected 302, got %d", code)
	}
	if loc := headers.Get("Location"); loc != "https://status.example.com/down" {
		t.Errorf("expected redirect to status page, got %q", loc)
	}
}

func TestE2E_OnError_NoMatchReturnsDefaultJSON(t *testing.T) {
	destID := createDestination(t, "onerror-dead-nomatch", "127.0.0.1", 19879)
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "onerror-nomatch",
		"match": map[string]any{"pathPrefix": "/e2e-onerror-nomatch"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
		},
		"onError": []map[string]any{
			{
				"on":             []string{"timeout"},
				"directResponse": map[string]any{"status": 504, "body": "timeout only"},
			},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, headers, body := proxyGet(t, "/e2e-onerror-nomatch", nil)
	if code != 502 {
		t.Errorf("expected default 502, got %d", code)
	}
	if ct := headers.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("expected JSON, got %q", ct)
	}
	var errBody map[string]string
	json.Unmarshal([]byte(body), &errBody)
	if errBody["error"] == "" {
		t.Error("expected error field in default JSON")
	}
}

func TestE2E_OnError_WildcardAll(t *testing.T) {
	destID := createDestination(t, "onerror-dead-all", "127.0.0.1", 19880)
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "onerror-all",
		"match": map[string]any{"pathPrefix": "/e2e-onerror-all"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
		},
		"onError": []map[string]any{
			{
				"on":             []string{"all"},
				"directResponse": map[string]any{"status": 500, "body": `{"caught":"all"}`},
			},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyGet(t, "/e2e-onerror-all", nil)
	if code != 500 {
		t.Errorf("expected 500, got %d", code)
	}
	if !strings.Contains(body, `"caught":"all"`) {
		t.Errorf("expected all wildcard response, got %q", body)
	}
}
