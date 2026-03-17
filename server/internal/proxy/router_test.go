package proxy

import (
	"net/http/httptest"
	"testing"

	"github.com/achetronic/rutoso/internal/model"
)

func TestRouterMatchesPath(t *testing.T) {
	router := NewRouter()

	table, err := BuildTable(
		[]model.Route{
			{
				ID:   "r1",
				Name: "test",
				Match: model.MatchRule{PathPrefix: "/api"},
				DirectResponse: &model.RouteDirectResponse{Status: 200, Body: "ok"},
			},
		},
		nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	router.SwapTable(table)

	// Should match.
	r := httptest.NewRequest("GET", "/api/foo", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Errorf("/api/foo should match, got %d", w.Code)
	}

	// Should not match.
	r2 := httptest.NewRequest("GET", "/other", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, r2)
	if w2.Code != 404 {
		t.Errorf("/other should 404, got %d", w2.Code)
	}
}

func TestRouterMatchesMethods(t *testing.T) {
	router := NewRouter()

	table, _ := BuildTable(
		[]model.Route{
			{
				ID:   "r1",
				Match: model.MatchRule{PathPrefix: "/", Methods: []string{"POST"}},
				DirectResponse: &model.RouteDirectResponse{Status: 201},
			},
		},
		nil, nil, nil,
	)
	router.SwapTable(table)

	// POST should match.
	r := httptest.NewRequest("POST", "/foo", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 201 {
		t.Errorf("POST should match, got %d", w.Code)
	}

	// GET should not match.
	r2 := httptest.NewRequest("GET", "/foo", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, r2)
	if w2.Code != 404 {
		t.Errorf("GET should 404, got %d", w2.Code)
	}
}

func TestRouterGroupComposition(t *testing.T) {
	router := NewRouter()

	table, _ := BuildTable(
		[]model.Route{
			{
				ID:   "r1",
				Match: model.MatchRule{PathPrefix: "/app"},
				DirectResponse: &model.RouteDirectResponse{Status: 200, Body: "ok"},
			},
		},
		[]model.RouteGroup{
			{
				ID:        "g1",
				PathRegex: "/(en|es)",
				RouteIDs:  []string{"r1"},
			},
		},
		nil, nil,
	)
	router.SwapTable(table)

	// /en/app should match.
	r := httptest.NewRequest("GET", "/en/app", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Errorf("/en/app should match, got %d", w.Code)
	}

	// /fr/app should not.
	r2 := httptest.NewRequest("GET", "/fr/app", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, r2)
	if w2.Code != 404 {
		t.Errorf("/fr/app should 404, got %d", w2.Code)
	}
}

func TestRouterStandaloneRoute(t *testing.T) {
	router := NewRouter()

	table, _ := BuildTable(
		[]model.Route{
			{
				ID:   "r1",
				Match: model.MatchRule{PathPrefix: "/health"},
				DirectResponse: &model.RouteDirectResponse{Status: 200, Body: "healthy"},
			},
		},
		nil, nil, nil,
	)
	router.SwapTable(table)

	r := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != "healthy" {
		t.Errorf("standalone route: got %d %q", w.Code, w.Body.String())
	}
}

func TestRouterAtomicSwap(t *testing.T) {
	router := NewRouter()

	// Initially empty — should 404.
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Errorf("empty router should 404, got %d", w.Code)
	}

	// Swap in a route.
	table, _ := BuildTable(
		[]model.Route{
			{
				ID:   "r1",
				Match: model.MatchRule{PathPrefix: "/"},
				DirectResponse: &model.RouteDirectResponse{Status: 200},
			},
		},
		nil, nil, nil,
	)
	router.SwapTable(table)

	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, r)
	if w2.Code != 200 {
		t.Errorf("after swap should 200, got %d", w2.Code)
	}
}

func TestRouterCELOnlyMatch(t *testing.T) {
	router := NewRouter()

	table, err := BuildTable(
		[]model.Route{
			{
				ID:   "r1",
				Match: model.MatchRule{
					PathPrefix: "/",
					CEL:        `request.method == "POST" && "x-api-key" in request.headers`,
				},
				DirectResponse: &model.RouteDirectResponse{Status: 200, Body: "cel-ok"},
			},
		},
		nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	router.SwapTable(table)

	// POST with header — match.
	r := httptest.NewRequest("POST", "/anything", nil)
	r.Header.Set("X-Api-Key", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != "cel-ok" {
		t.Errorf("CEL match: got %d %q", w.Code, w.Body.String())
	}

	// GET with header — CEL fails (method).
	r2 := httptest.NewRequest("GET", "/anything", nil)
	r2.Header.Set("X-Api-Key", "secret")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, r2)
	if w2.Code != 404 {
		t.Errorf("CEL method mismatch: got %d, want 404", w2.Code)
	}

	// POST without header — CEL fails (missing header).
	r3 := httptest.NewRequest("POST", "/anything", nil)
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, r3)
	if w3.Code != 404 {
		t.Errorf("CEL header mismatch: got %d, want 404", w3.Code)
	}
}

func TestRouterCELWithStaticMatchers(t *testing.T) {
	router := NewRouter()

	table, err := BuildTable(
		[]model.Route{
			{
				ID: "r1",
				Match: model.MatchRule{
					PathPrefix: "/api",
					Methods:    []string{"GET"},
					CEL:        `"debug" in request.queryParams && request.queryParams["debug"] == "1"`,
				},
				DirectResponse: &model.RouteDirectResponse{Status: 200, Body: "debug-on"},
			},
		},
		nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	router.SwapTable(table)

	// All matchers pass.
	r := httptest.NewRequest("GET", "/api/test?debug=1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Errorf("all match: got %d, want 200", w.Code)
	}

	// Static passes, CEL fails (no debug param).
	r2 := httptest.NewRequest("GET", "/api/test", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, r2)
	if w2.Code != 404 {
		t.Errorf("CEL fail: got %d, want 404", w2.Code)
	}

	// Static fails (wrong method), CEL would pass.
	r3 := httptest.NewRequest("POST", "/api/test?debug=1", nil)
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, r3)
	if w3.Code != 404 {
		t.Errorf("static fail: got %d, want 404", w3.Code)
	}

	// Static fails (wrong path).
	r4 := httptest.NewRequest("GET", "/web/test?debug=1", nil)
	w4 := httptest.NewRecorder()
	router.ServeHTTP(w4, r4)
	if w4.Code != 404 {
		t.Errorf("path fail: got %d, want 404", w4.Code)
	}
}

func TestRouterCELInvalidExpressionSkipsRoute(t *testing.T) {
	router := NewRouter()

	table, err := BuildTable(
		[]model.Route{
			{
				ID:   "r-bad",
				Name: "bad-cel",
				Match: model.MatchRule{PathPrefix: "/bad", CEL: "not valid cel !!!"},
				DirectResponse: &model.RouteDirectResponse{Status: 200},
			},
			{
				ID:   "r-good",
				Name: "good",
				Match: model.MatchRule{PathPrefix: "/good"},
				DirectResponse: &model.RouteDirectResponse{Status: 200, Body: "ok"},
			},
		},
		nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("BuildTable should not fail, got: %v", err)
	}
	router.SwapTable(table)

	// Good route still works.
	r := httptest.NewRequest("GET", "/good", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != "ok" {
		t.Errorf("good route: got %d %q, want 200 ok", w.Code, w.Body.String())
	}

	// Bad route was skipped.
	r2 := httptest.NewRequest("GET", "/bad", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, r2)
	if w2.Code != 404 {
		t.Errorf("bad CEL route should be skipped (404), got %d", w2.Code)
	}
}
