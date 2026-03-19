// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestRaftApplyRejectsNonClusterMode(t *testing.T) {
	deps := &Dependencies{Logger: testLogger()}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	deps.RaftApply(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestRaftApplyRejectsPublicIP(t *testing.T) {
	deps := &Dependencies{
		Logger: testLogger(),
		Raft:   &fakeRaftApplier{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"type":"SaveRoute"}`))
	r.RemoteAddr = "203.0.113.1:1234"
	deps.RaftApply(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for public IP, got %d", w.Code)
	}
}

func TestRaftApplyAcceptsPrivateIP(t *testing.T) {
	deps := &Dependencies{
		Logger: testLogger(),
		Raft:   &fakeRaftApplier{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"type":"SaveRoute"}`))
	r.RemoteAddr = "10.0.0.5:1234"
	deps.RaftApply(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for private IP, got %d", w.Code)
	}
}

func TestRaftApplyAcceptsLoopback(t *testing.T) {
	deps := &Dependencies{
		Logger: testLogger(),
		Raft:   &fakeRaftApplier{},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"type":"SaveRoute"}`))
	r.RemoteAddr = "127.0.0.1:1234"
	deps.RaftApply(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for loopback, got %d", w.Code)
	}
}

func TestIsPrivateAddr(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:1234", true},
		{"10.0.0.1:5678", true},
		{"172.16.0.1:9999", true},
		{"192.168.1.1:80", true},
		{"[::1]:8080", true},
		{"8.8.8.8:53", false},
		{"203.0.113.1:80", false},
	}
	for _, tt := range tests {
		got := isPrivateAddr(tt.addr)
		if got != tt.want {
			t.Errorf("isPrivateAddr(%q) = %v, want %v", tt.addr, got, tt.want)
		}
	}
}

// fakeRaftApplier satisfies the RaftApplier interface for testing.
type fakeRaftApplier struct{}

func (f *fakeRaftApplier) ApplyRaw(data []byte) error { return nil }

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}
