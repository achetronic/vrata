// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte("{}"), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ControlPlaneURL != "http://localhost:8080" {
		t.Errorf("expected default controlPlaneUrl, got %q", cfg.ControlPlaneURL)
	}
	if cfg.Snapshot.Debounce != "5s" {
		t.Errorf("expected default debounce 5s, got %q", cfg.Snapshot.Debounce)
	}
	if cfg.Snapshot.MaxBatch != 100 {
		t.Errorf("expected default maxBatch 100, got %d", cfg.Snapshot.MaxBatch)
	}
	if cfg.Log.Format != "console" {
		t.Errorf("expected default log format console, got %q", cfg.Log.Format)
	}
	if !cfg.WatchHTTPRoutes() {
		t.Error("expected httpRoutes enabled by default")
	}
	if cfg.WatchSuperHTTPRoutes() {
		t.Error("expected superHttpRoutes disabled by default")
	}
	if !cfg.WatchGateways() {
		t.Error("expected gateways enabled by default")
	}
	if cfg.DuplicatesMode() != DuplicateModeWarn {
		t.Error("expected warn duplicates enabled by default")
	}
}

func TestLoadEnvExpansion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`controlPlaneUrl: "${TEST_VRATA_URL}"
`), 0644)

	t.Setenv("TEST_VRATA_URL", "http://cp:9090")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ControlPlaneURL != "http://cp:9090" {
		t.Errorf("expected expanded URL, got %q", cfg.ControlPlaneURL)
	}
}

func TestLoadCustomValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
controlPlaneUrl: "http://vrata:8080"
watch:
  namespaces: ["prod", "staging"]
  httpRoutes: false
  superHttpRoutes: true
  gateways: false
snapshot:
  debounce: "10s"
  maxBatch: 50
duplicates:
  mode: reject
log:
  format: json
  level: debug
`), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ControlPlaneURL != "http://vrata:8080" {
		t.Errorf("expected custom URL, got %q", cfg.ControlPlaneURL)
	}
	if len(cfg.Watch.Namespaces) != 2 {
		t.Errorf("expected 2 namespaces, got %d", len(cfg.Watch.Namespaces))
	}
	if cfg.WatchHTTPRoutes() {
		t.Error("expected httpRoutes disabled")
	}
	if !cfg.WatchSuperHTTPRoutes() {
		t.Error("expected superHttpRoutes enabled")
	}
	if cfg.WatchGateways() {
		t.Error("expected gateways disabled")
	}
	if cfg.Snapshot.MaxBatch != 50 {
		t.Errorf("expected maxBatch 50, got %d", cfg.Snapshot.MaxBatch)
	}
	if cfg.DuplicatesMode() != DuplicateModeReject {
		t.Error("expected warn duplicates disabled")
	}
	if cfg.Log.Format != "json" {
		t.Errorf("expected json format, got %q", cfg.Log.Format)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte("{{invalid"), 0644)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}
