// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package celeval

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompileAndEvalSimplePath(t *testing.T) {
	prg, err := Compile(`request.path.startsWith("/api")`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/users", nil)
	if !prg.Eval(req) {
		t.Error("expected true for /api/users")
	}

	req = httptest.NewRequest("GET", "/web/home", nil)
	if prg.Eval(req) {
		t.Error("expected false for /web/home")
	}
}

func TestCompileAndEvalMethod(t *testing.T) {
	prg, err := Compile(`request.method == "POST"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	req := httptest.NewRequest("POST", "/submit", nil)
	if !prg.Eval(req) {
		t.Error("expected true for POST")
	}

	req = httptest.NewRequest("GET", "/submit", nil)
	if prg.Eval(req) {
		t.Error("expected false for GET")
	}
}

func TestCompileAndEvalHeaders(t *testing.T) {
	prg, err := Compile(`"admin" in request.headers && request.headers["admin"] == "true"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Admin", "true")
	if !prg.Eval(req) {
		t.Error("expected true with admin=true header")
	}

	req = httptest.NewRequest("GET", "/", nil)
	if prg.Eval(req) {
		t.Error("expected false without admin header")
	}
}

func TestCompileAndEvalQueryParams(t *testing.T) {
	prg, err := Compile(`"debug" in request.queryParams && request.queryParams["debug"] == "1"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	req := httptest.NewRequest("GET", "/test?debug=1", nil)
	if !prg.Eval(req) {
		t.Error("expected true with debug=1")
	}

	req = httptest.NewRequest("GET", "/test?debug=0", nil)
	if prg.Eval(req) {
		t.Error("expected false with debug=0")
	}

	req = httptest.NewRequest("GET", "/test", nil)
	if prg.Eval(req) {
		t.Error("expected false without debug param")
	}
}

func TestCompileAndEvalHost(t *testing.T) {
	prg, err := Compile(`request.host == "example.com"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "example.com:8080"
	if !prg.Eval(req) {
		t.Error("expected true for example.com:8080")
	}

	req = httptest.NewRequest("GET", "/", nil)
	req.Host = "other.com"
	if prg.Eval(req) {
		t.Error("expected false for other.com")
	}
}

func TestCompileAndEvalScheme(t *testing.T) {
	prg, err := Compile(`request.scheme == "http"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	if !prg.Eval(req) {
		t.Error("expected true for http")
	}
}

func TestCompileAndEvalComplex(t *testing.T) {
	prg, err := Compile(`request.path.startsWith("/api") && request.method != "DELETE" && "x-api-key" in request.headers`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/users", nil)
	req.Header.Set("X-Api-Key", "secret")
	if !prg.Eval(req) {
		t.Error("expected true for GET /api with x-api-key")
	}

	req = httptest.NewRequest("DELETE", "/api/users", nil)
	req.Header.Set("X-Api-Key", "secret")
	if prg.Eval(req) {
		t.Error("expected false for DELETE")
	}

	req = httptest.NewRequest("GET", "/api/users", nil)
	if prg.Eval(req) {
		t.Error("expected false without x-api-key")
	}
}

func TestCompileInvalidExpression(t *testing.T) {
	_, err := Compile(`this is not valid CEL`)
	if err == nil {
		t.Error("expected error for invalid expression")
	}
}

func TestCompileNonBoolExpression(t *testing.T) {
	_, err := Compile(`request.path`)
	if err == nil {
		t.Error("expected error for non-bool expression")
	}
}

func TestEvalClientIp(t *testing.T) {
	prg, err := Compile(`request.clientIp == "192.168.1.1"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	if !prg.Eval(req) {
		t.Error("expected true for RemoteAddr 192.168.1.1")
	}

	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "192.168.1.1, 10.0.0.1")
	if !prg.Eval(req) {
		t.Error("expected true for XFF 192.168.1.1")
	}
}

func TestEvalRegexMatch(t *testing.T) {
	prg, err := Compile(`request.path.matches("^/v[0-9]+/")`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	tests := []struct {
		path string
		want bool
	}{
		{"/v1/users", true},
		{"/v23/items", true},
		{"/api/v1", false},
		{"/va/test", false},
	}

	for _, tt := range tests {
		req := httptest.NewRequest("GET", tt.path, nil)
		got := prg.Eval(req)
		if got != tt.want {
			t.Errorf("path=%q: got %v, want %v", tt.path, got, tt.want)
		}
	}
}

func BenchmarkCELEval(b *testing.B) {
	prg, err := Compile(`request.path.startsWith("/api") && request.method == "GET" && "x-api-key" in request.headers`)
	if err != nil {
		b.Fatal(err)
	}

	req, _ := http.NewRequest("GET", "/api/users", nil)
	req.Header.Set("X-Api-Key", "test")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prg.Eval(req)
	}
}
