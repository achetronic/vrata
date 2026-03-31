// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package mapper

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/achetronic/vrata/clients/controller/apis/agentic"
)

func TestMapXAccessPolicy_ExternalAuth_HTTP(t *testing.T) {
	port := int32(8080)
	policy := &agentic.XAccessPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "http-pol", Namespace: "default"},
		Spec: agentic.XAccessPolicySpec{
			TargetRefs: []agentic.PolicyTargetRef{{Group: "agentic.prototype.x-k8s.io", Kind: "XBackend", Name: "calc"}},
			Rules: []agentic.AccessRule{
				{
					Name:   "http-rule",
					Source: agentic.Source{Type: "SPIFFE", SPIFFE: strPtr("spiffe://x/sa/a")},
					Authorization: &agentic.AuthorizationRule{
						Type: "ExternalAuth",
						ExternalAuth: &agentic.ExternalAuthConfig{
							BackendRef: agentic.BackendRef{Name: "authz-http", Port: &port},
							Protocol:   "HTTP",
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
	if m.Middleware.ExtAuthz["mode"] != "http" {
		t.Errorf("mode: got %v, want http", m.Middleware.ExtAuthz["mode"])
	}
}

func TestMapXAccessPolicy_ExternalAuth_CrossNamespace(t *testing.T) {
	port := int32(9090)
	policy := &agentic.XAccessPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "cross-ns", Namespace: "default"},
		Spec: agentic.XAccessPolicySpec{
			TargetRefs: []agentic.PolicyTargetRef{{Group: "agentic.prototype.x-k8s.io", Kind: "XBackend", Name: "calc"}},
			Rules: []agentic.AccessRule{
				{
					Name:   "cross-rule",
					Source: agentic.Source{Type: "SPIFFE", SPIFFE: strPtr("spiffe://x/sa/a")},
					Authorization: &agentic.AuthorizationRule{
						Type: "ExternalAuth",
						ExternalAuth: &agentic.ExternalAuthConfig{
							BackendRef: agentic.BackendRef{Name: "authz-svc", Namespace: "auth-system", Port: &port},
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

	destName, _ := m.Middleware.ExtAuthz["destinationId"].(string)
	if destName != "k8s:agentic:auth-system/authz-svc:9090" {
		t.Errorf("destination should use explicit namespace: got %q", destName)
	}
}

func TestMapXAccessPolicy_ExternalAuth_NoPort(t *testing.T) {
	policy := &agentic.XAccessPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "no-port", Namespace: "default"},
		Spec: agentic.XAccessPolicySpec{
			TargetRefs: []agentic.PolicyTargetRef{{Group: "agentic.prototype.x-k8s.io", Kind: "XBackend", Name: "calc"}},
			Rules: []agentic.AccessRule{
				{
					Name:   "no-port-rule",
					Source: agentic.Source{Type: "SPIFFE", SPIFFE: strPtr("spiffe://x/sa/a")},
					Authorization: &agentic.AuthorizationRule{
						Type: "ExternalAuth",
						ExternalAuth: &agentic.ExternalAuthConfig{
							BackendRef: agentic.BackendRef{Name: "authz-svc"},
							Protocol:   "HTTP",
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
	destName, _ := m.Middleware.ExtAuthz["destinationId"].(string)
	if destName != "k8s:agentic:default/authz-svc:0" {
		t.Errorf("no port should default to 0: got %q", destName)
	}
}

func TestMapXAccessPolicy_MixedRules_ExternalAuthWins(t *testing.T) {
	port := int32(9090)
	policy := &agentic.XAccessPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "mixed", Namespace: "default"},
		Spec: agentic.XAccessPolicySpec{
			TargetRefs: []agentic.PolicyTargetRef{{Group: "agentic.prototype.x-k8s.io", Kind: "XBackend", Name: "calc"}},
			Rules: []agentic.AccessRule{
				{
					Name:          "inline",
					Source:        agentic.Source{Type: "SPIFFE", SPIFFE: strPtr("spiffe://x/sa/a")},
					Authorization: &agentic.AuthorizationRule{Type: "InlineTools", Tools: []string{"add"}},
				},
				{
					Name:   "external",
					Source: agentic.Source{Type: "SPIFFE", SPIFFE: strPtr("spiffe://x/sa/b")},
					Authorization: &agentic.AuthorizationRule{
						Type:         "ExternalAuth",
						ExternalAuth: &agentic.ExternalAuthConfig{BackendRef: agentic.BackendRef{Name: "authz", Port: &port}, Protocol: "GRPC"},
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
		t.Errorf("ExternalAuth should take precedence, got type %q", m.Middleware.Type)
	}
}

func TestMapXBackend_EmptyServiceAndHostname(t *testing.T) {
	backend := &agentic.XBackend{
		ObjectMeta: metav1.ObjectMeta{Name: "empty", Namespace: "default"},
		Spec: agentic.XBackendSpec{
			MCP: agentic.MCPBackend{Port: 8080},
		},
	}

	m := MapXBackend(backend)

	if m.Destination.Host != "" {
		t.Errorf("host should be empty when neither serviceName nor hostname is set, got %q", m.Destination.Host)
	}
}

func TestMapXAccessPolicy_InlineTools_AlwaysAllowRule(t *testing.T) {
	policy := &agentic.XAccessPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-always", Namespace: "default"},
		Spec: agentic.XAccessPolicySpec{
			TargetRefs: []agentic.PolicyTargetRef{{Group: "agentic.prototype.x-k8s.io", Kind: "XBackend", Name: "calc"}},
			Rules: []agentic.AccessRule{
				{
					Name:          "agent",
					Source:        agentic.Source{Type: "SPIFFE", SPIFFE: strPtr("spiffe://x/sa/a")},
					Authorization: &agentic.AuthorizationRule{Type: "InlineTools", Tools: []string{"add"}},
				},
			},
		},
	}

	m := MapXAccessPolicy(policy, "cluster.local")
	rules := m.Middleware.InlineAuthz["rules"].([]map[string]any)

	firstCEL, _ := rules[0]["cel"].(string)
	firstAction, _ := rules[0]["action"].(string)

	if firstAction != "allow" {
		t.Errorf("first rule action: got %q, want allow", firstAction)
	}
	if !strings.Contains(firstCEL, "initialize") || !strings.Contains(firstCEL, "tools/list") {
		t.Errorf("first rule should allow initialize and tools/list: %s", firstCEL)
	}
	if !strings.Contains(firstCEL, `request.method == "GET"`) {
		t.Errorf("first rule should allow GET (SSE): %s", firstCEL)
	}
	if !strings.Contains(firstCEL, `request.method == "DELETE"`) {
		t.Errorf("first rule should allow DELETE (session close): %s", firstCEL)
	}
}

func TestMapXAccessPolicy_DefaultAction_IsDeny(t *testing.T) {
	policy := &agentic.XAccessPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "deny-default", Namespace: "default"},
		Spec: agentic.XAccessPolicySpec{
			TargetRefs: []agentic.PolicyTargetRef{{Group: "agentic.prototype.x-k8s.io", Kind: "XBackend", Name: "calc"}},
			Rules: []agentic.AccessRule{
				{
					Name:          "agent",
					Source:        agentic.Source{Type: "SPIFFE", SPIFFE: strPtr("spiffe://x/sa/a")},
					Authorization: &agentic.AuthorizationRule{Type: "InlineTools", Tools: []string{"add"}},
				},
			},
		},
	}

	m := MapXAccessPolicy(policy, "cluster.local")
	defaultAction, _ := m.Middleware.InlineAuthz["defaultAction"].(string)
	if defaultAction != "deny" {
		t.Errorf("defaultAction: got %q, want deny", defaultAction)
	}
}
