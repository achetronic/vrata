package dedup

import (
	"log/slog"
	"os"
	"testing"

	"github.com/achetronic/vrata/clients/controller/internal/mapper"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func route(name, ns, hostname, pathType, pathValue string) mapper.HTTPRouteInput {
	return mapper.HTTPRouteInput{
		Name: name, Namespace: ns,
		Hostnames: []string{hostname},
		Rules: []mapper.RuleInput{
			{Matches: []mapper.MatchInput{{PathType: pathType, PathValue: pathValue}}},
		},
	}
}

func routeWithHeaders(name, ns, hostname, pathType, pathValue string, headers []mapper.HeaderMatchInput) mapper.HTTPRouteInput {
	return mapper.HTTPRouteInput{
		Name: name, Namespace: ns,
		Hostnames: []string{hostname},
		Rules: []mapper.RuleInput{
			{Matches: []mapper.MatchInput{{PathType: pathType, PathValue: pathValue, Headers: headers}}},
		},
	}
}

func routeWithMethod(name, ns, hostname, pathType, pathValue, method string) mapper.HTTPRouteInput {
	return mapper.HTTPRouteInput{
		Name: name, Namespace: ns,
		Hostnames: []string{hostname},
		Rules: []mapper.RuleInput{
			{Matches: []mapper.MatchInput{{PathType: pathType, PathValue: pathValue, Method: method}}},
		},
	}
}

func routeWithMethodAndHeaders(name, ns, hostname, pathType, pathValue, method string, headers []mapper.HeaderMatchInput) mapper.HTTPRouteInput {
	return mapper.HTTPRouteInput{
		Name: name, Namespace: ns,
		Hostnames: []string{hostname},
		Rules: []mapper.RuleInput{
			{Matches: []mapper.MatchInput{{PathType: pathType, PathValue: pathValue, Method: method, Headers: headers}}},
		},
	}
}

func TestDetector_NoDuplicate(t *testing.T) {
	d := NewDetector(testLogger())
	overlaps := d.Check(route("a", "default", "a.example.com", "PathPrefix", "/a"))
	if len(overlaps) > 0 {
		t.Error("should not be duplicate")
	}
}

func TestDetector_ExactDuplicate_Prefix(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(route("a", "default", "api.example.com", "PathPrefix", "/api"))
	overlaps := d.Check(route("b", "prod", "api.example.com", "PathPrefix", "/api"))
	if len(overlaps) == 0 {
		t.Error("identical prefix+host should be a duplicate")
	}
}

func TestDetector_ExactDuplicate_Exact(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(route("a", "default", "api.example.com", "Exact", "/health"))
	overlaps := d.Check(route("b", "prod", "api.example.com", "Exact", "/health"))
	if len(overlaps) == 0 {
		t.Error("identical exact+host should be a duplicate")
	}
}

func TestDetector_PrefixCoversPrefix(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(route("a", "default", "api.example.com", "PathPrefix", "/api"))
	overlaps := d.Check(route("b", "prod", "api.example.com", "PathPrefix", "/api/users"))
	if len(overlaps) == 0 {
		t.Error("/api should cover /api/users")
	}
}

func TestDetector_PrefixCoversExact(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(route("a", "default", "api.example.com", "PathPrefix", "/api"))
	overlaps := d.Check(route("b", "prod", "api.example.com", "Exact", "/api/users"))
	if len(overlaps) == 0 {
		t.Error("PathPrefix /api should cover Exact /api/users")
	}
}

func TestDetector_ExactCoveredByPrefix(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(route("a", "default", "api.example.com", "Exact", "/api/users"))
	overlaps := d.Check(route("b", "prod", "api.example.com", "PathPrefix", "/api"))
	if len(overlaps) == 0 {
		t.Error("Exact /api/users should be covered by incoming PathPrefix /api")
	}
}

func TestDetector_PrefixDoesNotCoverSibling(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(route("a", "default", "api.example.com", "PathPrefix", "/api/users"))
	overlaps := d.Check(route("b", "prod", "api.example.com", "PathPrefix", "/api/admin"))
	if len(overlaps) > 0 {
		t.Error("/api/users and /api/admin are siblings, should not overlap")
	}
}

func TestDetector_PrefixDoesNotCoverSimilarName(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(route("a", "default", "api.example.com", "PathPrefix", "/api"))
	overlaps := d.Check(route("b", "prod", "api.example.com", "PathPrefix", "/apikeys"))
	if len(overlaps) > 0 {
		t.Error("/api should NOT cover /apikeys (no segment boundary)")
	}
}

func TestDetector_DifferentExactPaths(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(route("a", "default", "api.example.com", "Exact", "/health"))
	overlaps := d.Check(route("b", "prod", "api.example.com", "Exact", "/ready"))
	if len(overlaps) > 0 {
		t.Error("different exact paths should not overlap")
	}
}

func TestDetector_DifferentHostNotDuplicate(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(route("a", "default", "a.example.com", "PathPrefix", "/api"))
	overlaps := d.Check(route("b", "default", "b.example.com", "PathPrefix", "/api"))
	if len(overlaps) > 0 {
		t.Error("different hostnames should not overlap")
	}
}

func TestDetector_SameSourceNotDuplicate(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(route("a", "default", "api.example.com", "PathPrefix", "/api"))
	overlaps := d.Check(route("a", "default", "api.example.com", "PathPrefix", "/api"))
	if len(overlaps) > 0 {
		t.Error("same source should not be flagged")
	}
}

func TestDetector_RegexSkipped(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(route("a", "default", "api.example.com", "RegularExpression", "/api/.*"))
	overlaps := d.Check(route("b", "prod", "api.example.com", "PathPrefix", "/api"))
	if len(overlaps) > 0 {
		t.Error("regex should be skipped, no overlap detected")
	}
}

func TestDetector_Remove(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(route("a", "default", "api.example.com", "PathPrefix", "/api"))
	d.Remove("default", "a")
	overlaps := d.Check(route("b", "prod", "api.example.com", "PathPrefix", "/api"))
	if len(overlaps) > 0 {
		t.Error("after remove, should not overlap")
	}
}

func TestDetector_RootPrefixCoversEverything(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(route("a", "default", "example.com", "PathPrefix", "/"))
	overlaps := d.Check(route("b", "prod", "example.com", "Exact", "/anything"))
	if len(overlaps) == 0 {
		t.Error("PathPrefix / should cover everything")
	}
}

// --- Header-aware tests ---

func TestDetector_DifferentHeaders_NoOverlap(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(routeWithHeaders("a", "default", "api.example.com", "PathPrefix", "/api",
		[]mapper.HeaderMatchInput{{Name: "X-Env", Value: "sandbox", Type: "Exact"}}))
	overlaps := d.Check(routeWithHeaders("b", "prod", "api.example.com", "PathPrefix", "/api",
		[]mapper.HeaderMatchInput{{Name: "X-Env", Value: "production", Type: "Exact"}}))
	if len(overlaps) > 0 {
		t.Error("same path+host but different header matchers should NOT overlap")
	}
}

func TestDetector_SameHeaders_Overlap(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(routeWithHeaders("a", "default", "api.example.com", "PathPrefix", "/api",
		[]mapper.HeaderMatchInput{{Name: "X-Env", Value: "production", Type: "Exact"}}))
	overlaps := d.Check(routeWithHeaders("b", "prod", "api.example.com", "PathPrefix", "/api",
		[]mapper.HeaderMatchInput{{Name: "X-Env", Value: "production", Type: "Exact"}}))
	if len(overlaps) == 0 {
		t.Error("same path+host+headers should overlap")
	}
}

func TestDetector_HeadersVsNoHeaders_NoOverlap(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(route("a", "default", "api.example.com", "PathPrefix", "/api"))
	overlaps := d.Check(routeWithHeaders("b", "prod", "api.example.com", "PathPrefix", "/api",
		[]mapper.HeaderMatchInput{{Name: "X-Env", Value: "sandbox", Type: "Exact"}}))
	if len(overlaps) > 0 {
		t.Error("route with headers vs route without headers should NOT overlap")
	}
}

func TestDetector_NoHeadersVsNoHeaders_Overlap(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(route("a", "default", "api.example.com", "PathPrefix", "/api"))
	overlaps := d.Check(route("b", "prod", "api.example.com", "PathPrefix", "/api"))
	if len(overlaps) == 0 {
		t.Error("both without headers on same path+host should overlap")
	}
}

func TestDetector_MultipleHeaders_DifferentOrder_Overlap(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(routeWithHeaders("a", "default", "api.example.com", "PathPrefix", "/api",
		[]mapper.HeaderMatchInput{
			{Name: "X-Env", Value: "prod", Type: "Exact"},
			{Name: "X-Region", Value: "eu", Type: "Exact"},
		}))
	overlaps := d.Check(routeWithHeaders("b", "prod", "api.example.com", "PathPrefix", "/api",
		[]mapper.HeaderMatchInput{
			{Name: "X-Region", Value: "eu", Type: "Exact"},
			{Name: "X-Env", Value: "prod", Type: "Exact"},
		}))
	if len(overlaps) == 0 {
		t.Error("same headers in different order should still overlap")
	}
}

func TestDetector_MultipleHeaders_OneDiffers_NoOverlap(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(routeWithHeaders("a", "default", "api.example.com", "PathPrefix", "/api",
		[]mapper.HeaderMatchInput{
			{Name: "X-Env", Value: "prod", Type: "Exact"},
			{Name: "X-Region", Value: "eu", Type: "Exact"},
		}))
	overlaps := d.Check(routeWithHeaders("b", "prod", "api.example.com", "PathPrefix", "/api",
		[]mapper.HeaderMatchInput{
			{Name: "X-Env", Value: "prod", Type: "Exact"},
			{Name: "X-Region", Value: "us", Type: "Exact"},
		}))
	if len(overlaps) > 0 {
		t.Error("one differing header value should prevent overlap")
	}
}

func TestDetector_HeaderNameCaseInsensitive(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(routeWithHeaders("a", "default", "api.example.com", "PathPrefix", "/api",
		[]mapper.HeaderMatchInput{{Name: "X-Env", Value: "prod", Type: "Exact"}}))
	overlaps := d.Check(routeWithHeaders("b", "prod", "api.example.com", "PathPrefix", "/api",
		[]mapper.HeaderMatchInput{{Name: "x-env", Value: "prod", Type: "Exact"}}))
	if len(overlaps) == 0 {
		t.Error("header names are case-insensitive in HTTP, should overlap")
	}
}

func TestDetector_HeaderTypeDefaultsToExact(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(routeWithHeaders("a", "default", "api.example.com", "PathPrefix", "/api",
		[]mapper.HeaderMatchInput{{Name: "X-Env", Value: "prod", Type: "Exact"}}))
	overlaps := d.Check(routeWithHeaders("b", "prod", "api.example.com", "PathPrefix", "/api",
		[]mapper.HeaderMatchInput{{Name: "X-Env", Value: "prod", Type: ""}}))
	if len(overlaps) == 0 {
		t.Error("empty Type defaults to Exact, should overlap")
	}
}

func TestPrefixCovers(t *testing.T) {
	tests := []struct {
		prefix, path string
		want         bool
	}{
		{"/api", "/api", true},
		{"/api", "/api/users", true},
		{"/api", "/api/", true},
		{"/api", "/apikeys", false},
		{"/api/", "/api/users", true},
		{"/", "/anything", true},
		{"/", "/", true},
		{"/a", "/b", false},
		{"/api/v1", "/api/v1/users", true},
		{"/api/v1", "/api/v2", false},
	}
	for _, tt := range tests {
		got := prefixCovers(tt.prefix, tt.path)
		if got != tt.want {
			t.Errorf("prefixCovers(%q, %q) = %v, want %v", tt.prefix, tt.path, got, tt.want)
		}
	}
}

func TestSameHeaders(t *testing.T) {
	tests := []struct {
		name string
		a, b []mapper.HeaderMatchInput
		want bool
	}{
		{"both empty", nil, nil, true},
		{"both empty slices", []mapper.HeaderMatchInput{}, []mapper.HeaderMatchInput{}, true},
		{"one empty", nil, []mapper.HeaderMatchInput{{Name: "X", Value: "1"}}, false},
		{"same single", []mapper.HeaderMatchInput{{Name: "X", Value: "1", Type: "Exact"}}, []mapper.HeaderMatchInput{{Name: "X", Value: "1", Type: "Exact"}}, true},
		{"different value", []mapper.HeaderMatchInput{{Name: "X", Value: "1"}}, []mapper.HeaderMatchInput{{Name: "X", Value: "2"}}, false},
		{"different name", []mapper.HeaderMatchInput{{Name: "X", Value: "1"}}, []mapper.HeaderMatchInput{{Name: "Y", Value: "1"}}, false},
		{"different count", []mapper.HeaderMatchInput{{Name: "X", Value: "1"}, {Name: "Y", Value: "2"}}, []mapper.HeaderMatchInput{{Name: "X", Value: "1"}}, false},
		{"same reordered", []mapper.HeaderMatchInput{{Name: "A", Value: "1"}, {Name: "B", Value: "2"}}, []mapper.HeaderMatchInput{{Name: "B", Value: "2"}, {Name: "A", Value: "1"}}, true},
		{"case insensitive name", []mapper.HeaderMatchInput{{Name: "X-Env", Value: "prod"}}, []mapper.HeaderMatchInput{{Name: "x-env", Value: "prod"}}, true},
		{"type default", []mapper.HeaderMatchInput{{Name: "X", Value: "1", Type: ""}}, []mapper.HeaderMatchInput{{Name: "X", Value: "1", Type: "Exact"}}, true},
		{"different type same name value", []mapper.HeaderMatchInput{{Name: "X", Value: "v.*", Type: "RegularExpression"}}, []mapper.HeaderMatchInput{{Name: "X", Value: "v.*", Type: "Exact"}}, false},
		{"value is case sensitive", []mapper.HeaderMatchInput{{Name: "X", Value: "Prod"}}, []mapper.HeaderMatchInput{{Name: "X", Value: "prod"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sameHeaders(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("sameHeaders() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- Detector integration-style tests ---
// These exercise the full Check → findOverlaps → entriesOverlap → sameHeaders
// chain with realistic multi-rule, multi-hostname scenarios.

// TestDetector_SamePathDifferentHeaders_MultipleHostnames verifies that header
// disambiguation works correctly when the routes share multiple hostnames.
func TestDetector_SamePathDifferentHeaders_MultipleHostnames(t *testing.T) {
	d := NewDetector(testLogger())
	sandbox := mapper.HTTPRouteInput{
		Name: "sandbox", Namespace: "default",
		Hostnames: []string{"api.example.com", "api.staging.example.com"},
		Rules: []mapper.RuleInput{{
			Matches: []mapper.MatchInput{{
				PathType: "PathPrefix", PathValue: "/api",
				Headers: []mapper.HeaderMatchInput{{Name: "X-Env", Value: "sandbox", Type: "Exact"}},
			}},
		}},
	}
	production := mapper.HTTPRouteInput{
		Name: "production", Namespace: "default",
		Hostnames: []string{"api.example.com", "api.staging.example.com"},
		Rules: []mapper.RuleInput{{
			Matches: []mapper.MatchInput{{
				PathType: "PathPrefix", PathValue: "/api",
				Headers: []mapper.HeaderMatchInput{{Name: "X-Env", Value: "production", Type: "Exact"}},
			}},
		}},
	}
	d.Check(sandbox)
	overlaps := d.Check(production)
	if len(overlaps) > 0 {
		t.Error("same path+hostnames but different X-Env header should NOT overlap")
	}
}

// TestDetector_SamePathSameHeaders_MultipleHostnames verifies that routes with
// identical header matchers across multiple hostnames ARE detected as overlaps.
func TestDetector_SamePathSameHeaders_MultipleHostnames(t *testing.T) {
	d := NewDetector(testLogger())
	a := mapper.HTTPRouteInput{
		Name: "a", Namespace: "ns-a",
		Hostnames: []string{"api.example.com", "api.staging.example.com"},
		Rules: []mapper.RuleInput{{
			Matches: []mapper.MatchInput{{
				PathType: "PathPrefix", PathValue: "/api",
				Headers: []mapper.HeaderMatchInput{{Name: "X-Env", Value: "prod"}},
			}},
		}},
	}
	b := mapper.HTTPRouteInput{
		Name: "b", Namespace: "ns-b",
		Hostnames: []string{"api.example.com"},
		Rules: []mapper.RuleInput{{
			Matches: []mapper.MatchInput{{
				PathType: "PathPrefix", PathValue: "/api",
				Headers: []mapper.HeaderMatchInput{{Name: "X-Env", Value: "prod"}},
			}},
		}},
	}
	d.Check(a)
	overlaps := d.Check(b)
	if len(overlaps) == 0 {
		t.Error("same path+header on shared hostname api.example.com should overlap")
	}
}

// TestDetector_MultiRuleRoute_PartialOverlap verifies that only the conflicting
// rules within a multi-rule route produce overlaps, not the entire route.
func TestDetector_MultiRuleRoute_PartialOverlap(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(route("existing", "default", "api.example.com", "PathPrefix", "/api"))

	multiRule := mapper.HTTPRouteInput{
		Name: "multi", Namespace: "prod",
		Hostnames: []string{"api.example.com"},
		Rules: []mapper.RuleInput{
			{Matches: []mapper.MatchInput{{PathType: "PathPrefix", PathValue: "/api"}}},
			{Matches: []mapper.MatchInput{{PathType: "PathPrefix", PathValue: "/health"}}},
		},
	}
	overlaps := d.Check(multiRule)
	if len(overlaps) != 1 {
		t.Errorf("expected exactly 1 overlap (on /api), got %d", len(overlaps))
	}
}

// TestDetector_RemoveWithHeaders verifies that Remove correctly unregisters
// header-bearing entries so subsequent checks no longer detect overlaps.
func TestDetector_RemoveWithHeaders(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(routeWithHeaders("a", "default", "api.example.com", "PathPrefix", "/api",
		[]mapper.HeaderMatchInput{{Name: "X-Env", Value: "prod"}}))
	d.Remove("default", "a")
	overlaps := d.Check(routeWithHeaders("b", "prod", "api.example.com", "PathPrefix", "/api",
		[]mapper.HeaderMatchInput{{Name: "X-Env", Value: "prod"}}))
	if len(overlaps) > 0 {
		t.Error("after remove, should not overlap even with same headers")
	}
}

// TestDetector_ResetClearsHeaders verifies that Reset drops all entries
// including their header state.
func TestDetector_ResetClearsHeaders(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(routeWithHeaders("a", "default", "api.example.com", "PathPrefix", "/api",
		[]mapper.HeaderMatchInput{{Name: "X-Env", Value: "prod"}}))
	d.Reset()
	overlaps := d.Check(routeWithHeaders("b", "prod", "api.example.com", "PathPrefix", "/api",
		[]mapper.HeaderMatchInput{{Name: "X-Env", Value: "prod"}}))
	if len(overlaps) > 0 {
		t.Error("after reset, should not overlap")
	}
}

// --- Method-aware tests ---

// TestDetector_DifferentMethods_NoOverlap verifies that two routes with the
// same hostname and path but different methods are NOT considered overlapping.
func TestDetector_DifferentMethods_NoOverlap(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(routeWithMethod("a", "default", "api.example.com", "PathPrefix", "/api", "GET"))
	overlaps := d.Check(routeWithMethod("b", "prod", "api.example.com", "PathPrefix", "/api", "POST"))
	if len(overlaps) > 0 {
		t.Error("same path+host but different methods (GET vs POST) should NOT overlap")
	}
}

// TestDetector_SameMethods_Overlap verifies that two routes with the same
// hostname, path, and method ARE detected as overlapping.
func TestDetector_SameMethods_Overlap(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(routeWithMethod("a", "default", "api.example.com", "PathPrefix", "/api", "GET"))
	overlaps := d.Check(routeWithMethod("b", "prod", "api.example.com", "PathPrefix", "/api", "GET"))
	if len(overlaps) == 0 {
		t.Error("same path+host+method should overlap")
	}
}

// TestDetector_MethodVsNoMethod_Overlap verifies that a route restricted to
// GET overlaps with a route that accepts all methods (empty = all).
func TestDetector_MethodVsNoMethod_Overlap(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(route("a", "default", "api.example.com", "PathPrefix", "/api"))
	overlaps := d.Check(routeWithMethod("b", "prod", "api.example.com", "PathPrefix", "/api", "GET"))
	if len(overlaps) == 0 {
		t.Error("route with no method (all) should overlap with a specific method")
	}
}

// TestDetector_NoMethodVsNoMethod_Overlap verifies that two routes with no
// method restriction (both = all methods) overlap normally.
func TestDetector_NoMethodVsNoMethod_Overlap(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(route("a", "default", "api.example.com", "PathPrefix", "/api"))
	overlaps := d.Check(route("b", "prod", "api.example.com", "PathPrefix", "/api"))
	if len(overlaps) == 0 {
		t.Error("both with no method restriction should overlap")
	}
}

// TestDetector_MethodCaseInsensitive verifies that method comparison is
// case-insensitive (GET == get).
func TestDetector_MethodCaseInsensitive(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(routeWithMethod("a", "default", "api.example.com", "PathPrefix", "/api", "GET"))
	overlaps := d.Check(routeWithMethod("b", "prod", "api.example.com", "PathPrefix", "/api", "get"))
	if len(overlaps) == 0 {
		t.Error("GET and get should be the same method, should overlap")
	}
}

// TestDetector_MethodAndHeadersCombined verifies that method AND headers are
// both checked: same path+host, different method but same headers → no overlap.
func TestDetector_MethodAndHeadersCombined_DifferentMethod(t *testing.T) {
	d := NewDetector(testLogger())
	headers := []mapper.HeaderMatchInput{{Name: "X-Env", Value: "prod"}}
	d.Check(routeWithMethodAndHeaders("a", "default", "api.example.com", "PathPrefix", "/api", "GET", headers))
	overlaps := d.Check(routeWithMethodAndHeaders("b", "prod", "api.example.com", "PathPrefix", "/api", "POST", headers))
	if len(overlaps) > 0 {
		t.Error("same headers but different methods should NOT overlap")
	}
}

// TestDetector_MethodAndHeadersCombined_DifferentHeaders verifies that
// same method but different headers → no overlap.
func TestDetector_MethodAndHeadersCombined_DifferentHeaders(t *testing.T) {
	d := NewDetector(testLogger())
	d.Check(routeWithMethodAndHeaders("a", "default", "api.example.com", "PathPrefix", "/api", "GET",
		[]mapper.HeaderMatchInput{{Name: "X-Env", Value: "sandbox"}}))
	overlaps := d.Check(routeWithMethodAndHeaders("b", "prod", "api.example.com", "PathPrefix", "/api", "GET",
		[]mapper.HeaderMatchInput{{Name: "X-Env", Value: "production"}}))
	if len(overlaps) > 0 {
		t.Error("same method but different headers should NOT overlap")
	}
}

// TestDetector_MethodAndHeadersCombined_AllSame verifies that same method +
// same headers + same path → overlap.
func TestDetector_MethodAndHeadersCombined_AllSame(t *testing.T) {
	d := NewDetector(testLogger())
	headers := []mapper.HeaderMatchInput{{Name: "X-Env", Value: "prod"}}
	d.Check(routeWithMethodAndHeaders("a", "default", "api.example.com", "PathPrefix", "/api", "GET", headers))
	overlaps := d.Check(routeWithMethodAndHeaders("b", "prod", "api.example.com", "PathPrefix", "/api", "GET", headers))
	if len(overlaps) == 0 {
		t.Error("same method + same headers + same path should overlap")
	}
}

// TestMethodsOverlap is a table-driven test for the methodsOverlap function.
func TestMethodsOverlap(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{"both empty", "", "", true},
		{"a empty b GET", "", "GET", true},
		{"a GET b empty", "GET", "", true},
		{"same GET", "GET", "GET", true},
		{"same POST", "POST", "POST", true},
		{"different", "GET", "POST", false},
		{"case insensitive", "GET", "get", true},
		{"DELETE vs PUT", "DELETE", "PUT", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := methodsOverlap(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("methodsOverlap(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
