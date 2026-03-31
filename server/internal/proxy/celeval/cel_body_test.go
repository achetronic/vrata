// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package celeval

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBufferBody_JSONParsed(t *testing.T) {
	body := `{"method":"tools/call","params":{"name":"add"}}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	req, data := BufferBody(req, 65536)

	if data.Raw != body {
		t.Errorf("raw: got %q, want %q", data.Raw, body)
	}
	if data.JSON == nil {
		t.Fatal("json should be populated for application/json")
	}
	if data.JSON["method"] != "tools/call" {
		t.Errorf("json.method: got %v, want tools/call", data.JSON["method"])
	}
	params, ok := data.JSON["params"].(map[string]any)
	if !ok {
		t.Fatal("json.params should be a map")
	}
	if params["name"] != "add" {
		t.Errorf("json.params.name: got %v, want add", params["name"])
	}

	// Body should still be readable for upstream.
	remaining, _ := io.ReadAll(req.Body)
	if string(remaining) != body {
		t.Errorf("body re-read: got %q, want %q", string(remaining), body)
	}
}

func TestBufferBody_NonJSON(t *testing.T) {
	body := "plain text body"
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")

	_, data := BufferBody(req, 65536)

	if data.Raw != body {
		t.Errorf("raw: got %q, want %q", data.Raw, body)
	}
	if data.JSON != nil {
		t.Error("json should be nil for non-JSON content type")
	}
}

func TestBufferBody_InvalidJSON(t *testing.T) {
	body := `{"broken json`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	_, data := BufferBody(req, 65536)

	if data.Raw != body {
		t.Errorf("raw: got %q, want %q", data.Raw, body)
	}
	if data.JSON != nil {
		t.Error("json should be nil for invalid JSON")
	}
}

func TestBufferBody_EmptyBody(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)

	_, data := BufferBody(req, 65536)

	if data.Raw != "" {
		t.Errorf("raw: got %q, want empty", data.Raw)
	}
	if data.JSON != nil {
		t.Error("json should be nil for empty body")
	}
}

func TestBufferBody_ExceedsMaxSize(t *testing.T) {
	body := strings.Repeat("x", 200)
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	_, data := BufferBody(req, 100)

	if len(data.Raw) != 100 {
		t.Errorf("raw length: got %d, want 100 (truncated)", len(data.Raw))
	}
	if data.JSON != nil {
		t.Error("json should be nil when body exceeds max size")
	}
}

func TestBufferBody_CachedOnSecondCall(t *testing.T) {
	body := `{"key":"value"}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	req, data1 := BufferBody(req, 65536)
	_, data2 := BufferBody(req, 65536)

	if data1 != data2 {
		t.Error("second BufferBody call should return cached data")
	}
}

func TestBufferBody_JSONWithCharset(t *testing.T) {
	body := `{"ok":true}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	_, data := BufferBody(req, 65536)

	if data.JSON == nil {
		t.Error("json should be populated for application/json with charset")
	}
}

func TestBufferBody_NumericPrecision(t *testing.T) {
	body := `{"big":12345678901234567890}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	_, data := BufferBody(req, 65536)

	if data.JSON == nil {
		t.Fatal("json should be populated")
	}
	big := data.JSON["big"]
	if big == nil {
		t.Fatal("big should be present")
	}
	// json.Number preserves precision as a string representation.
	num, ok := big.(json.Number)
	if !ok {
		t.Fatalf("big should be json.Number, got %T", big)
	}
	if num.String() != "12345678901234567890" {
		t.Errorf("big value: got %q, want 12345678901234567890", num.String())
	}
}

func TestNeedsBody(t *testing.T) {
	tests := []struct {
		expr string
		want bool
	}{
		{`request.method == "GET"`, false},
		{`request.path.startsWith("/api")`, false},
		{`request.body.raw.contains("test")`, true},
		{`request.body.json.method == "tools/call"`, true},
		{`request.method == "POST" && request.body.json.method == "tools/call"`, true},
		{`"body" in request.headers`, false},
	}

	for _, tt := range tests {
		prg, err := Compile(tt.expr)
		if err != nil {
			t.Errorf("compile %q: %v", tt.expr, err)
			continue
		}
		if prg.NeedsBody() != tt.want {
			t.Errorf("NeedsBody(%q): got %v, want %v", tt.expr, prg.NeedsBody(), tt.want)
		}
	}
}

func TestEvalBodyJSON_FieldAccess(t *testing.T) {
	prg, err := Compile(`has(request.body) && has(request.body.json) && request.body.json.method == "tools/call"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	body := `{"method":"tools/call"}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req, _ = BufferBody(req, 65536)

	if !prg.Eval(req) {
		t.Error("expected true for matching JSON body")
	}
}

func TestEvalBodyJSON_NestedAccess(t *testing.T) {
	prg, err := Compile(`has(request.body) && has(request.body.json) && request.body.json.params.name == "add"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	body := `{"method":"tools/call","params":{"name":"add"}}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req, _ = BufferBody(req, 65536)

	if !prg.Eval(req) {
		t.Error("expected true for nested field access")
	}
}

func TestEvalBodyJSON_InList(t *testing.T) {
	prg, err := Compile(`has(request.body) && has(request.body.json) && request.body.json.params.name in ["add", "subtract"]`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	tests := []struct {
		body string
		want bool
	}{
		{`{"params":{"name":"add"}}`, true},
		{`{"params":{"name":"subtract"}}`, true},
		{`{"params":{"name":"delete"}}`, false},
	}

	for _, tt := range tests {
		req := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
		req.Header.Set("Content-Type", "application/json")
		req, _ = BufferBody(req, 65536)
		got := prg.Eval(req)
		if got != tt.want {
			t.Errorf("body=%q: got %v, want %v", tt.body, got, tt.want)
		}
	}
}

func TestEvalBodyJSON_MissingField(t *testing.T) {
	prg, err := Compile(`has(request.body) && has(request.body.json) && request.body.json.method == "test"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	body := `{"other":"field"}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req, _ = BufferBody(req, 65536)

	if prg.Eval(req) {
		t.Error("expected false — accessing missing field should not match")
	}
}

func TestEvalBodyRaw_Contains(t *testing.T) {
	prg, err := Compile(`has(request.body) && request.body.raw.contains("ERROR")`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	req := httptest.NewRequest("POST", "/", strings.NewReader("some ERROR occurred"))
	req.Header.Set("Content-Type", "text/plain")
	req, _ = BufferBody(req, 65536)

	if !prg.Eval(req) {
		t.Error("expected true for raw body containing ERROR")
	}

	req = httptest.NewRequest("POST", "/", strings.NewReader("all good"))
	req.Header.Set("Content-Type", "text/plain")
	req, _ = BufferBody(req, 65536)

	if prg.Eval(req) {
		t.Error("expected false for raw body without ERROR")
	}
}

func TestEvalBodyJSON_WithoutBuffering(t *testing.T) {
	prg, err := Compile(`has(request.body) && has(request.body.json) && request.body.json.method == "test"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"method":"test"}`))
	req.Header.Set("Content-Type", "application/json")

	if prg.Eval(req) {
		t.Error("expected false — body not buffered, request.body should not exist")
	}
}

func TestEvalNoBody_DoesNotReadBody(t *testing.T) {
	prg, err := Compile(`request.method == "GET"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	body := "should not be read"
	req := httptest.NewRequest("GET", "/", strings.NewReader(body))

	if !prg.Eval(req) {
		t.Error("expected true for GET method")
	}

	remaining, _ := io.ReadAll(req.Body)
	if string(remaining) != body {
		t.Error("body should not have been consumed — CEL does not reference request.body")
	}
}

func TestBodyFromCtx_NilWhenNotBuffered(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	data := BodyFromCtx(req)
	if data != nil {
		t.Error("expected nil when BufferBody has not been called")
	}
}

func TestBufferBody_NoBody(t *testing.T) {
	req := httptest.NewRequest("GET", "/", http.NoBody)

	_, data := BufferBody(req, 65536)

	if data.Raw != "" {
		t.Errorf("raw: got %q, want empty for http.NoBody", data.Raw)
	}
}
