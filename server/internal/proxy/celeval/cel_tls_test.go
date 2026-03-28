// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package celeval

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func buildTLSRequest(uris []string, dnsNames []string, subject pkix.Name) *http.Request {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      subject,
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     dnsNames,
	}
	for _, u := range uris {
		parsed, _ := url.Parse(u)
		tmpl.URIs = append(tmpl.URIs, parsed)
	}
	certDER, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	cert, _ := x509.ParseCertificate(certDER)

	req := httptest.NewRequest("GET", "/", nil)
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
	}
	return req
}

func TestCEL_PeerCert_SPIFFEUri(t *testing.T) {
	prg, err := Compile(`has(request.tls) && request.tls.peerCertificate.uris.exists(u, u == "spiffe://cluster.local/ns/default/sa/agent-a")`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	req := buildTLSRequest(
		[]string{"spiffe://cluster.local/ns/default/sa/agent-a"},
		nil,
		pkix.Name{CommonName: "agent-a"},
	)
	if !prg.Eval(req) {
		t.Error("expected true for matching SPIFFE URI")
	}

	reqWrong := buildTLSRequest(
		[]string{"spiffe://cluster.local/ns/default/sa/agent-b"},
		nil,
		pkix.Name{CommonName: "agent-b"},
	)
	if prg.Eval(reqWrong) {
		t.Error("expected false for non-matching SPIFFE URI")
	}
}

func TestCEL_PeerCert_DNSNames(t *testing.T) {
	prg, err := Compile(`has(request.tls) && request.tls.peerCertificate.dnsNames.exists(d, d == "agent.example.com")`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	req := buildTLSRequest(nil, []string{"agent.example.com"}, pkix.Name{})
	if !prg.Eval(req) {
		t.Error("expected true for matching DNS SAN")
	}

	reqWrong := buildTLSRequest(nil, []string{"other.example.com"}, pkix.Name{})
	if prg.Eval(reqWrong) {
		t.Error("expected false for non-matching DNS SAN")
	}
}

func TestCEL_PeerCert_Subject(t *testing.T) {
	prg, err := Compile(`has(request.tls) && request.tls.peerCertificate.subject.contains("agent-a")`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	req := buildTLSRequest(nil, nil, pkix.Name{CommonName: "agent-a", Organization: []string{"myorg"}})
	if !prg.Eval(req) {
		t.Error("expected true for subject containing agent-a")
	}
}

func TestCEL_PeerCert_Serial(t *testing.T) {
	prg, err := Compile(`has(request.tls) && request.tls.peerCertificate.serial == "1"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	req := buildTLSRequest(nil, nil, pkix.Name{CommonName: "test"})
	if !prg.Eval(req) {
		t.Error("expected true for serial 1")
	}
}

func TestCEL_PeerCert_MultipleURIs(t *testing.T) {
	prg, err := Compile(`has(request.tls) && request.tls.peerCertificate.uris.exists(u, u == "spiffe://example.org/sa/agent")`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	req := buildTLSRequest(
		[]string{"https://example.com/id", "spiffe://example.org/sa/agent"},
		nil,
		pkix.Name{},
	)
	if !prg.Eval(req) {
		t.Error("expected true — second URI matches")
	}
}

func TestCEL_PeerCert_NoCert(t *testing.T) {
	prg, err := Compile(`has(request.tls) && request.tls.peerCertificate.uris.size() > 0`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	if prg.Eval(req) {
		t.Error("expected false — no TLS at all")
	}

	req2 := httptest.NewRequest("GET", "/", nil)
	req2.TLS = &tls.ConnectionState{}
	if prg.Eval(req2) {
		t.Error("expected false — TLS but no peer certs")
	}
}

func TestCEL_PeerCert_NonTLS(t *testing.T) {
	prg, err := Compile(`!has(request.tls)`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	if !prg.Eval(req) {
		t.Error("expected true — non-TLS request should not have request.tls")
	}
}
