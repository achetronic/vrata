// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/achetronic/vrata/internal/model"
)

func TestHeadersAddRequest(t *testing.T) {
	cfg := &model.HeadersConfig{
		RequestHeadersToAdd: []model.HeaderValue{
			{Key: "X-Custom", Value: "added", Append: false},
		},
	}

	var gotHeader string
	handler := HeadersMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Custom")
	}))

	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if gotHeader != "added" {
		t.Errorf("request header = %q, want added", gotHeader)
	}
}

func TestHeadersRemoveRequest(t *testing.T) {
	cfg := &model.HeadersConfig{
		RequestHeadersToRemove: []string{"X-Remove-Me"},
	}

	var gotHeader string
	handler := HeadersMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Remove-Me")
	}))

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Remove-Me", "should-be-gone")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if gotHeader != "" {
		t.Errorf("header should be removed, got %q", gotHeader)
	}
}

func TestHeadersAddResponse(t *testing.T) {
	cfg := &model.HeadersConfig{
		ResponseHeadersToAdd: []model.HeaderValue{
			{Key: "X-Frame-Options", Value: "DENY", Append: false},
		},
	}

	handler := HeadersMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("response header = %q, want DENY", got)
	}
}
