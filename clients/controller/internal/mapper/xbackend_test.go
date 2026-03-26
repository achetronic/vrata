// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package mapper

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/achetronic/vrata/clients/controller/apis/agentic"
)

func strPtr(s string) *string { return &s }
func int32Ptr(i int32) *int32 { return &i }

// ─── XBackend ───────────────────────────────────────────────────────────────

func TestMapXBackend_ServiceName(t *testing.T) {
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

	m := MapXBackend(backend)

	if m.Destination.Name != "k8s:agentic:default/calc" {
		t.Errorf("dest name: got %q", m.Destination.Name)
	}
	if m.Destination.Host != "calc-svc.default.svc.cluster.local" {
		t.Errorf("dest host: got %q", m.Destination.Host)
	}
	if m.Destination.Port != 9000 {
		t.Errorf("dest port: got %d", m.Destination.Port)
	}
	if m.Destination.Options != nil {
		t.Error("in-cluster backend should not have TLS options")
	}

	if m.Route.Name != "k8s:agentic:default/calc/mcp" {
		t.Errorf("route name: got %q", m.Route.Name)
	}
	if m.Route.Match["pathPrefix"] != "/mcp" {
		t.Errorf("route pathPrefix: got %v", m.Route.Match["pathPrefix"])
	}
	if m.Route.Forward == nil {
		t.Fatal("route forward should not be nil")
	}
}

func TestMapXBackend_Hostname(t *testing.T) {
	backend := &agentic.XBackend{
		ObjectMeta: metav1.ObjectMeta{Name: "wiki", Namespace: "prod"},
		Spec: agentic.XBackendSpec{
			MCP: agentic.MCPBackend{
				Hostname: strPtr("mcp.deepwiki.com"),
				Port:     443,
			},
		},
	}

	m := MapXBackend(backend)

	if m.Destination.Host != "mcp.deepwiki.com" {
		t.Errorf("dest host: got %q", m.Destination.Host)
	}
	if m.Destination.Options == nil {
		t.Fatal("external backend should have TLS options")
	}
	tls, ok := m.Destination.Options["tls"].(map[string]any)
	if !ok || tls["mode"] != "tls" {
		t.Errorf("expected TLS mode: got %v", m.Destination.Options["tls"])
	}
}

func TestMapXBackend_DefaultPath(t *testing.T) {
	backend := &agentic.XBackend{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "ns"},
		Spec: agentic.XBackendSpec{
			MCP: agentic.MCPBackend{
				ServiceName: strPtr("svc"),
				Port:        8080,
			},
		},
	}

	m := MapXBackend(backend)

	if m.Route.Match["pathPrefix"] != "/mcp" {
		t.Errorf("default path should be /mcp, got %v", m.Route.Match["pathPrefix"])
	}
}

func TestMapXBackend_CustomPath(t *testing.T) {
	backend := &agentic.XBackend{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "ns"},
		Spec: agentic.XBackendSpec{
			MCP: agentic.MCPBackend{
				ServiceName: strPtr("svc"),
				Port:        8080,
				Path:        "/custom/mcp",
			},
		},
	}

	m := MapXBackend(backend)

	if m.Route.Match["pathPrefix"] != "/custom/mcp" {
		t.Errorf("custom path: got %v", m.Route.Match["pathPrefix"])
	}
}

// ─── XAccessPolicy ──────────────────────────────────────────────────────────

func TestMapXAccessPolicy_InlineTools_SPIFFE(t *testing.T) {
	policy := &agentic.XAccessPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "pol1", Namespace: "default"},
		Spec: agentic.XAccessPolicySpec{
			TargetRefs: []agentic.PolicyTargetRef{{Group: "agentic.prototype.x-k8s.io", Kind: "XBackend", Name: "calc"}},
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
			},
		},
	}

	m := MapXAccessPolicy(policy, "cluster.local")
	if m == nil {
		t.Fatal("expected non-nil result")
	}

	if m.Middleware.Type != "inlineAuthz" {
		t.Errorf("type: got %q", m.Middleware.Type)
	}
	if m.Middleware.InlineAuthz == nil {
		t.Fatal("inlineAuthz config should not be nil")
	}

	rules, ok := m.Middleware.InlineAuthz["rules"].([]map[string]any)
	if !ok {
		t.Fatal("rules should be []map[string]any")
	}
	// Should have: 1 always-allow + 1 per-source rule = 2.
	if len(rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(rules))
	}

	// Second rule should contain SPIFFE check and tool list.
	cel2, _ := rules[1]["cel"].(string)
	if cel2 == "" {
		t.Fatal("second rule should have a CEL expression")
	}
	if !contains(cel2, "spiffe://cluster.local/ns/default/sa/agent-a") {
		t.Errorf("CEL should contain SPIFFE URI, got: %s", cel2)
	}
	if !contains(cel2, `"add"`) || !contains(cel2, `"subtract"`) {
		t.Errorf("CEL should contain tool names, got: %s", cel2)
	}
}

func TestMapXAccessPolicy_InlineTools_ServiceAccount(t *testing.T) {
	policy := &agentic.XAccessPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "pol2", Namespace: "default"},
		Spec: agentic.XAccessPolicySpec{
			TargetRefs: []agentic.PolicyTargetRef{{Group: "agentic.prototype.x-k8s.io", Kind: "XBackend", Name: "calc"}},
			Rules: []agentic.AccessRule{
				{
					Name: "sa-rule",
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

	m := MapXAccessPolicy(policy, "cluster.local")
	if m == nil {
		t.Fatal("expected non-nil result")
	}

	rules := m.Middleware.InlineAuthz["rules"].([]map[string]any)
	cel2, _ := rules[1]["cel"].(string)

	// ServiceAccount should be converted to SPIFFE ID with policy namespace.
	if !contains(cel2, "spiffe://cluster.local/ns/default/sa/agent-b") {
		t.Errorf("CEL should contain SPIFFE ID for SA, got: %s", cel2)
	}
}

func TestMapXAccessPolicy_InlineTools_SAWithNamespace(t *testing.T) {
	policy := &agentic.XAccessPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "pol3", Namespace: "default"},
		Spec: agentic.XAccessPolicySpec{
			TargetRefs: []agentic.PolicyTargetRef{{Group: "agentic.prototype.x-k8s.io", Kind: "XBackend", Name: "calc"}},
			Rules: []agentic.AccessRule{
				{
					Name: "cross-ns",
					Source: agentic.Source{
						Type:           "ServiceAccount",
						ServiceAccount: &agentic.SourceServiceAccount{Name: "agent-c", Namespace: "other"},
					},
					Authorization: &agentic.AuthorizationRule{
						Type:  "InlineTools",
						Tools: []string{"add"},
					},
				},
			},
		},
	}

	m := MapXAccessPolicy(policy, "cluster.local")
	rules := m.Middleware.InlineAuthz["rules"].([]map[string]any)
	cel2, _ := rules[1]["cel"].(string)

	if !contains(cel2, "spiffe://cluster.local/ns/other/sa/agent-c") {
		t.Errorf("CEL should use explicit namespace, got: %s", cel2)
	}
}

func TestMapXAccessPolicy_ExternalAuth(t *testing.T) {
	port := int32(9090)
	policy := &agentic.XAccessPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "ext-pol", Namespace: "default"},
		Spec: agentic.XAccessPolicySpec{
			TargetRefs: []agentic.PolicyTargetRef{{Group: "agentic.prototype.x-k8s.io", Kind: "XBackend", Name: "calc"}},
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

	m := MapXAccessPolicy(policy, "cluster.local")
	if m == nil {
		t.Fatal("expected non-nil result")
	}

	if m.Middleware.Type != "extAuthz" {
		t.Errorf("type: got %q, want extAuthz", m.Middleware.Type)
	}
	if m.Middleware.ExtAuthz == nil {
		t.Fatal("extAuthz config should not be nil")
	}
	if m.Middleware.ExtAuthz["mode"] != "grpc" {
		t.Errorf("mode: got %v", m.Middleware.ExtAuthz["mode"])
	}
}

func TestMapXAccessPolicy_EmptyTools_DenyAll(t *testing.T) {
	policy := &agentic.XAccessPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "deny-pol", Namespace: "default"},
		Spec: agentic.XAccessPolicySpec{
			TargetRefs: []agentic.PolicyTargetRef{{Group: "agentic.prototype.x-k8s.io", Kind: "XBackend", Name: "calc"}},
			Rules: []agentic.AccessRule{
				{
					Name:   "deny-rule",
					Source: agentic.Source{Type: "SPIFFE", SPIFFE: strPtr("spiffe://example.org/sa/agent")},
					Authorization: &agentic.AuthorizationRule{
						Type:  "InlineTools",
						Tools: []string{},
					},
				},
			},
		},
	}

	m := MapXAccessPolicy(policy, "cluster.local")
	if m == nil {
		t.Fatal("expected non-nil result")
	}

	rules := m.Middleware.InlineAuthz["rules"].([]map[string]any)
	// Only the always-allow rule — empty tools means no per-source rule generated.
	if len(rules) != 1 {
		t.Errorf("empty tools should produce only always-allow rule, got %d rules", len(rules))
	}
}

func TestMapXAccessPolicy_NoAuthorization_Nil(t *testing.T) {
	policy := &agentic.XAccessPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "empty", Namespace: "default"},
		Spec: agentic.XAccessPolicySpec{
			TargetRefs: []agentic.PolicyTargetRef{{Group: "agentic.prototype.x-k8s.io", Kind: "XBackend", Name: "calc"}},
			Rules: []agentic.AccessRule{
				{
					Name:   "no-auth",
					Source: agentic.Source{Type: "SPIFFE", SPIFFE: strPtr("spiffe://x/sa/a")},
				},
			},
		},
	}

	m := MapXAccessPolicy(policy, "cluster.local")
	if m != nil {
		t.Error("policy with no authorization rules should return nil")
	}
}

func TestMapXAccessPolicy_MultipleInlineRules(t *testing.T) {
	policy := &agentic.XAccessPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "multi", Namespace: "default"},
		Spec: agentic.XAccessPolicySpec{
			TargetRefs: []agentic.PolicyTargetRef{{Group: "agentic.prototype.x-k8s.io", Kind: "XBackend", Name: "calc"}},
			Rules: []agentic.AccessRule{
				{
					Name:          "r1",
					Source:        agentic.Source{Type: "SPIFFE", SPIFFE: strPtr("spiffe://x/sa/a")},
					Authorization: &agentic.AuthorizationRule{Type: "InlineTools", Tools: []string{"add"}},
				},
				{
					Name:          "r2",
					Source:        agentic.Source{Type: "SPIFFE", SPIFFE: strPtr("spiffe://x/sa/b")},
					Authorization: &agentic.AuthorizationRule{Type: "InlineTools", Tools: []string{"subtract"}},
				},
			},
		},
	}

	m := MapXAccessPolicy(policy, "cluster.local")
	rules := m.Middleware.InlineAuthz["rules"].([]map[string]any)
	// 1 always-allow + 2 per-source = 3.
	if len(rules) != 3 {
		t.Errorf("expected 3 rules, got %d", len(rules))
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func TestIsAgenticOwned(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"k8s:agentic:default/calc", true},
		{"k8s:agentic:ns/name/mcp", true},
		{"k8s:default/route", false},
		{"manual-route", false},
		{"k8s:agentic:", false},
	}
	for _, tt := range tests {
		if got := IsAgenticOwned(tt.name); got != tt.want {
			t.Errorf("IsAgenticOwned(%q): got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
