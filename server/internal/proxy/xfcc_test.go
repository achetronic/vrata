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
	injectXFCC(req)

	xfcc := req.Header.Get("X-Forwarded-Client-Cert")
	if xfcc != "spiffe://cluster.local/ns/default/sa/agent-a" {
		t.Errorf("XFCC: got %q, want SPIFFE URI", xfcc)
	}
}

func TestXFCC_SpoofedStripped(t *testing.T) {
	req := httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("X-Forwarded-Client-Cert", "spoofed-value")

	injectXFCC(req)

	if req.Header.Get("X-Forwarded-Client-Cert") != "" {
		t.Error("spoofed XFCC should be stripped when no TLS cert is present")
	}
}

func TestXFCC_SpoofedReplacedByCert(t *testing.T) {
	req := buildMTLSRequest([]string{"spiffe://cluster.local/ns/default/sa/real"})
	req.Header.Set("X-Forwarded-Client-Cert", "spoofed-value")

	injectXFCC(req)

	xfcc := req.Header.Get("X-Forwarded-Client-Cert")
	if xfcc != "spiffe://cluster.local/ns/default/sa/real" {
		t.Errorf("spoofed XFCC should be replaced by real cert, got %q", xfcc)
	}
}

func TestXFCC_NoCert_NoHeader(t *testing.T) {
	req := httptest.NewRequest("POST", "/test", nil)
	injectXFCC(req)

	if req.Header.Get("X-Forwarded-Client-Cert") != "" {
		t.Error("no cert should mean no XFCC header")
	}
}

func TestXFCC_TLSNoCerts_NoHeader(t *testing.T) {
	req := httptest.NewRequest("POST", "/test", nil)
	req.TLS = &tls.ConnectionState{}
	injectXFCC(req)

	if req.Header.Get("X-Forwarded-Client-Cert") != "" {
		t.Error("TLS without peer certs should mean no XFCC header")
	}
}

func TestXFCC_CertWithoutURIs_NoHeader(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	certDER, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	cert, _ := x509.ParseCertificate(certDER)

	req := httptest.NewRequest("POST", "/test", nil)
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	injectXFCC(req)

	if req.Header.Get("X-Forwarded-Client-Cert") != "" {
		t.Error("cert without URI SANs should not produce XFCC header")
	}
}

func TestXFCC_MultipleURIs(t *testing.T) {
	req := buildMTLSRequest([]string{
		"spiffe://cluster.local/ns/default/sa/agent-a",
		"https://example.com/id/123",
	})
	injectXFCC(req)

	xfcc := req.Header.Get("X-Forwarded-Client-Cert")
	want := "spiffe://cluster.local/ns/default/sa/agent-a;https://example.com/id/123"
	if xfcc != want {
		t.Errorf("XFCC: got %q, want %q", xfcc, want)
	}
	if !strings.Contains(xfcc, ";") {
		t.Errorf("XFCC should be semicolon-separated, got %q", xfcc)
	}
}

func TestXFCC_PreservesOtherHeaders(t *testing.T) {
	req := buildMTLSRequest([]string{"spiffe://cluster.local/ns/default/sa/agent-a"})
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("X-Forwarded-Client-Cert", "spoofed")

	injectXFCC(req)

	if req.Header.Get("Authorization") != "Bearer token" {
		t.Error("injectXFCC should not touch other headers")
	}
	if req.Header.Get("X-Forwarded-Client-Cert") == "spoofed" {
		t.Error("spoofed XFCC should have been replaced")
	}
}
