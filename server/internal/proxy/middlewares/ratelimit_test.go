package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/achetronic/rutoso/internal/model"
)

func TestRateLimitAllows(t *testing.T) {
	cfg := &model.RateLimitConfig{
		RequestsPerSecond: 100,
		Burst:             100,
	}

	handler := RateLimitMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "1.2.3.4:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("first request should pass, got %d", w.Code)
	}
}

func TestRateLimitBlocks(t *testing.T) {
	cfg := &model.RateLimitConfig{
		RequestsPerSecond: 1,
		Burst:             1,
	}

	handler := RateLimitMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request passes.
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "1.2.3.4:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("first request should pass, got %d", w.Code)
	}

	// Second request should be rate limited.
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, r)

	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("second request should be rate limited, got %d", w2.Code)
	}
}
