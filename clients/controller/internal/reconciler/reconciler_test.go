package reconciler

import (
	"testing"

	"github.com/achetronic/vrata/clients/controller/internal/vrata"
)

func TestRefCount_IncrementDecrement(t *testing.T) {
	rc := NewRefCount()
	rc.Increment("k8s:default/svc:80")
	rc.Increment("k8s:default/svc:80")
	rc.Increment("k8s:prod/api:8080")

	if rc.Count("k8s:default/svc:80") != 2 {
		t.Errorf("expected 2, got %d", rc.Count("k8s:default/svc:80"))
	}
	if rc.Count("k8s:prod/api:8080") != 1 {
		t.Errorf("expected 1, got %d", rc.Count("k8s:prod/api:8080"))
	}

	zero := rc.Decrement("k8s:prod/api:8080")
	if !zero {
		t.Error("expected zero after decrement")
	}
	if rc.Count("k8s:prod/api:8080") != 0 {
		t.Error("expected 0 after zero")
	}

	notZero := rc.Decrement("k8s:default/svc:80")
	if notZero {
		t.Error("should not be zero yet")
	}
	if rc.Count("k8s:default/svc:80") != 1 {
		t.Errorf("expected 1, got %d", rc.Count("k8s:default/svc:80"))
	}
}

func TestRefCount_RebuildFromRoutes(t *testing.T) {
	routes := []vrata.Route{
		{
			Name: "k8s:default/app/rule-0/match-0",
			Forward: map[string]any{
				"destinations": []any{
					map[string]any{"destinationId": "k8s:default/svc-a:80"},
				},
			},
		},
		{
			Name: "k8s:default/app/rule-1/match-0",
			Forward: map[string]any{
				"destinations": []any{
					map[string]any{"destinationId": "k8s:default/svc-a:80"},
					map[string]any{"destinationId": "k8s:default/svc-b:8080"},
				},
			},
		},
		{
			Name:     "k8s:default/redirect/rule-0/match-0",
			Redirect: map[string]any{"scheme": "https"},
		},
		{
			Name: "manual-route",
			Forward: map[string]any{
				"destinations": []any{
					map[string]any{"destinationId": "manual-dest"},
				},
			},
		},
	}

	rc := NewRefCount()
	rc.RebuildFromRoutes(routes)

	if rc.Count("k8s:default/svc-a:80") != 2 {
		t.Errorf("svc-a: expected 2, got %d", rc.Count("k8s:default/svc-a:80"))
	}
	if rc.Count("k8s:default/svc-b:8080") != 1 {
		t.Errorf("svc-b: expected 1, got %d", rc.Count("k8s:default/svc-b:8080"))
	}
	if rc.Count("manual-dest") != 0 {
		t.Error("manual-dest should not be counted (not owned)")
	}
}

func TestExtractDestinationNames(t *testing.T) {
	route := vrata.Route{
		Forward: map[string]any{
			"destinations": []any{
				map[string]any{"destinationId": "dest-a", "weight": 80},
				map[string]any{"destinationId": "dest-b", "weight": 20},
			},
		},
	}
	names := extractDestinationNames(route)
	if len(names) != 2 {
		t.Fatalf("expected 2, got %d", len(names))
	}
	if names[0] != "dest-a" || names[1] != "dest-b" {
		t.Errorf("unexpected names: %v", names)
	}
}

func TestExtractDestinationNames_NoForward(t *testing.T) {
	route := vrata.Route{Redirect: map[string]any{"scheme": "https"}}
	names := extractDestinationNames(route)
	if len(names) != 0 {
		t.Errorf("expected 0 for redirect route, got %d", len(names))
	}
}

func TestResolveRouteRefs(t *testing.T) {
	route := vrata.Route{
		Name: "test",
		Forward: map[string]any{
			"destinations": []map[string]any{
				{"destinationId": "k8s:default/svc:80", "weight": 100},
			},
		},
		MiddlewareIDs: []string{"k8s:default/app/rule-0/headers"},
	}
	destIDs := map[string]string{"k8s:default/svc:80": "uuid-dest-1"}
	mwIDs := map[string]string{"k8s:default/app/rule-0/headers": "uuid-mw-1"}

	resolved := resolveRouteRefs(route, destIDs, mwIDs)

	dests := resolved.Forward["destinations"].([]map[string]any)
	if dests[0]["destinationId"] != "uuid-dest-1" {
		t.Errorf("destination not resolved: %v", dests[0])
	}
	if resolved.MiddlewareIDs[0] != "uuid-mw-1" {
		t.Errorf("middleware not resolved: %v", resolved.MiddlewareIDs)
	}
}
