// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/achetronic/vrata/internal/model"
	"github.com/achetronic/vrata/internal/proxy/celeval"
)

func TestBuildClientIPResolver_Direct(t *testing.T) {
	resolver := BuildClientIPResolver(&model.ClientIPConfig{
		Source: model.ClientIPSourceDirect,
	})
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.42:12345"
	r.Header.Set("X-Forwarded-For", "10.0.0.1, 192.168.1.1")

	got := resolver(r)
	if got != "203.0.113.42" {
		t.Errorf("direct: got %q, want %q", got, "203.0.113.42")
	}
}

func TestBuildClientIPResolver_Header(t *testing.T) {
	resolver := BuildClientIPResolver(&model.ClientIPConfig{
		Source: model.ClientIPSourceHeader,
		Header: "X-Real-IP",
	})
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:9999"
	r.Header.Set("X-Real-IP", "198.51.100.7")

	if got := resolver(r); got != "198.51.100.7" {
		t.Errorf("header: got %q, want %q", got, "198.51.100.7")
	}
}

func TestBuildClientIPResolver_HeaderMissing(t *testing.T) {
	resolver := BuildClientIPResolver(&model.ClientIPConfig{
		Source: model.ClientIPSourceHeader,
		Header: "X-Real-IP",
	})
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:9999"

	if got := resolver(r); got != "10.0.0.1" {
		t.Errorf("header missing fallback: got %q, want %q", got, "10.0.0.1")
	}
}

func TestBuildClientIPResolver_XFF_TrustedCidrs(t *testing.T) {
	tests := []struct {
		name   string
		xff    string
		cidrs  []string
		remote string
		want   string
	}{
		{
			name:   "single trusted proxy",
			xff:    "203.0.113.50, 10.0.0.1",
			cidrs:  []string{"10.0.0.0/8"},
			remote: "10.0.0.2:9999",
			want:   "203.0.113.50",
		},
		{
			name:   "two trusted proxies",
			xff:    "198.51.100.7, 172.16.0.5, 10.0.0.1",
			cidrs:  []string{"10.0.0.0/8", "172.16.0.0/12"},
			remote: "10.0.0.2:9999",
			want:   "198.51.100.7",
		},
		{
			name:   "all entries trusted — falls back to RemoteAddr",
			xff:    "10.0.0.5, 10.0.0.1",
			cidrs:  []string{"10.0.0.0/8"},
			remote: "10.0.0.2:9999",
			want:   "10.0.0.2",
		},
		{
			name:   "no XFF header — falls back to RemoteAddr",
			xff:    "",
			cidrs:  []string{"10.0.0.0/8"},
			remote: "203.0.113.10:9999",
			want:   "203.0.113.10",
		},
		{
			name:   "single entry not trusted",
			xff:    "203.0.113.50",
			cidrs:  []string{"10.0.0.0/8"},
			remote: "10.0.0.1:9999",
			want:   "203.0.113.50",
		},
		{
			name:   "IPv6 trusted proxy",
			xff:    "2001:db8::1, ::1",
			cidrs:  []string{"::1/128"},
			remote: "[::1]:9999",
			want:   "2001:db8::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := BuildClientIPResolver(&model.ClientIPConfig{
				Source:       model.ClientIPSourceXFF,
				TrustedCidrs: tt.cidrs,
			})
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.remote
			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}
			if got := resolver(r); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildClientIPResolver_XFF_NumTrustedHops(t *testing.T) {
	tests := []struct {
		name   string
		xff    string
		hops   int
		remote string
		want   string
	}{
		{
			name:   "1 hop",
			xff:    "203.0.113.50, 10.0.0.1",
			hops:   1,
			remote: "10.0.0.2:9999",
			want:   "203.0.113.50",
		},
		{
			name:   "2 hops",
			xff:    "198.51.100.7, 10.0.0.5, 10.0.0.1",
			hops:   2,
			remote: "10.0.0.2:9999",
			want:   "198.51.100.7",
		},
		{
			name:   "more hops than entries — falls back to RemoteAddr",
			xff:    "10.0.0.1",
			hops:   2,
			remote: "10.0.0.2:9999",
			want:   "10.0.0.2",
		},
		{
			name:   "exactly matching entries — leftmost is client",
			xff:    "203.0.113.50, 10.0.0.1, 10.0.0.2",
			hops:   2,
			remote: "10.0.0.3:9999",
			want:   "203.0.113.50",
		},
		{
			name:   "no XFF — falls back to RemoteAddr",
			xff:    "",
			hops:   1,
			remote: "203.0.113.10:9999",
			want:   "203.0.113.10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := BuildClientIPResolver(&model.ClientIPConfig{
				Source:         model.ClientIPSourceXFF,
				NumTrustedHops: tt.hops,
			})
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.remote
			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}
			if got := resolver(r); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildClientIPResolver_XFF_Leftmost(t *testing.T) {
	resolver := BuildClientIPResolver(&model.ClientIPConfig{
		Source: model.ClientIPSourceXFF,
	})
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:9999"
	r.Header.Set("X-Forwarded-For", "203.0.113.50, 10.0.0.5")

	if got := resolver(r); got != "203.0.113.50" {
		t.Errorf("leftmost: got %q, want %q", got, "203.0.113.50")
	}
}

func TestBuildClientIPResolver_Nil(t *testing.T) {
	resolver := BuildClientIPResolver(nil)
	if resolver != nil {
		t.Error("expected nil resolver for nil config")
	}
}

func TestInjectClientIP(t *testing.T) {
	resolver := BuildClientIPResolver(&model.ClientIPConfig{
		Source:       model.ClientIPSourceXFF,
		TrustedCidrs: []string{"10.0.0.0/8"},
	})

	var got string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = celeval.ResolvedClientIP(r.Context())
	})

	handler := injectClientIP(resolver, inner)

	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.2:9999"
	r.Header.Set("X-Forwarded-For", "203.0.113.50, 10.0.0.1")
	handler.ServeHTTP(httptest.NewRecorder(), r)

	if got != "203.0.113.50" {
		t.Errorf("injectClientIP: got %q, want %q", got, "203.0.113.50")
	}
}

func TestInjectClientIP_NilResolver(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if ip := celeval.ResolvedClientIP(r.Context()); ip != "" {
			t.Errorf("expected empty IP with nil resolver, got %q", ip)
		}
	})

	handler := injectClientIP(nil, inner)
	r := httptest.NewRequest("GET", "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), r)

	if !called {
		t.Error("handler was not called")
	}
}
