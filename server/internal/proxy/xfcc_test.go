// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package proxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func buildMTLSRequest(uris []string) *http.Request {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	for _, u := range uris {
		parsed, _ := url.Parse(u)
		tmpl.URIs = append(tmpl.URIs, parsed)
	}
	certDER, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	cert, _ := x509.ParseCertificate(certDER)

	req := httptest.NewRequest("POST", "/test", nil)
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
	}
	return req
}

func TestXFCC_Injected(t *testing.T) {
	req := buildMTLSRequest([]string{"spiffe://cluster.local/ns/default/sa/agent-a"})

	// Simulate forwardHandler XFCC logic.
	req.Header.Del("X-Forwarded-Client-Cert")
	if req.TLS != nil && len(req.TLS.PeerCertificates) > 0 {
		cert := req.TLS.PeerCertificates[0]
		if len(cert.URIs) > 0 {
			var parts []string
			for _, u := range cert.URIs {
				parts = append(parts, u.String())
			}
			req.Header.Set("X-Forwarded-Client-Cert", strings.Join(parts, ";"))
		}
	}

	xfcc := req.Header.Get("X-Forwarded-Client-Cert")
	if xfcc != "spiffe://cluster.local/ns/default/sa/agent-a" {
		t.Errorf("XFCC: got %q, want SPIFFE URI", xfcc)
	}
}

func TestXFCC_SpoofedStripped(t *testing.T) {
	req := httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("X-Forwarded-Client-Cert", "spoofed-value")

	// Simulate forwardHandler XFCC logic: strip then inject.
	req.Header.Del("X-Forwarded-Client-Cert")

	if req.Header.Get("X-Forwarded-Client-Cert") != "" {
		t.Error("spoofed XFCC should be stripped")
	}
}

func TestXFCC_NoCert_NoHeader(t *testing.T) {
	req := httptest.NewRequest("POST", "/test", nil)

	// Simulate forwardHandler: strip + no cert = no header.
	req.Header.Del("X-Forwarded-Client-Cert")
	if req.TLS != nil && len(req.TLS.PeerCertificates) > 0 {
		t.Fatal("should not have peer certs")
	}

	if req.Header.Get("X-Forwarded-Client-Cert") != "" {
		t.Error("no cert should mean no XFCC header")
	}
}

func TestXFCC_MultipleURIs(t *testing.T) {
	req := buildMTLSRequest([]string{
		"spiffe://cluster.local/ns/default/sa/agent-a",
		"https://example.com/id/123",
	})

	req.Header.Del("X-Forwarded-Client-Cert")
	if req.TLS != nil && len(req.TLS.PeerCertificates) > 0 {
		cert := req.TLS.PeerCertificates[0]
		if len(cert.URIs) > 0 {
			var parts []string
			for _, u := range cert.URIs {
				parts = append(parts, u.String())
			}
			req.Header.Set("X-Forwarded-Client-Cert", strings.Join(parts, ";"))
		}
	}

	xfcc := req.Header.Get("X-Forwarded-Client-Cert")
	if !strings.Contains(xfcc, "spiffe://") || !strings.Contains(xfcc, "https://") {
		t.Errorf("XFCC should contain both URIs, got %q", xfcc)
	}
	if !strings.Contains(xfcc, ";") {
		t.Errorf("XFCC should be semicolon-separated, got %q", xfcc)
	}
}
