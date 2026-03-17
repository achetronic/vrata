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
