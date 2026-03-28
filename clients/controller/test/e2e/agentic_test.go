// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/achetronic/vrata/clients/controller/apis/agentic"
	"github.com/achetronic/vrata/clients/controller/internal/mapper"
	"github.com/achetronic/vrata/clients/controller/internal/reconciler"
	"github.com/achetronic/vrata/clients/controller/internal/vrata"
)

func TestE2E_Agentic_XBackend_ServiceName(t *testing.T) {
	ctx := context.Background()
	vrataCleanOwned(t)
	defer vrataCleanOwned(t)

	vrataClient := vrata.NewClient("http://localhost:8080")
	rec := reconciler.NewReconciler(vrataClient, testLogger())
	rec.Init(ctx)

	backend := &agentic.XBackend{
		ObjectMeta: metav1.ObjectMeta{Name: "calc", Namespace: "default"},
		Spec: agentic.XBackendSpec{
			MCP: agentic.MCPBackend{
				ServiceName: strPtr("calc-svc"),
				Port:        9000,
				Path:        "/mcp",
			},
		},
	}

	mapped := mapper.MapXBackend(backend)

	// Create destination.
	created, err := vrataClient.CreateDestination(ctx, mapped.Destination)
	if err != nil {
		t.Fatalf("creating destination: %v", err)
	}

	// Resolve destination ID in route.
	if fwd, ok := mapped.Route.Forward["destinations"].([]map[string]any); ok && len(fwd) > 0 {
		fwd[0]["destinationId"] = created.ID
	}

	if _, err := vrataClient.CreateRoute(ctx, mapped.Route); err != nil {
		t.Fatalf("creating route: %v", err)
	}

	// Verify.
	dests, _ := vrataClient.ListDestinations(ctx)
	found := false
	for _, d := range dests {
		if d.Name == "k8s:agentic:default/calc" {
			found = true
			if d.Host != "calc-svc.default.svc.cluster.local" {
				t.Errorf("host: %q", d.Host)
			}
			if d.Port != 9000 {
				t.Errorf("port: %d", d.Port)
			}
		}
	}
	if !found {
		t.Error("destination not found in Vrata")
	}

	routes, _ := vrataClient.ListRoutes(ctx)
	routeFound := false
	for _, r := range routes {
		if r.Name == "k8s:agentic:default/calc/mcp" {
			routeFound = true
			if r.Match["pathPrefix"] != "/mcp" {
				t.Errorf("pathPrefix: %v", r.Match["pathPrefix"])
			}
		}
	}
	if !routeFound {
		t.Error("route not found in Vrata")
	}
}

func TestE2E_Agentic_XBackend_Hostname(t *testing.T) {
	ctx := context.Background()
	vrataCleanOwned(t)
	defer vrataCleanOwned(t)

	vrataClient := vrata.NewClient("http://localhost:8080")

	backend := &agentic.XBackend{
		ObjectMeta: metav1.ObjectMeta{Name: "wiki", Namespace: "default"},
		Spec: agentic.XBackendSpec{
			MCP: agentic.MCPBackend{
				Hostname: strPtr("mcp.deepwiki.com"),
				Port:     443,
			},
		},
	}

	mapped := mapper.MapXBackend(backend)

	created, err := vrataClient.CreateDestination(ctx, mapped.Destination)
	if err != nil {
		t.Fatalf("creating destination: %v", err)
	}
	_ = created

	dests, _ := vrataClient.ListDestinations(ctx)
	for _, d := range dests {
		if d.Name == "k8s:agentic:default/wiki" {
			if d.Host != "mcp.deepwiki.com" {
				t.Errorf("host: %q", d.Host)
			}
			return
		}
	}
	t.Error("destination not found")
}

func TestE2E_Agentic_XAccessPolicy_InlineTools(t *testing.T) {
	ctx := context.Background()
	vrataCleanOwned(t)
	defer vrataCleanOwned(t)

	vrataClient := vrata.NewClient("http://localhost:8080")

	policy := &agentic.XAccessPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "calc-access", Namespace: "default"},
		Spec: agentic.XAccessPolicySpec{
			TargetRefs: []agentic.PolicyTargetRef{{
				Group: "agentic.prototype.x-k8s.io",
				Kind:  "XBackend",
				Name:  "calc",
			}},
			Rules: []agentic.AccessRule{
				{
					Name: "agent-a",
					Source: agentic.Source{
						Type:   "SPIFFE",
						SPIFFE: strPtr("spiffe://cluster.local/ns/default/sa/agent-a"),
					},
					Authorization: &agentic.AuthorizationRule{
						Type:  "InlineTools",
						Tools: []string{"add", "subtract"},
					},
				},
				{
					Name: "agent-b",
					Source: agentic.Source{
						Type:           "ServiceAccount",
						ServiceAccount: &agentic.SourceServiceAccount{Name: "agent-b"},
					},
					Authorization: &agentic.AuthorizationRule{
						Type:  "InlineTools",
						Tools: []string{"subtract"},
					},
				},
			},
		},
	}

	mapped := mapper.MapXAccessPolicy(policy, "cluster.local")
	if mapped == nil {
		t.Fatal("expected non-nil mapped result")
	}

	if _, err := vrataClient.CreateMiddleware(ctx, mapped.Middleware); err != nil {
		t.Fatalf("creating middleware: %v", err)
	}

	mws, _ := vrataClient.ListMiddlewares(ctx)
	found := false
	for _, mw := range mws {
		if mw.Name == "k8s:agentic:default/calc-access/inlineauthz" {
			found = true
			if mw.Type != "inlineAuthz" {
				t.Errorf("type: %q", mw.Type)
			}
			if mw.InlineAuthz == nil {
				t.Fatal("inlineAuthz config nil")
			}

			rules, ok := mw.InlineAuthz["rules"].([]any)
			if !ok {
				t.Fatal("rules not []any")
			}
			// 1 always-allow + 2 per-source = 3 rules.
			if len(rules) != 3 {
				t.Errorf("expected 3 rules, got %d", len(rules))
			}

			// Check agent-a rule contains SPIFFE and tool names.
			rule1, ok := rules[1].(map[string]any)
			if ok {
				cel1, _ := rule1["cel"].(string)
				if !strings.Contains(cel1, "agent-a") {
					t.Errorf("rule 1 should reference agent-a: %s", cel1)
				}
				if !strings.Contains(cel1, `"add"`) {
					t.Errorf("rule 1 should list add tool: %s", cel1)
				}
			}

			// Check agent-b rule uses SA→SPIFFE conversion.
			rule2, ok := rules[2].(map[string]any)
			if ok {
				cel2, _ := rule2["cel"].(string)
				if !strings.Contains(cel2, "spiffe://cluster.local/ns/default/sa/agent-b") {
					t.Errorf("rule 2 should convert SA to SPIFFE: %s", cel2)
				}
				if !strings.Contains(cel2, `"subtract"`) {
					t.Errorf("rule 2 should list subtract tool: %s", cel2)
				}
			}
		}
	}
	if !found {
		t.Error("middleware not found in Vrata")
	}
}

func TestE2E_Agentic_XAccessPolicy_ExternalAuth(t *testing.T) {
	ctx := context.Background()
	vrataCleanOwned(t)
	defer vrataCleanOwned(t)

	vrataClient := vrata.NewClient("http://localhost:8080")

	port := int32(9090)
	policy := &agentic.XAccessPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "ext-pol", Namespace: "default"},
		Spec: agentic.XAccessPolicySpec{
			TargetRefs: []agentic.PolicyTargetRef{{
				Group: "agentic.prototype.x-k8s.io",
				Kind:  "XBackend",
				Name:  "calc",
			}},
			Rules: []agentic.AccessRule{
				{
					Name:   "ext-rule",
					Source: agentic.Source{Type: "SPIFFE", SPIFFE: strPtr("spiffe://example.org/sa/agent")},
					Authorization: &agentic.AuthorizationRule{
						Type: "ExternalAuth",
						ExternalAuth: &agentic.ExternalAuthConfig{
							BackendRef: agentic.BackendRef{Name: "authz-svc", Port: &port},
							Protocol:   "GRPC",
						},
					},
				},
			},
		},
	}

	mapped := mapper.MapXAccessPolicy(policy, "cluster.local")
	if mapped == nil {
		t.Fatal("expected non-nil mapped result")
	}

	if _, err := vrataClient.CreateMiddleware(ctx, mapped.Middleware); err != nil {
		t.Fatalf("creating middleware: %v", err)
	}

	mws, _ := vrataClient.ListMiddlewares(ctx)
	for _, mw := range mws {
		if mw.Name == "k8s:agentic:default/ext-pol/extauthz" {
			if mw.Type != "extAuthz" {
				t.Errorf("type: %q", mw.Type)
			}
			if mw.ExtAuthz["mode"] != "grpc" {
				t.Errorf("mode: %v", mw.ExtAuthz["mode"])
			}
			return
		}
	}
	t.Error("extAuthz middleware not found")
}

func TestE2E_Agentic_GC(t *testing.T) {
	ctx := context.Background()
	vrataCleanOwned(t)
	defer vrataCleanOwned(t)

	vrataClient := vrata.NewClient("http://localhost:8080")

	// Create an agentic destination directly.
	dest := vrata.Destination{
		Name: "k8s:agentic:default/orphan",
		Host: "orphan.default.svc.cluster.local",
		Port: 8080,
	}
	created, err := vrataClient.CreateDestination(ctx, dest)
	if err != nil {
		t.Fatalf("creating orphan destination: %v", err)
	}

	// Verify it exists.
	dests, _ := vrataClient.ListDestinations(ctx)
	found := false
	for _, d := range dests {
		if d.Name == "k8s:agentic:default/orphan" {
			found = true
		}
	}
	if !found {
		t.Fatal("orphan destination should exist before GC")
	}

	// Run GC with empty desired set — should delete the orphan.
	desired := make(map[string]bool)
	routes, _ := vrataClient.ListRoutes(ctx)
	for _, r := range routes {
		if mapper.IsAgenticOwned(r.Name) && !desired[r.Name] {
			vrataClient.DeleteRoute(ctx, r.ID)
		}
	}
	dests2, _ := vrataClient.ListDestinations(ctx)
	for _, d := range dests2 {
		if mapper.IsAgenticOwned(d.Name) && !desired[d.Name] {
			vrataClient.DeleteDestination(ctx, d.ID)
		}
	}

	// Verify cleaned.
	_ = created
	dests3, _ := vrataClient.ListDestinations(ctx)
	for _, d := range dests3 {
		if d.Name == "k8s:agentic:default/orphan" {
			t.Error("orphan destination should be deleted by GC")
		}
	}
}
