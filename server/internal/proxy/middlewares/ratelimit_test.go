package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/achetronic/vrata/internal/model"
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

func TestRateLimitIgnoresUntrustedXFF(t *testing.T) {
	cfg := &model.RateLimitConfig{
		RequestsPerSecond: 1,
		Burst:             1,
	}

	handler := RateLimitMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "1.2.3.4:1234"
	r.Header.Set("X-Forwarded-For", "9.9.9.9")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("first: %d", w.Code)
	}

	r2 := httptest.NewRequest("GET", "/", nil)
	r2.RemoteAddr = "1.2.3.4:1234"
	r2.Header.Set("X-Forwarded-For", "8.8.8.8")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, r2)

	if w2.Code != 429 {
		t.Errorf("untrusted XFF should be ignored, same IP should be limited: got %d", w2.Code)
	}
}

func TestRateLimitTrustsConfiguredProxy(t *testing.T) {
	cfg := &model.RateLimitConfig{
		RequestsPerSecond: 1,
		Burst:             1,
		TrustedProxies:    []string{"10.0.0.0/8"},
	}

	handler := RateLimitMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("first: %d", w.Code)
	}

	r2 := httptest.NewRequest("GET", "/", nil)
	r2.RemoteAddr = "10.0.0.1:1234"
	r2.Header.Set("X-Forwarded-For", "5.6.7.8")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, r2)

	if w2.Code != 200 {
		t.Errorf("different XFF from trusted proxy should be different key: got %d", w2.Code)
	}
}
