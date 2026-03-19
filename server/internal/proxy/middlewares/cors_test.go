// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/achetronic/vrata/internal/model"
)

func TestCORSPreflightAllowed(t *testing.T) {
	cfg := &model.CORSConfig{
		AllowOrigins:    []model.CORSOrigin{{Value: "https://example.com"}},
		AllowMethods:    []string{"GET", "POST"},
		AllowHeaders:    []string{"Authorization"},
		MaxAge:          3600,
		AllowCredentials: true,
	}

	handler := CORSMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("preflight should not reach next handler")
	}))

	r := httptest.NewRequest("OPTIONS", "/", nil)
	r.Header.Set("Origin", "https://example.com")
	r.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Errorf("allow-origin = %q, want https://example.com", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("allow-credentials = %q, want true", got)
	}
	if got := w.Header().Get("Access-Control-Max-Age"); got != "3600" {
		t.Errorf("max-age = %q, want 3600", got)
	}
}

func TestCORSPreflightDenied(t *testing.T) {
	cfg := &model.CORSConfig{
		AllowOrigins: []model.CORSOrigin{{Value: "https://example.com"}},
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := CORSMiddleware(cfg)(next)

	r := httptest.NewRequest("OPTIONS", "/", nil)
	r.Header.Set("Origin", "https://evil.com")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("should not set allow-origin for denied origin, got %q", got)
	}
}

func TestCORSRegexOrigin(t *testing.T) {
	cfg := &model.CORSConfig{
		AllowOrigins: []model.CORSOrigin{{Value: "https://.*\\.example\\.com", Regex: true}},
	}

	handler := CORSMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Origin", "https://sub.example.com")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://sub.example.com" {
		t.Errorf("allow-origin = %q, want https://sub.example.com", got)
	}
}

func TestCORSNoOrigin(t *testing.T) {
	cfg := &model.CORSConfig{
		AllowOrigins: []model.CORSOrigin{{Value: "https://example.com"}},
	}

	called := false
	handler := CORSMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if !called {
		t.Error("handler should be called when no Origin header")
	}
}
