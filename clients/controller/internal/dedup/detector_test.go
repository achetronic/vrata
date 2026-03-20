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
