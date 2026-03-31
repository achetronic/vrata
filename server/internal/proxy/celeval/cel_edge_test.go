// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package celeval

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestBufferBody_JSONArray(t *testing.T) {
	body := `[1, 2, 3]`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	_, data := BufferBody(req, 65536)

	if data.Raw != body {
		t.Errorf("raw: got %q", data.Raw)
	}
	if data.JSON != nil {
		t.Error("json should be nil for JSON arrays (not an object)")
	}
}

func TestBufferBody_JSONString(t *testing.T) {
	body := `"hello"`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	_, data := BufferBody(req, 65536)

	if data.Raw != body {
		t.Errorf("raw: got %q", data.Raw)
	}
	if data.JSON != nil {
		t.Error("json should be nil for JSON string (not an object)")
	}
}

func TestEvalBodyAndTLS_Combined(t *testing.T) {
	prg, err := Compile(`has(request.body) && has(request.body.json) && request.body.json.method == "tools/call" && has(request.tls) && request.tls.peerCertificate.uris.exists(u, u == "spiffe://cluster.local/ns/default/sa/agent-a")`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	body := `{"method":"tools/call"}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req, _ = BufferBody(req, 65536)

	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	parsed, _ := url.Parse("spiffe://cluster.local/ns/default/sa/agent-a")
	tmpl.URIs = append(tmpl.URIs, parsed)
	certDER, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	cert, _ := x509.ParseCertificate(certDER)
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	if !prg.Eval(req) {
		t.Error("expected true for body match + TLS match combined")
	}

	// Wrong SPIFFE → false.
	key2, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl2 := &x509.Certificate{SerialNumber: big.NewInt(2), NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour)}
	parsed2, _ := url.Parse("spiffe://cluster.local/ns/default/sa/agent-b")
	tmpl2.URIs = []*url.URL{parsed2}
	certDER2, _ := x509.CreateCertificate(rand.Reader, tmpl2, tmpl2, &key2.PublicKey, key2)
	cert2, _ := x509.ParseCertificate(certDER2)

	req2 := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2, _ = BufferBody(req2, 65536)
	req2.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert2}}

	if prg.Eval(req2) {
		t.Error("expected false — body matches but SPIFFE doesn't")
	}
}

func TestBufferBody_MaxSizeZero_Disabled(t *testing.T) {
	body := `{"key": "value"}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	_, data := BufferBody(req, 0)

	// maxSize 0 reads 1 byte (LimitReader maxSize+1=1), then truncates to 0.
	if data.Raw != "" {
		t.Errorf("raw should be empty when maxSize=0, got %q", data.Raw)
	}
	if data.JSON != nil {
		t.Error("json should be nil when maxSize=0")
	}
}
