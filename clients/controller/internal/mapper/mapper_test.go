// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package mapper

import (
	"testing"
)

func TestMapHTTPRoute_SimpleForward(t *testing.T) {
	input := HTTPRouteInput{
		Name:      "my-route",
		Namespace: "default",
		Hostnames: []string{"api.example.com"},
		Rules: []RuleInput{
			{
				Matches: []MatchInput{
					{PathType: "PathPrefix", PathValue: "/api"},
				},
				BackendRefs: []BackendRefInput{
					{ServiceName: "api-svc", ServiceNamespace: "default", Port: 80, Weight: 100},
				},
			},
		},
	}

	result := MapHTTPRoute(input)

	if result.Group.Name != "k8s:default/my-route" {
		t.Errorf("group name: got %q", result.Group.Name)
	}
	if len(result.Group.Hostnames) != 1 || result.Group.Hostnames[0] != "api.example.com" {
		t.Errorf("group hostnames: %v", result.Group.Hostnames)
	}
	if len(result.Routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(result.Routes))
	}
	r := result.Routes[0]
	if r.Name != "k8s:default/my-route/rule-0/match-0" {
		t.Errorf("route name: got %q", r.Name)
	}
	if r.Match["pathPrefix"] != "/api" {
		t.Errorf("route match: %v", r.Match)
	}
	if r.Forward == nil {
		t.Fatal("expected forward action")
	}
	if len(result.Destinations) != 1 {
		t.Fatalf("expected 1 destination, got %d", len(result.Destinations))
	}
	if result.Destinations[0].DestinationName() != "k8s:default/api-svc:80" {
		t.Errorf("destination name: got %q", result.Destinations[0].DestinationName())
	}
}

func TestMapHTTPRoute_ExactPath(t *testing.T) {
	input := HTTPRouteInput{
		Name: "exact", Namespace: "prod",
		Rules: []RuleInput{
			{
				Matches: []MatchInput{
					{PathType: "Exact", PathValue: "/health"},
				},
				BackendRefs: []BackendRefInput{
					{ServiceName: "health-svc", ServiceNamespace: "prod", Port: 8080},
				},
			},
		},
	}
	result := MapHTTPRoute(input)
	if result.Routes[0].Match["path"] != "/health" {
		t.Errorf("expected exact path, got %v", result.Routes[0].Match)
	}
}

func TestMapHTTPRoute_RegexPath(t *testing.T) {
	input := HTTPRouteInput{
		Name: "regex", Namespace: "prod",
		Rules: []RuleInput{
			{
				Matches: []MatchInput{
					{PathType: "RegularExpression", PathValue: "/users/[0-9]+"},
				},
				BackendRefs: []BackendRefInput{
					{ServiceName: "users", ServiceNamespace: "prod", Port: 80},
				},
			},
		},
	}
	result := MapHTTPRoute(input)
	if result.Routes[0].Match["pathRegex"] != "/users/[0-9]+" {
		t.Errorf("expected regex path, got %v", result.Routes[0].Match)
	}
}

func TestMapHTTPRoute_MultipleMatches(t *testing.T) {
	input := HTTPRouteInput{
		Name: "multi", Namespace: "default",
		Rules: []RuleInput{
			{
				Matches: []MatchInput{
					{PathType: "PathPrefix", PathValue: "/a"},
					{PathType: "PathPrefix", PathValue: "/b"},
					{PathType: "Exact", PathValue: "/c"},
				},
				BackendRefs: []BackendRefInput{
					{ServiceName: "svc", ServiceNamespace: "default", Port: 80},
				},
			},
		},
	}
	result := MapHTTPRoute(input)
	if len(result.Routes) != 3 {
		t.Fatalf("expected 3 routes (one per match), got %d", len(result.Routes))
	}
	if result.Routes[2].Match["path"] != "/c" {
		t.Errorf("third route should be exact /c, got %v", result.Routes[2].Match)
	}
}

func TestMapHTTPRoute_Redirect(t *testing.T) {
	input := HTTPRouteInput{
		Name: "redir", Namespace: "default",
		Rules: []RuleInput{
			{
				Matches: []MatchInput{
					{PathType: "PathPrefix", PathValue: "/old"},
				},
				Filters: []FilterInput{
					{Type: "RequestRedirect", RedirectScheme: "https", RedirectCode: 301},
				},
			},
		},
	}
	result := MapHTTPRoute(input)
	r := result.Routes[0]
	if r.Forward != nil {
		t.Error("redirect route should not have forward")
	}
	if r.Redirect == nil {
		t.Fatal("expected redirect action")
	}
	if r.Redirect["scheme"] != "https" {
		t.Errorf("redirect scheme: %v", r.Redirect)
	}
	if r.Redirect["code"] != uint32(301) {
		t.Errorf("redirect code: %v", r.Redirect["code"])
	}
}

func TestMapHTTPRoute_URLRewrite(t *testing.T) {
	input := HTTPRouteInput{
		Name: "rewrite", Namespace: "default",
		Rules: []RuleInput{
			{
				Matches: []MatchInput{
					{PathType: "PathPrefix", PathValue: "/old"},
				},
				BackendRefs: []BackendRefInput{
					{ServiceName: "svc", ServiceNamespace: "default", Port: 80},
				},
				Filters: []FilterInput{
					{Type: "URLRewrite", RewritePathPrefix: "/new"},
				},
			},
		},
	}
	result := MapHTTPRoute(input)
	fwd := result.Routes[0].Forward
	if fwd == nil {
		t.Fatal("expected forward")
	}
	rw, ok := fwd["rewrite"].(map[string]any)
	if !ok {
		t.Fatal("expected rewrite in forward")
	}
	if rw["path"] != "/new" {
		t.Errorf("rewrite path: %v", rw)
	}
}

func TestMapHTTPRoute_HeaderModifier(t *testing.T) {
	input := HTTPRouteInput{
		Name: "hdr", Namespace: "default",
		Rules: []RuleInput{
			{
				Matches: []MatchInput{
					{PathType: "PathPrefix", PathValue: "/"},
				},
				BackendRefs: []BackendRefInput{
					{ServiceName: "svc", ServiceNamespace: "default", Port: 80},
				},
				Filters: []FilterInput{
					{
						Type:            "RequestHeaderModifier",
						HeadersToAdd:    []HeaderValue{{Name: "X-Source", Value: "vrata"}},
						HeadersToRemove: []string{"X-Internal"},
					},
				},
			},
		},
	}
	result := MapHTTPRoute(input)
	if len(result.Middlewares) != 1 {
		t.Fatalf("expected 1 middleware, got %d", len(result.Middlewares))
	}
	mw := result.Middlewares[0]
	if mw.Type != "headers" {
		t.Errorf("expected headers type, got %q", mw.Type)
	}
	if len(result.Routes[0].MiddlewareIDs) != 1 {
		t.Error("route should reference the middleware")
	}
}

func TestMapHTTPRoute_DeduplicateDestinations(t *testing.T) {
	input := HTTPRouteInput{
		Name: "dedup", Namespace: "default",
		Rules: []RuleInput{
			{
				Matches:     []MatchInput{{PathType: "PathPrefix", PathValue: "/a"}},
				BackendRefs: []BackendRefInput{{ServiceName: "svc", ServiceNamespace: "default", Port: 80}},
			},
			{
				Matches:     []MatchInput{{PathType: "PathPrefix", PathValue: "/b"}},
				BackendRefs: []BackendRefInput{{ServiceName: "svc", ServiceNamespace: "default", Port: 80}},
			},
		},
	}
	result := MapHTTPRoute(input)
	if len(result.Destinations) != 1 {
		t.Errorf("expected 1 deduplicated destination, got %d", len(result.Destinations))
	}
}

func TestMapHTTPRoute_MultipleBackends(t *testing.T) {
	input := HTTPRouteInput{
		Name: "split", Namespace: "default",
		Rules: []RuleInput{
			{
				Matches: []MatchInput{{PathType: "PathPrefix", PathValue: "/"}},
				BackendRefs: []BackendRefInput{
					{ServiceName: "svc-a", ServiceNamespace: "default", Port: 80, Weight: 80},
					{ServiceName: "svc-b", ServiceNamespace: "default", Port: 80, Weight: 20},
				},
			},
		},
	}
	result := MapHTTPRoute(input)
	if len(result.Destinations) != 2 {
		t.Errorf("expected 2 destinations, got %d", len(result.Destinations))
	}
	dests := result.Routes[0].Forward["destinations"].([]map[string]any)
	if len(dests) != 2 {
		t.Fatalf("expected 2 destination refs, got %d", len(dests))
	}
	if dests[0]["weight"] != uint32(80) {
		t.Errorf("expected weight 80, got %v", dests[0]["weight"])
	}
}

func TestMapHTTPRoute_NoMatches(t *testing.T) {
	input := HTTPRouteInput{
		Name: "nomatch", Namespace: "default",
		Rules: []RuleInput{
			{
				BackendRefs: []BackendRefInput{
					{ServiceName: "svc", ServiceNamespace: "default", Port: 80},
				},
			},
		},
	}
	result := MapHTTPRoute(input)
	if len(result.Routes) != 1 {
		t.Fatalf("expected 1 route with default match, got %d", len(result.Routes))
	}
	if result.Routes[0].Match["pathPrefix"] != "/" {
		t.Errorf("expected default pathPrefix /, got %v", result.Routes[0].Match)
	}
}

func TestMapHTTPRoute_MethodMatch(t *testing.T) {
	input := HTTPRouteInput{
		Name: "method", Namespace: "default",
		Rules: []RuleInput{
			{
				Matches: []MatchInput{
					{PathType: "PathPrefix", PathValue: "/api", Method: "POST"},
				},
				BackendRefs: []BackendRefInput{
					{ServiceName: "svc", ServiceNamespace: "default", Port: 80},
				},
			},
		},
	}
	result := MapHTTPRoute(input)
	methods, ok := result.Routes[0].Match["methods"].([]string)
	if !ok || len(methods) != 1 || methods[0] != "POST" {
		t.Errorf("expected methods [POST], got %v", result.Routes[0].Match["methods"])
	}
}

func TestMapHTTPRoute_HeaderMatch(t *testing.T) {
	input := HTTPRouteInput{
		Name: "headermatch", Namespace: "default",
		Rules: []RuleInput{
			{
				Matches: []MatchInput{
					{
						PathType: "PathPrefix", PathValue: "/",
						Headers: []HeaderMatchInput{
							{Name: "X-Tenant", Value: "acme", Type: "Exact"},
						},
					},
				},
				BackendRefs: []BackendRefInput{
					{ServiceName: "svc", ServiceNamespace: "default", Port: 80},
				},
			},
		},
	}
	result := MapHTTPRoute(input)
	headers, ok := result.Routes[0].Match["headers"].([]map[string]any)
	if !ok || len(headers) != 1 {
		t.Fatalf("expected 1 header match, got %v", result.Routes[0].Match["headers"])
	}
	if headers[0]["name"] != "X-Tenant" || headers[0]["value"] != "acme" {
		t.Errorf("unexpected header match: %v", headers[0])
	}
}

func TestMapGateway(t *testing.T) {
	input := GatewayInput{
		Name:      "stable",
		Namespace: "istio",
		Listeners: []GatewayListenerInput{
			{Name: "http", Port: 80, Protocol: "HTTP"},
			{Name: "https", Port: 443, Protocol: "HTTPS"},
		},
	}
	listeners := MapGateway(input)
	if len(listeners) != 2 {
		t.Fatalf("expected 2 listeners, got %d", len(listeners))
	}
	if listeners[0].Name != "k8s:istio/stable/http" {
		t.Errorf("listener 0 name: %q", listeners[0].Name)
	}
	if listeners[1].Port != 443 {
		t.Errorf("listener 1 port: %d", listeners[1].Port)
	}
}

func TestDestinationKeyFQDN(t *testing.T) {
	dk := DestinationKey{Name: "api-svc", Namespace: "prod", Port: 8080}
	fqdn := dk.FQDN()
	if fqdn != "api-svc.prod.svc.cluster.local" {
		t.Errorf("expected FQDN, got %q", fqdn)
	}
}

func TestIsOwned(t *testing.T) {
	if !IsOwned("k8s:default/test") {
		t.Error("should be owned")
	}
	if IsOwned("manual-route") {
		t.Error("should not be owned")
	}
}
