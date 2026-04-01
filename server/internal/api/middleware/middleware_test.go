// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/achetronic/vrata/internal/config"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestLoggerMiddleware(t *testing.T) {
	handler := Logger(testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/test", nil))

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "ok" {
		t.Errorf("expected body 'ok', got %q", w.Body.String())
	}
}

func TestRecoveryMiddleware(t *testing.T) {
	handler := Recovery(testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/test", nil))

	if w.Code != 500 {
		t.Errorf("expected 500 after panic, got %d", w.Code)
	}
}

func TestRecoveryMiddlewareNoPanic(t *testing.T) {
	handler := Recovery(testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAuthNoKeysIsNoop(t *testing.T) {
	handler := Auth(nil, testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))

	if w.Code != 200 {
		t.Errorf("expected 200 (no auth configured), got %d", w.Code)
	}
}

func TestAuthMissingHeader(t *testing.T) {
	keys := []config.APIKeyEntry{{Name: "test", Key: "secret123"}}
	handler := Auth(keys, testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))

	if w.Code != 401 {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuthInvalidScheme(t *testing.T) {
	keys := []config.APIKeyEntry{{Name: "test", Key: "secret123"}}
	handler := Auth(keys, testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	handler.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("expected 401, got %d", w.Code)
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["error"] != "authorization header must use Bearer scheme" {
		t.Errorf("unexpected error: %s", body["error"])
	}
}

func TestAuthInvalidKey(t *testing.T) {
	keys := []config.APIKeyEntry{{Name: "test", Key: "secret123"}}
	handler := Auth(keys, testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	handler.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuthValidKey(t *testing.T) {
	keys := []config.APIKeyEntry{{Name: "proxy", Key: "secret123"}}
	called := false
	handler := Auth(keys, testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer secret123")
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !called {
		t.Error("expected handler to be called")
	}
}
