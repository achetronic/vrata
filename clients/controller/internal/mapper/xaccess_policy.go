// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package mapper

import (
	"fmt"
	"strings"

	"github.com/achetronic/vrata/clients/controller/apis/agentic"
	"github.com/achetronic/vrata/clients/controller/internal/vrata"
)

// MappedXAccessPolicy holds the Vrata middleware produced from an XAccessPolicy.
type MappedXAccessPolicy struct {
	Middleware vrata.Middleware
}

// MapXAccessPolicy translates an XAccessPolicy into a Vrata Middleware.
// For InlineTools rules, it generates an inlineAuthz middleware with CEL rules.
// For ExternalAuth rules, it generates an extAuthz middleware.
// trustDomain is used to convert ServiceAccount sources to SPIFFE URIs.
func MapXAccessPolicy(policy *agentic.XAccessPolicy, trustDomain string) *MappedXAccessPolicy {
	ns := policy.Namespace
	name := policy.Name
	prefix := fmt.Sprintf("%s%s/%s", AgenticPrefix, ns, name)

	// Separate rules by authorization type.
	var inlineRules []agentic.AccessRule
	var externalRule *agentic.AccessRule

	for i := range policy.Spec.Rules {
		r := &policy.Spec.Rules[i]
		if r.Authorization == nil {
			continue
		}
		switch r.Authorization.Type {
		case "InlineTools":
			inlineRules = append(inlineRules, *r)
		case "ExternalAuth":
			externalRule = r
		}
	}

	// ExternalAuth takes precedence if present (max 1 per policy per spec).
	if externalRule != nil {
		return mapExternalAuth(prefix, externalRule, ns)
	}

	// InlineTools → inlineAuthz middleware with CEL rules.
	if len(inlineRules) > 0 {
		return mapInlineTools(prefix, inlineRules, ns, trustDomain)
	}

	return nil
}

// mapExternalAuth creates an extAuthz middleware from an ExternalAuth rule.
func mapExternalAuth(prefix string, rule *agentic.AccessRule, policyNs string) *MappedXAccessPolicy {
	ea := rule.Authorization.ExternalAuth
	if ea == nil {
		return nil
	}

	authzNs := policyNs
	if ea.BackendRef.Namespace != "" {
		authzNs = ea.BackendRef.Namespace
	}
	destName := fmt.Sprintf("%s%s/%s:%d", AgenticPrefix, authzNs, ea.BackendRef.Name, safePort(ea.BackendRef.Port))

	mode := "http"
	if strings.EqualFold(ea.Protocol, "GRPC") {
		mode = "grpc"
	}

	cfg := map[string]any{
		"destinationId":    destName,
		"mode":             mode,
		"failureModeAllow": false,
	}

	return &MappedXAccessPolicy{
		Middleware: vrata.Middleware{
			Name:     prefix + "/extauthz",
			Type:     "extAuthz",
			ExtAuthz: cfg,
		},
	}
}

// mapInlineTools creates an inlineAuthz middleware from InlineTools rules.
func mapInlineTools(prefix string, rules []agentic.AccessRule, policyNs, trustDomain string) *MappedXAccessPolicy {
	var celRules []map[string]any

	// Always-allow rule: MCP session init, tools/list, SSE, session close.
	celRules = append(celRules, map[string]any{
		"cel":    `request.method == "GET" || request.method == "DELETE" || (has(request.body) && has(request.body.json) && request.body.json.method in ["initialize", "notifications/initialized", "tools/list"])`,
		"action": "allow",
	})

	// Per-source+tools rules.
	for _, rule := range rules {
		sourceCEL := sourceToСEL(rule.Source, policyNs, trustDomain)
		if sourceCEL == "" {
			continue
		}

		tools := rule.Authorization.Tools
		if len(tools) == 0 {
			// No tools = deny all tools/call from this source (only allow list above).
			continue
		}

		toolsList := `"` + strings.Join(tools, `", "`) + `"`
		toolsCEL := fmt.Sprintf(
			`has(request.body) && has(request.body.json) && (request.body.json.method != "tools/call" || request.body.json.params.name in [%s])`,
			toolsList,
		)

		celRules = append(celRules, map[string]any{
			"cel":    sourceCEL + " && " + toolsCEL,
			"action": "allow",
		})
	}

	cfg := map[string]any{
		"rules":         celRules,
		"defaultAction": "deny",
		"denyStatus":    float64(403),
		"denyBody":      `{"error":"access denied"}`,
	}

	return &MappedXAccessPolicy{
		Middleware: vrata.Middleware{
			Name:        prefix + "/inlineauthz",
			Type:        "inlineAuthz",
			InlineAuthz: cfg,
		},
	}
}

// sourceToСEL converts a Source to a CEL expression that matches the caller identity.
func sourceToСEL(src agentic.Source, policyNs, trustDomain string) string {
	switch src.Type {
	case "SPIFFE":
		if src.SPIFFE == nil {
			return ""
		}
		return fmt.Sprintf(
			`has(request.tls) && request.tls.peerCertificate.uris.exists(u, u == "%s")`,
			string(*src.SPIFFE),
		)
	case "ServiceAccount":
		if src.ServiceAccount == nil {
			return ""
		}
		ns := src.ServiceAccount.Namespace
		if ns == "" {
			ns = policyNs
		}
		spiffeID := fmt.Sprintf("spiffe://%s/ns/%s/sa/%s", trustDomain, ns, src.ServiceAccount.Name)
		return fmt.Sprintf(
			`has(request.tls) && request.tls.peerCertificate.uris.exists(u, u == "%s")`,
			spiffeID,
		)
	}
	return ""
}

// safePort returns the port value or 0 if nil.
func safePort(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}
