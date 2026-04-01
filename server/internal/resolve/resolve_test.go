// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package resolve

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/achetronic/vrata/internal/model"
	memstore "github.com/achetronic/vrata/internal/store/memory"
)

func TestResolveValueSource(t *testing.T) {
	st := memstore.New()
	st.SaveSecret(context.Background(), model.Secret{ID: "s1", Name: "test", Value: "resolved-value"})

	input := []byte(`{"field":"{{secret:value:s1}}"}`)
	out, err := Secrets(context.Background(), st, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != `{"field":"resolved-value"}` {
		t.Errorf("expected resolved value, got %s", out)
	}
}

func TestResolveEnvSource(t *testing.T) {
	st := memstore.New()
	t.Setenv("TEST_SECRET_VAR", "env-value")

	input := []byte(`{"key":"{{secret:env:TEST_SECRET_VAR}}"}`)
	out, err := Secrets(context.Background(), st, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != `{"key":"env-value"}` {
		t.Errorf("expected env value, got %s", out)
	}
}

func TestResolveFileSource(t *testing.T) {
	st := memstore.New()
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.txt")
	os.WriteFile(path, []byte("file-content"), 0644)

	input := []byte(`{"data":"{{secret:file:` + path + `}}"}`)
	out, err := Secrets(context.Background(), st, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != `{"data":"file-content"}` {
		t.Errorf("expected file content, got %s", out)
	}
}

func TestResolveMultiplePatterns(t *testing.T) {
	st := memstore.New()
	st.SaveSecret(context.Background(), model.Secret{ID: "cert", Name: "cert", Value: "CERT_PEM"})
	st.SaveSecret(context.Background(), model.Secret{ID: "key", Name: "key", Value: "KEY_PEM"})

	input := []byte(`{"cert":"{{secret:value:cert}}","key":"{{secret:value:key}}"}`)
	out, err := Secrets(context.Background(), st, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != `{"cert":"CERT_PEM","key":"KEY_PEM"}` {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestResolveNoPatterns(t *testing.T) {
	st := memstore.New()
	input := []byte(`{"plain":"value"}`)
	out, err := Secrets(context.Background(), st, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != string(input) {
		t.Errorf("expected unchanged input, got %s", out)
	}
}

func TestResolveMissingSecretFails(t *testing.T) {
	st := memstore.New()
	input := []byte(`{"cert":"{{secret:value:nonexistent}}"}`)
	_, err := Secrets(context.Background(), st, input)
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
}

func TestResolveMissingEnvFails(t *testing.T) {
	st := memstore.New()
	os.Unsetenv("DEFINITELY_NOT_SET_12345")
	input := []byte(`{"key":"{{secret:env:DEFINITELY_NOT_SET_12345}}"}`)
	_, err := Secrets(context.Background(), st, input)
	if err == nil {
		t.Fatal("expected error for unset env var")
	}
}

func TestResolveMissingFileFails(t *testing.T) {
	st := memstore.New()
	input := []byte(`{"ca":"{{secret:file:/nonexistent/path}}"}`)
	_, err := Secrets(context.Background(), st, input)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestResolveEscapesNewlines(t *testing.T) {
	st := memstore.New()
	st.SaveSecret(context.Background(), model.Secret{
		ID: "pem", Name: "pem",
		Value: "-----BEGIN CERT-----\ndata\n-----END CERT-----",
	})

	input := []byte(`{"cert":"{{secret:value:pem}}"}`)
	out, err := Secrets(context.Background(), st, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := `{"cert":"-----BEGIN CERT-----\ndata\n-----END CERT-----"}`
	if string(out) != expected {
		t.Errorf("expected %s, got %s", expected, out)
	}
}

func TestResolveEscapesQuotes(t *testing.T) {
	st := memstore.New()
	st.SaveSecret(context.Background(), model.Secret{
		ID: "json", Name: "json",
		Value: `{"nested":"value"}`,
	})

	input := []byte(`{"data":"{{secret:value:json}}"}`)
	out, err := Secrets(context.Background(), st, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := `{"data":"{\"nested\":\"value\"}"}`
	if string(out) != expected {
		t.Errorf("expected %s, got %s", expected, out)
	}
}

func TestResolveMultipleErrorsReported(t *testing.T) {
	st := memstore.New()
	input := []byte(`{"a":"{{secret:value:x}}","b":"{{secret:value:y}}"}`)
	_, err := Secrets(context.Background(), st, input)
	if err == nil {
		t.Fatal("expected error")
	}
	errStr := err.Error()
	if !containsAll(errStr, "x", "y") {
		t.Errorf("expected both missing secrets reported, got: %s", errStr)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
