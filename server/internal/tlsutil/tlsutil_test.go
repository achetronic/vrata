// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/achetronic/vrata/internal/config"
)

// ######################## Test CA + cert generator ########################

type testPKI struct {
	CACertPEM   string
	CAKeyPEM    string
	caCert      *x509.Certificate
	caKey       *ecdsa.PrivateKey
}

func newTestPKI(t *testing.T) *testPKI {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating CA key: %v", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("creating CA cert: %v", err)
	}

	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		t.Fatalf("parsing CA cert: %v", err)
	}

	caKeyDER, err := x509.MarshalECPrivateKey(caKey)
	if err != nil {
		t.Fatalf("marshaling CA key: %v", err)
	}

	return &testPKI{
		CACertPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})),
		CAKeyPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: caKeyDER})),
		caCert:    caCert,
		caKey:     caKey,
	}
}

type testCert struct {
	CertPEM string
	KeyPEM  string
}

func (p *testPKI) issueCert(t *testing.T, cn string, isServer bool) testCert {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key for %s: %v", cn, err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		DNSNames:     []string{"localhost"},
	}
	if isServer {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	} else {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, p.caCert, &key.PublicKey, p.caKey)
	if err != nil {
		t.Fatalf("creating cert for %s: %v", cn, err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshaling key for %s: %v", cn, err)
	}

	return testCert{
		CertPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})),
		KeyPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})),
	}
}

// ######################## ServerConfig unit tests ########################

func TestServerConfigValidCert(t *testing.T) {
	pki := newTestPKI(t)
	srv := pki.issueCert(t, "server", true)

	cfg, err := ServerConfig(&config.TLSConfig{
		Cert: srv.CertPEM,
		Key:  srv.KeyPEM,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Certificates) != 1 {
		t.Errorf("expected 1 certificate, got %d", len(cfg.Certificates))
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("expected MinVersion TLS 1.2, got %d", cfg.MinVersion)
	}
	if cfg.ClientAuth != tls.NoClientCert {
		t.Errorf("expected NoClientCert, got %d", cfg.ClientAuth)
	}
}

func TestServerConfigInvalidCert(t *testing.T) {
	_, err := ServerConfig(&config.TLSConfig{
		Cert: "not-a-cert",
		Key:  "not-a-key",
	})
	if err == nil {
		t.Fatal("expected error for invalid cert")
	}
}

func TestServerConfigInvalidCA(t *testing.T) {
	pki := newTestPKI(t)
	srv := pki.issueCert(t, "server", true)

	_, err := ServerConfig(&config.TLSConfig{
		Cert: srv.CertPEM,
		Key:  srv.KeyPEM,
		CA:   "not-a-ca",
	})
	if err == nil {
		t.Fatal("expected error for invalid CA")
	}
}

func TestServerConfigClientAuthOptional(t *testing.T) {
	pki := newTestPKI(t)
	srv := pki.issueCert(t, "server", true)

	cfg, err := ServerConfig(&config.TLSConfig{
		Cert:       srv.CertPEM,
		Key:        srv.KeyPEM,
		CA:         pki.CACertPEM,
		ClientAuth: "optional",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClientAuth != tls.VerifyClientCertIfGiven {
		t.Errorf("expected VerifyClientCertIfGiven, got %d", cfg.ClientAuth)
	}
	if cfg.ClientCAs == nil {
		t.Error("expected ClientCAs to be set")
	}
}

func TestServerConfigClientAuthRequire(t *testing.T) {
	pki := newTestPKI(t)
	srv := pki.issueCert(t, "server", true)

	cfg, err := ServerConfig(&config.TLSConfig{
		Cert:       srv.CertPEM,
		Key:        srv.KeyPEM,
		CA:         pki.CACertPEM,
		ClientAuth: "require",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Errorf("expected RequireAndVerifyClientCert, got %d", cfg.ClientAuth)
	}
}

// ######################## ClientTransport unit tests ########################

func TestClientTransportNil(t *testing.T) {
	tr, err := ClientTransport(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr == nil {
		t.Fatal("expected non-nil transport")
	}
}

func TestClientTransportWithCA(t *testing.T) {
	pki := newTestPKI(t)

	tr, err := ClientTransport(&config.TLSConfig{
		CA: pki.CACertPEM,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.TLSClientConfig == nil {
		t.Fatal("expected TLSClientConfig to be set")
	}
	if tr.TLSClientConfig.RootCAs == nil {
		t.Error("expected RootCAs to be set")
	}
}

func TestClientTransportWithClientCert(t *testing.T) {
	pki := newTestPKI(t)
	client := pki.issueCert(t, "client", false)

	tr, err := ClientTransport(&config.TLSConfig{
		Cert: client.CertPEM,
		Key:  client.KeyPEM,
		CA:   pki.CACertPEM,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tr.TLSClientConfig.Certificates) != 1 {
		t.Errorf("expected 1 client cert, got %d", len(tr.TLSClientConfig.Certificates))
	}
}

func TestClientTransportInvalidCA(t *testing.T) {
	_, err := ClientTransport(&config.TLSConfig{
		CA: "garbage",
	})
	if err == nil {
		t.Fatal("expected error for invalid CA")
	}
}

func TestClientTransportInvalidClientCert(t *testing.T) {
	pki := newTestPKI(t)
	_, err := ClientTransport(&config.TLSConfig{
		Cert: "bad",
		Key:  "bad",
		CA:   pki.CACertPEM,
	})
	if err == nil {
		t.Fatal("expected error for invalid client cert")
	}
}

// ######################## Integration: TLS server + client ########################

func TestIntegrationTLSOnly(t *testing.T) {
	pki := newTestPKI(t)
	srvCert := pki.issueCert(t, "server", true)

	srvTLS, err := ServerConfig(&config.TLSConfig{
		Cert: srvCert.CertPEM,
		Key:  srvCert.KeyPEM,
	})
	if err != nil {
		t.Fatalf("server config: %v", err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	srv.TLS = srvTLS
	srv.StartTLS()
	defer srv.Close()

	tr, err := ClientTransport(&config.TLSConfig{CA: pki.CACertPEM})
	if err != nil {
		t.Fatalf("client transport: %v", err)
	}
	client := &http.Client{Transport: tr}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("expected 'ok', got %q", body)
	}
}

func TestIntegrationMTLS(t *testing.T) {
	pki := newTestPKI(t)
	srvCert := pki.issueCert(t, "server", true)
	clientCert := pki.issueCert(t, "client", false)

	srvTLS, err := ServerConfig(&config.TLSConfig{
		Cert:       srvCert.CertPEM,
		Key:        srvCert.KeyPEM,
		CA:         pki.CACertPEM,
		ClientAuth: "require",
	})
	if err != nil {
		t.Fatalf("server config: %v", err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "mtls-ok")
	}))
	srv.TLS = srvTLS
	srv.StartTLS()
	defer srv.Close()

	tr, err := ClientTransport(&config.TLSConfig{
		Cert: clientCert.CertPEM,
		Key:  clientCert.KeyPEM,
		CA:   pki.CACertPEM,
	})
	if err != nil {
		t.Fatalf("client transport: %v", err)
	}
	client := &http.Client{Transport: tr}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "mtls-ok" {
		t.Errorf("expected 'mtls-ok', got %q", body)
	}
}

func TestIntegrationMTLSRejectsWithoutClientCert(t *testing.T) {
	pki := newTestPKI(t)
	srvCert := pki.issueCert(t, "server", true)

	srvTLS, err := ServerConfig(&config.TLSConfig{
		Cert:       srvCert.CertPEM,
		Key:        srvCert.KeyPEM,
		CA:         pki.CACertPEM,
		ClientAuth: "require",
	})
	if err != nil {
		t.Fatalf("server config: %v", err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))
	srv.TLS = srvTLS
	srv.StartTLS()
	defer srv.Close()

	tr, err := ClientTransport(&config.TLSConfig{CA: pki.CACertPEM})
	if err != nil {
		t.Fatalf("client transport: %v", err)
	}
	client := &http.Client{Transport: tr}

	_, err = client.Get(srv.URL)
	if err == nil {
		t.Fatal("expected TLS handshake error when client cert is missing")
	}
}

func TestIntegrationMTLSOptionalAllowsWithout(t *testing.T) {
	pki := newTestPKI(t)
	srvCert := pki.issueCert(t, "server", true)

	srvTLS, err := ServerConfig(&config.TLSConfig{
		Cert:       srvCert.CertPEM,
		Key:        srvCert.KeyPEM,
		CA:         pki.CACertPEM,
		ClientAuth: "optional",
	})
	if err != nil {
		t.Fatalf("server config: %v", err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "optional-ok")
	}))
	srv.TLS = srvTLS
	srv.StartTLS()
	defer srv.Close()

	tr, err := ClientTransport(&config.TLSConfig{CA: pki.CACertPEM})
	if err != nil {
		t.Fatalf("client transport: %v", err)
	}
	client := &http.Client{Transport: tr}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "optional-ok" {
		t.Errorf("expected 'optional-ok', got %q", body)
	}
}

func TestIntegrationMTLSRejectsWrongCA(t *testing.T) {
	pki := newTestPKI(t)
	otherPKI := newTestPKI(t)
	srvCert := pki.issueCert(t, "server", true)
	badClient := otherPKI.issueCert(t, "bad-client", false)

	srvTLS, err := ServerConfig(&config.TLSConfig{
		Cert:       srvCert.CertPEM,
		Key:        srvCert.KeyPEM,
		CA:         pki.CACertPEM,
		ClientAuth: "require",
	})
	if err != nil {
		t.Fatalf("server config: %v", err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))
	srv.TLS = srvTLS
	srv.StartTLS()
	defer srv.Close()

	tr, err := ClientTransport(&config.TLSConfig{
		Cert: badClient.CertPEM,
		Key:  badClient.KeyPEM,
		CA:   pki.CACertPEM,
	})
	if err != nil {
		t.Fatalf("client transport: %v", err)
	}
	client := &http.Client{Transport: tr}

	_, err = client.Get(srv.URL)
	if err == nil {
		t.Fatal("expected TLS error when client cert is signed by wrong CA")
	}
}

// ######################## Integration: TLS + API key auth ########################

func TestIntegrationTLSWithAPIKeyAuth(t *testing.T) {
	pki := newTestPKI(t)
	srvCert := pki.issueCert(t, "server", true)
	clientCert := pki.issueCert(t, "client", false)

	srvTLS, err := ServerConfig(&config.TLSConfig{
		Cert:       srvCert.CertPEM,
		Key:        srvCert.KeyPEM,
		CA:         pki.CACertPEM,
		ClientAuth: "require",
	})
	if err != nil {
		t.Fatalf("server config: %v", err)
	}

	validKey := "test-api-key-12345"
	handler := apiKeyMiddleware(validKey, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "authed-ok")
	}))

	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = srvTLS
	srv.StartTLS()
	defer srv.Close()

	tr, err := ClientTransport(&config.TLSConfig{
		Cert: clientCert.CertPEM,
		Key:  clientCert.KeyPEM,
		CA:   pki.CACertPEM,
	})
	if err != nil {
		t.Fatalf("client transport: %v", err)
	}
	client := &http.Client{Transport: tr}

	t.Run("valid key", func(t *testing.T) {
		req, _ := http.NewRequest("GET", srv.URL, nil)
		req.Header.Set("Authorization", "Bearer "+validKey)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if string(body) != "authed-ok" {
			t.Errorf("expected 'authed-ok', got %q", body)
		}
	})

	t.Run("invalid key", func(t *testing.T) {
		req, _ := http.NewRequest("GET", srv.URL, nil)
		req.Header.Set("Authorization", "Bearer wrong-key")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("missing key", func(t *testing.T) {
		req, _ := http.NewRequest("GET", srv.URL, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})
}

func TestIntegrationClientRejectsUntrustedServer(t *testing.T) {
	pki := newTestPKI(t)
	otherPKI := newTestPKI(t)
	srvCert := pki.issueCert(t, "server", true)

	srvTLS, err := ServerConfig(&config.TLSConfig{
		Cert: srvCert.CertPEM,
		Key:  srvCert.KeyPEM,
	})
	if err != nil {
		t.Fatalf("server config: %v", err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))
	srv.TLS = srvTLS
	srv.StartTLS()
	defer srv.Close()

	tr, err := ClientTransport(&config.TLSConfig{CA: otherPKI.CACertPEM})
	if err != nil {
		t.Fatalf("client transport: %v", err)
	}
	client := &http.Client{Transport: tr}

	_, err = client.Get(srv.URL)
	if err == nil {
		t.Fatal("expected error when server cert is not trusted by client CA")
	}
	if !strings.Contains(err.Error(), "certificate") {
		t.Errorf("expected certificate error, got: %v", err)
	}
}

// apiKeyMiddleware is a minimal auth check for integration tests. The real
// middleware lives in internal/api/middleware/auth.go and is tested there.
func apiKeyMiddleware(validKey string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			w.WriteHeader(401)
			fmt.Fprint(w, `{"error":"missing authorization header"}`)
			return
		}
		token, ok := strings.CutPrefix(auth, "Bearer ")
		if !ok || token != validKey {
			w.WriteHeader(401)
			fmt.Fprint(w, `{"error":"invalid API key"}`)
			return
		}
		next.ServeHTTP(w, r)
	})
}
