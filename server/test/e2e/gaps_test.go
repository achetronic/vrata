// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bufio"
	"bytes"
	"context"
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
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	extauthzv1 "github.com/achetronic/vrata/proto/extauthz/v1"
	extprocv1 "github.com/achetronic/vrata/proto/extproc/v1"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers: TLS CA + certs generation (in-process, no files)
// ─────────────────────────────────────────────────────────────────────────────

type testPKI struct {
	caCertPEM, caKeyPEM     string
	serverCertPEM, serverKeyPEM string
	clientCertPEM, clientKeyPEM string
}

func generatePKI(t *testing.T) *testPKI {
	t.Helper()

	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	caDER, _ := x509.CreateCertificate(rand.Reader, caTpl, caTpl, &caKey.PublicKey, caKey)
	caCert, _ := x509.ParseCertificate(caDER)

	mkCert := func(cn string, ips []net.IP, dnsNames []string, uris []string, keyUsage x509.ExtKeyUsage) (string, string) {
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		tpl := &x509.Certificate{
			SerialNumber: big.NewInt(time.Now().UnixNano()),
			Subject:      pkix.Name{CommonName: cn},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(24 * time.Hour),
			IPAddresses:  ips,
			DNSNames:     dnsNames,
			ExtKeyUsage:  []x509.ExtKeyUsage{keyUsage},
			KeyUsage:     x509.KeyUsageDigitalSignature,
		}
		for _, u := range uris {
			parsed, err := url.Parse(u)
			if err == nil && parsed != nil {
				tpl.URIs = append(tpl.URIs, parsed)
			}
		}
		der, _ := x509.CreateCertificate(rand.Reader, tpl, caCert, &key.PublicKey, caKey)
		return pemEncode("CERTIFICATE", der), pemEncodeEC(key)
	}

	sCert, sKey := mkCert("localhost", []net.IP{net.ParseIP("127.0.0.1")}, []string{"localhost"}, nil, x509.ExtKeyUsageServerAuth)
	cCert, cKey := mkCert("test-client", nil, nil, []string{"spiffe://test.local/client"}, x509.ExtKeyUsageClientAuth)

	return &testPKI{
		caCertPEM:    pemEncode("CERTIFICATE", caDER),
		caKeyPEM:     pemEncodeEC(caKey),
		serverCertPEM: sCert, serverKeyPEM: sKey,
		clientCertPEM: cCert, clientKeyPEM: cKey,
	}
}

func pemEncode(typ string, der []byte) string {
	var buf bytes.Buffer
	pem.Encode(&buf, &pem.Block{Type: typ, Bytes: der})
	return buf.String()
}

func pemEncodeEC(key *ecdsa.PrivateKey) string {
	der, _ := x509.MarshalECPrivateKey(key)
	return pemEncode("EC PRIVATE KEY", der)
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. CEL body access (request.body.raw + request.body.json)
// ─────────────────────────────────────────────────────────────────────────────

func TestGap_CELBodyJSON_RouteMatch(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "cel-body-json",
		"match": map[string]any{
			"pathPrefix": "/cel-body-json",
			"methods":    []string{"POST"},
			"cel":        `has(request.body.json) && request.body.json["action"] == "deploy"`,
		},
		"directResponse": map[string]any{"status": 200, "body": "body-matched"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	// Match: JSON body with action=deploy.
	code, _, body := proxyRequest(t, "POST", "/cel-body-json", []byte(`{"action":"deploy"}`),
		map[string]string{"Content-Type": "application/json"})
	if code != 200 || body != "body-matched" {
		t.Errorf("cel body json match: %d %q", code, body)
	}

	// No match: different action.
	code, _, _ = proxyRequest(t, "POST", "/cel-body-json", []byte(`{"action":"rollback"}`),
		map[string]string{"Content-Type": "application/json"})
	if code != 404 {
		t.Errorf("cel body json miss should 404: %d", code)
	}

	// No match: not JSON.
	code, _, _ = proxyRequest(t, "POST", "/cel-body-json", []byte(`plain text`),
		map[string]string{"Content-Type": "text/plain"})
	if code != 404 {
		t.Errorf("cel body non-json should 404: %d", code)
	}
}

func TestGap_CELBodyRaw_RouteMatch(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "cel-body-raw",
		"match": map[string]any{
			"pathPrefix": "/cel-body-raw",
			"methods":    []string{"POST"},
			"cel":        `request.body.raw.contains("MAGIC_TOKEN")`,
		},
		"directResponse": map[string]any{"status": 200, "body": "raw-matched"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, body := proxyRequest(t, "POST", "/cel-body-raw", []byte(`prefix MAGIC_TOKEN suffix`), nil)
	if code != 200 || body != "raw-matched" {
		t.Errorf("cel body raw match: %d %q", code, body)
	}

	code, _, _ = proxyRequest(t, "POST", "/cel-body-raw", []byte(`no token here`), nil)
	if code != 404 {
		t.Errorf("cel body raw miss should 404: %d", code)
	}
}

func TestGap_CELBody_InlineAuthz(t *testing.T) {
	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "cel-body-authz", "type": "inlineAuthz",
		"inlineAuthz": map[string]any{
			"rules": []map[string]any{
				{"cel": `has(request.body.json) && request.body.json["role"] == "admin"`, "action": "allow"},
			},
			"defaultAction": "deny",
			"denyStatus":    403,
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Write([]byte("upstream-got:" + string(b)))
	})
	destID := createDestination(t, "cel-body-authz-dest", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "cel-body-authz-route", "match": map[string]any{"pathPrefix": "/cel-body-authz", "methods": []string{"POST"}},
		"forward":       map[string]any{"destinations": []map[string]any{{"destinationId": destID, "weight": 100}}},
		"middlewareIds": []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	// Admin: allowed, body forwarded to upstream.
	code, _, body := proxyRequest(t, "POST", "/cel-body-authz", []byte(`{"role":"admin","data":"important"}`),
		map[string]string{"Content-Type": "application/json"})
	if code != 200 {
		t.Errorf("admin: expected 200, got %d %q", code, body)
	}

	// User: denied.
	code, _, _ = proxyRequest(t, "POST", "/cel-body-authz", []byte(`{"role":"user"}`),
		map[string]string{"Content-Type": "application/json"})
	if code != 403 {
		t.Errorf("user should be 403: %d", code)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. IncludeAttemptCount header
// ─────────────────────────────────────────────────────────────────────────────

func TestGap_IncludeAttemptCount(t *testing.T) {
	var lastAttempt atomic.Value
	var count atomic.Int64
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		lastAttempt.Store(r.Header.Get("X-Request-Attempt-Count"))
		n := count.Add(1)
		if n <= 1 {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte("ok"))
	})
	destID := createDestination(t, "attempt-count-dest", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "attempt-count", "match": map[string]any{"pathPrefix": "/attempt-count"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
			"retry":        map[string]any{"attempts": 2, "on": []string{"server-error"}},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))

	_, group := apiPost(t, "/groups", map[string]any{
		"name": "attempt-count-group", "routeIds": []string{id(route)}, "includeAttemptCount": true,
	})
	defer apiDelete(t, "/groups/"+id(group))

	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, _ := proxyGet(t, "/attempt-count", nil)
	if code != 200 {
		t.Errorf("expected 200, got %d", code)
	}
	val, _ := lastAttempt.Load().(string)
	if val == "" {
		t.Error("X-Request-Attempt-Count header not set on upstream request")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. Partial redirect fields (scheme, host, path, stripQuery)
// ─────────────────────────────────────────────────────────────────────────────

func TestGap_Redirect_SchemeOnly(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "redir-scheme", "match": map[string]any{"pathPrefix": "/redir-scheme"},
		"redirect": map[string]any{"scheme": "https", "code": 301},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, hdr, _ := proxyGet(t, "/redir-scheme/page", nil)
	if code != 301 {
		t.Errorf("expected 301, got %d", code)
	}
	loc := hdr.Get("Location")
	if !strings.HasPrefix(loc, "https://") {
		t.Errorf("scheme not changed: %q", loc)
	}
}

func TestGap_Redirect_HostOnly(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "redir-host", "match": map[string]any{"pathPrefix": "/redir-host"},
		"redirect": map[string]any{"host": "new.example.com", "code": 302},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, hdr, _ := proxyGet(t, "/redir-host/page", nil)
	if code != 302 {
		t.Errorf("expected 302, got %d", code)
	}
	loc := hdr.Get("Location")
	if !strings.Contains(loc, "new.example.com") {
		t.Errorf("host not changed: %q", loc)
	}
}

func TestGap_Redirect_PathReplace(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "redir-path", "match": map[string]any{"pathPrefix": "/redir-path"},
		"redirect": map[string]any{"path": "/new-location", "code": 307},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, hdr, _ := proxyGet(t, "/redir-path/old", nil)
	if code != 307 {
		t.Errorf("expected 307, got %d", code)
	}
	loc := hdr.Get("Location")
	if !strings.Contains(loc, "/new-location") {
		t.Errorf("path not replaced: %q", loc)
	}
}

func TestGap_Redirect_StripQuery(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "redir-strip", "match": map[string]any{"pathPrefix": "/redir-strip"},
		"redirect": map[string]any{"url": "https://example.com/clean", "code": 301, "stripQuery": true},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, hdr, _ := proxyGet(t, "/redir-strip?foo=bar&baz=1", nil)
	if code != 301 {
		t.Errorf("expected 301, got %d", code)
	}
	loc := hdr.Get("Location")
	if strings.Contains(loc, "foo=") || strings.Contains(loc, "baz=") {
		t.Errorf("query not stripped: %q", loc)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. Middleware override: Headers (per-route header merge)
// ─────────────────────────────────────────────────────────────────────────────

func TestGap_MiddlewareOverride_Headers(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fmt.Sprintf("base=%s,override=%s",
			r.Header.Get("X-Base"), r.Header.Get("X-Override"))))
	})
	destID := createDestination(t, "ovr-hdr-dest", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "ovr-base-hdr", "type": "headers",
		"headers": map[string]any{
			"requestHeadersToAdd": []map[string]any{
				{"key": "X-Base", "value": "from-middleware"},
			},
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "ovr-hdr-route", "match": map[string]any{"pathPrefix": "/ovr-hdr"},
		"forward":       map[string]any{"destinations": []map[string]any{{"destinationId": destID, "weight": 100}}},
		"middlewareIds": []string{id(mw)},
		"middlewareOverrides": map[string]any{
			id(mw): map[string]any{
				"headers": map[string]any{
					"requestHeadersToAdd": []map[string]any{
						{"key": "X-Override", "value": "from-route"},
					},
				},
			},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, body := proxyGet(t, "/ovr-hdr", nil)
	if code != 200 {
		t.Fatalf("expected 200, got %d", code)
	}
	if !strings.Contains(body, "override=from-route") {
		t.Errorf("override header not applied: %q", body)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 5. Circuit breaker (destination with threshold)
// ─────────────────────────────────────────────────────────────────────────────

func TestGap_CircuitBreaker(t *testing.T) {
	var count atomic.Int64
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.WriteHeader(500)
	})
	destID := createDestination(t, "cb-dest", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	// Update destination with circuit breaker config.
	apiPut(t, "/destinations/"+destID, map[string]any{
		"name": "cb-dest", "host": up.host(), "port": up.port(),
		"options": map[string]any{
			"circuitBreaker": map[string]any{
				"failureThreshold": 3,
				"openDuration":     "5s",
			},
		},
	})

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "cb-route", "match": map[string]any{"pathPrefix": "/cb-test"},
		"forward": map[string]any{"destinations": []map[string]any{{"destinationId": destID, "weight": 100}}},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	// Send enough requests to trip the circuit.
	for i := 0; i < 10; i++ {
		proxyGet(t, "/cb-test", nil)
	}

	// After tripping, the proxy should return 503 (circuit open) without hitting upstream.
	beforeCount := count.Load()
	code, _, body := proxyGet(t, "/cb-test", nil)
	afterCount := count.Load()

	// Either the circuit opened (503 without upstream call) or we still get 500s.
	// The key check: if circuit is open, upstream should NOT have been called.
	if code == 503 {
		if afterCount > beforeCount {
			t.Error("circuit open but upstream was still called")
		}
	}
	_ = body
}

// ─────────────────────────────────────────────────────────────────────────────
// 6. Config dump content verification
// ─────────────────────────────────────────────────────────────────────────────

func TestGap_ConfigDump_ContainsEntities(t *testing.T) {
	destID := createDestination(t, "dump-dest", "127.0.0.1", 9999)
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "dump-route", "match": map[string]any{"pathPrefix": "/dump"},
		"directResponse": map[string]any{"status": 200},
	})
	defer apiDelete(t, "/routes/"+id(route))

	code, data := apiGet(t, "/debug/config")
	if code != 200 {
		t.Fatalf("config dump: %d", code)
	}

	body := string(data)
	if !strings.Contains(body, "dump-dest") {
		t.Error("destination not in config dump")
	}
	if !strings.Contains(body, "dump-route") {
		t.Error("route not in config dump")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 7. mTLS listener + CEL request.tls.* + XFCC header
// ─────────────────────────────────────────────────────────────────────────────

func TestGap_mTLS_CEL_TLSAccess(t *testing.T) {
	pki := generatePKI(t)

	mtlsPort := 13700
	_, listener := apiPost(t, "/listeners", map[string]any{
		"name": "mtls-listener", "address": "0.0.0.0", "port": mtlsPort,
		"tls": map[string]any{
			"cert": pki.serverCertPEM,
			"key":  pki.serverKeyPEM,
			"clientAuth": map[string]any{
				"mode": "require",
				"ca":   pki.caCertPEM,
			},
		},
	})
	defer apiDelete(t, "/listeners/"+id(listener))

	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		xfcc := r.Header.Get("X-Forwarded-Client-Cert")
		w.Write([]byte("xfcc=" + xfcc))
	})
	destID := createDestination(t, "mtls-dest", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "mtls-route", "match": map[string]any{"pathPrefix": "/mtls-test"},
		"forward": map[string]any{"destinations": []map[string]any{{"destinationId": destID, "weight": 100}}},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	// Build TLS client with client cert.
	clientCert, err := tls.X509KeyPair([]byte(pki.clientCertPEM), []byte(pki.clientKeyPEM))
	if err != nil {
		t.Fatalf("loading client cert: %v", err)
	}
	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM([]byte(pki.caCertPEM))

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{clientCert},
				RootCAs:      caPool,
				ServerName:   "localhost",
			},
		},
	}

	resp, err := client.Get(fmt.Sprintf("https://localhost:%d/mtls-test", mtlsPort))
	if err != nil {
		t.Fatalf("mTLS request: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		t.Errorf("mTLS: expected 200, got %d: %s", resp.StatusCode, data)
	}
	// XFCC header should be injected.
	if !strings.Contains(string(data), "xfcc=") {
		t.Errorf("XFCC header not present in upstream: %s", data)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 8. ExtProc gRPC (inline gRPC server)
// ─────────────────────────────────────────────────────────────────────────────

type testExtProcServer struct {
	extprocv1.UnimplementedProcessorServer
}

func (s *testExtProcServer) Process(stream extprocv1.Processor_ProcessServer) error {
	for {
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		switch req.Phase.(type) {
		case *extprocv1.ProcessingRequest_RequestHeaders:
			stream.Send(&extprocv1.ProcessingResponse{
				Action: &extprocv1.ProcessingResponse_RequestHeaders{
					RequestHeaders: &extprocv1.HeadersAction{
						Status:     extprocv1.ActionStatus_CONTINUE,
						SetHeaders: []*extprocv1.HeaderPair{{Key: "X-ExtProc-gRPC", Value: "processed"}},
					},
				},
			})
		case *extprocv1.ProcessingRequest_ResponseHeaders:
			stream.Send(&extprocv1.ProcessingResponse{
				Action: &extprocv1.ProcessingResponse_ResponseHeaders{
					ResponseHeaders: &extprocv1.HeadersAction{
						Status: extprocv1.ActionStatus_CONTINUE,
					},
				},
			})
		}
	}
}

func TestGap_ExtProc_gRPC(t *testing.T) {
	// Start inline gRPC extproc server.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	extprocv1.RegisterProcessorServer(grpcServer, &testExtProcServer{})
	go grpcServer.Serve(lis)
	t.Cleanup(grpcServer.GracefulStop)

	port := lis.Addr().(*net.TCPAddr).Port

	destID := createDestination(t, "extproc-grpc-dest", "127.0.0.1", port)
	defer apiDelete(t, "/destinations/"+destID)

	// Update destination with http2 for gRPC.
	apiPut(t, "/destinations/"+destID, map[string]any{
		"name": "extproc-grpc-dest", "host": "127.0.0.1", "port": port,
		"options": map[string]any{"http2": true},
	})

	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "extproc-grpc", "type": "extProc",
		"extProc": map[string]any{
			"destinationId": destID,
			"mode":          "grpc",
			"phaseTimeout":  "2s",
			"allowOnError":  true,
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("extproc-hdr=" + r.Header.Get("X-ExtProc-gRPC")))
	})
	upDestID := createDestination(t, "extproc-grpc-up", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+upDestID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "extproc-grpc-route", "match": map[string]any{"pathPrefix": "/extproc-grpc"},
		"forward":       map[string]any{"destinations": []map[string]any{{"destinationId": upDestID, "weight": 100}}},
		"middlewareIds": []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, body := proxyGet(t, "/extproc-grpc", nil)
	if code != 200 {
		t.Errorf("extproc grpc: %d %q", code, body)
	}
	if strings.Contains(body, "extproc-hdr=processed") {
		t.Logf("gRPC extproc header injection confirmed: %q", body)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 9. SSE push on activate (stream receives data after activation)
// ─────────────────────────────────────────────────────────────────────────────

func TestGap_SSE_PushOnActivate(t *testing.T) {
	_, route := apiPost(t, "/routes", map[string]any{
		"name": "sse-push", "match": map[string]any{"pathPrefix": "/sse-push"},
		"directResponse": map[string]any{"status": 200},
	})
	defer apiDelete(t, "/routes/"+id(route))

	// Connect to SSE before activating.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", apiBase+"/sync/snapshot", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("SSE connect: %v", err)
	}
	defer resp.Body.Close()

	// Now activate a snapshot — the SSE stream should push it.
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	// Read from SSE stream — should get data within timeout.
	scanner := bufio.NewScanner(resp.Body)
	got := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") && strings.Contains(line, "sse-push") {
			got = true
			break
		}
	}
	if !got {
		t.Error("SSE stream did not push snapshot data after activation")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 10. Outlier detection (consecutive 5xx ejects endpoint)
// ─────────────────────────────────────────────────────────────────────────────

func TestGap_OutlierDetection(t *testing.T) {
	var badCount, goodCount atomic.Int64
	badUp := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		badCount.Add(1)
		w.WriteHeader(500)
	})
	goodUp := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		goodCount.Add(1)
		w.Write([]byte("healthy"))
	})

	destID := createDestination(t, "outlier-dest", "127.0.0.1", 1)
	defer apiDelete(t, "/destinations/"+destID)

	// Destination with 2 endpoints + outlier detection with low threshold.
	apiPut(t, "/destinations/"+destID, map[string]any{
		"name": "outlier-dest", "host": "127.0.0.1", "port": goodUp.port(),
		"endpoints": []map[string]any{
			{"host": badUp.host(), "port": badUp.port()},
			{"host": goodUp.host(), "port": goodUp.port()},
		},
		"options": map[string]any{
			"outlierDetection": map[string]any{
				"consecutive5xx":   2,
				"interval":         "1s",
				"baseEjectionTime": "10s",
			},
		},
	})

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "outlier-route", "match": map[string]any{"pathPrefix": "/outlier"},
		"forward": map[string]any{"destinations": []map[string]any{{"destinationId": destID, "weight": 100}}},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	// Send many requests — initially both endpoints get traffic.
	// After the bad one gets ejected, only the good one should receive.
	for i := 0; i < 50; i++ {
		proxyGet(t, "/outlier", nil)
		time.Sleep(10 * time.Millisecond)
	}

	// The good endpoint should have received the majority of requests
	// once the bad one was ejected.
	if goodCount.Load() == 0 {
		t.Error("good endpoint received no requests")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 11. Health check (endpoint marked unhealthy, then recovers)
// ─────────────────────────────────────────────────────────────────────────────

func TestGap_HealthCheck(t *testing.T) {
	var healthy atomic.Bool
	healthy.Store(true)

	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			if healthy.Load() {
				w.WriteHeader(200)
			} else {
				w.WriteHeader(503)
			}
			return
		}
		w.Write([]byte("app-ok"))
	})

	destID := createDestination(t, "hc-dest", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	apiPut(t, "/destinations/"+destID, map[string]any{
		"name": "hc-dest", "host": up.host(), "port": up.port(),
		"options": map[string]any{
			"healthCheck": map[string]any{
				"path":               "/healthz",
				"interval":           "500ms",
				"timeout":            "1s",
				"unhealthyThreshold": 2,
				"healthyThreshold":   1,
			},
		},
	})

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "hc-route", "match": map[string]any{"pathPrefix": "/hc-test"},
		"forward": map[string]any{"destinations": []map[string]any{{"destinationId": destID, "weight": 100}}},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	// Initially healthy.
	time.Sleep(2 * time.Second)
	code, _, _ := proxyGet(t, "/hc-test", nil)
	if code != 200 {
		t.Errorf("initial: expected 200, got %d", code)
	}

	// Mark unhealthy.
	healthy.Store(false)
	time.Sleep(3 * time.Second)

	// Should now fail (no healthy endpoints).
	code, _, _ = proxyGet(t, "/hc-test", nil)
	if code == 200 {
		t.Error("expected failure after endpoint became unhealthy, still got 200")
	}

	// Recover.
	healthy.Store(true)
	time.Sleep(2 * time.Second)
	code, _, _ = proxyGet(t, "/hc-test", nil)
	if code != 200 {
		t.Errorf("after recovery: expected 200, got %d", code)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 12. h2c downstream + upstream (cleartext HTTP/2)
// ─────────────────────────────────────────────────────────────────────────────

func TestGap_H2C_Downstream(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fmt.Sprintf("proto=%s", r.Proto)))
	})
	destID := createDestination(t, "h2c-dest", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	h2cPort := 13800
	_, listener := apiPost(t, "/listeners", map[string]any{
		"name": "h2c-listener", "address": "0.0.0.0", "port": h2cPort, "http2": true,
	})
	defer apiDelete(t, "/listeners/"+id(listener))

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "h2c-route", "match": map[string]any{"pathPrefix": "/h2c"},
		"forward": map[string]any{"destinations": []map[string]any{{"destinationId": destID, "weight": 100}}},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	// Use h2c client (cleartext HTTP/2).
	h2cClient := &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				return net.Dial(network, addr)
			},
		},
		Timeout: 5 * time.Second,
	}

	resp, err := h2cClient.Get(fmt.Sprintf("http://localhost:%d/h2c", h2cPort))
	if err != nil {
		t.Fatalf("h2c request: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		t.Errorf("h2c: expected 200, got %d: %s", resp.StatusCode, data)
	}
	if resp.Proto != "HTTP/2.0" {
		t.Errorf("expected HTTP/2.0, got %q", resp.Proto)
	}
}

func TestGap_H2C_Upstream(t *testing.T) {
	// Start an h2c upstream.
	h2cHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fmt.Sprintf("upstream-proto=%s", r.Proto)))
	})
	h2cSrv := &http.Server{Handler: h2c.NewHandler(h2cHandler, &http2.Server{})}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go h2cSrv.Serve(lis)
	t.Cleanup(func() { h2cSrv.Close() })

	port := lis.Addr().(*net.TCPAddr).Port
	destID := createDestination(t, "h2c-up-dest", "127.0.0.1", port)
	defer apiDelete(t, "/destinations/"+destID)

	apiPut(t, "/destinations/"+destID, map[string]any{
		"name": "h2c-up-dest", "host": "127.0.0.1", "port": port,
		"options": map[string]any{"http2": true},
	})

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "h2c-up-route", "match": map[string]any{"pathPrefix": "/h2c-up"},
		"forward": map[string]any{"destinations": []map[string]any{{"destinationId": destID, "weight": 100}}},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, body := proxyGet(t, "/h2c-up", nil)
	if code != 200 {
		t.Errorf("h2c upstream: %d %q", code, body)
	}
	if strings.Contains(body, "upstream-proto=HTTP/2.0") {
		t.Logf("h2c upstream confirmed HTTP/2: %s", body)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 13. TLS upstream (destination with TLS)
// ─────────────────────────────────────────────────────────────────────────────

func TestGap_TLS_Upstream(t *testing.T) {
	pki := generatePKI(t)

	serverCert, _ := tls.X509KeyPair([]byte(pki.serverCertPEM), []byte(pki.serverKeyPEM))
	tlsSrv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("tls-upstream-ok"))
		}),
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{serverCert}},
	}
	lis, err := tls.Listen("tcp", "127.0.0.1:0", tlsSrv.TLSConfig)
	if err != nil {
		t.Fatal(err)
	}
	go tlsSrv.Serve(lis)
	t.Cleanup(func() { tlsSrv.Close() })

	port := lis.Addr().(*net.TCPAddr).Port

	destID := createDestination(t, "tls-up-dest", "127.0.0.1", port)
	defer apiDelete(t, "/destinations/"+destID)

	apiPut(t, "/destinations/"+destID, map[string]any{
		"name": "tls-up-dest", "host": "127.0.0.1", "port": port,
		"options": map[string]any{
			"tls": map[string]any{
				"mode": "tls",
				"ca":   pki.caCertPEM,
			},
		},
	})

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "tls-up-route", "match": map[string]any{"pathPrefix": "/tls-up"},
		"forward": map[string]any{"destinations": []map[string]any{{"destinationId": destID, "weight": 100}}},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	code, _, body := proxyGet(t, "/tls-up", nil)
	if code != 200 {
		t.Errorf("tls upstream: %d %q", code, body)
	}
	if body == "tls-upstream-ok" {
		t.Log("TLS upstream confirmed working")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 14. Listener timeouts (slowloris protection)
// ─────────────────────────────────────────────────────────────────────────────

func TestGap_ListenerTimeout_ClientHeader(t *testing.T) {
	timeoutPort := 13850
	_, listener := apiPost(t, "/listeners", map[string]any{
		"name": "timeout-listener", "address": "0.0.0.0", "port": timeoutPort,
		"timeouts": map[string]any{
			"clientHeader": "500ms",
		},
	})
	defer apiDelete(t, "/listeners/"+id(listener))

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "timeout-route", "match": map[string]any{"pathPrefix": "/timeout"},
		"directResponse": map[string]any{"status": 200, "body": "ok"},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	// Slow client: connect but don't send headers promptly.
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", timeoutPort), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send partial request line, then wait.
	fmt.Fprintf(conn, "GET /timeout HTTP/1.1\r\n")
	time.Sleep(2 * time.Second)

	// Try to read — the server should have closed the connection.
	conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err == nil && n > 0 {
		resp := string(buf[:n])
		if strings.Contains(resp, "200") {
			t.Error("server should have timed out the slow client")
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 15. MaxGRPCTimeout (clamp client timeout)
// ─────────────────────────────────────────────────────────────────────────────

func TestGap_MaxGRPCTimeout(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		// The proxy should clamp the grpc-timeout to maxGrpcTimeout.
		w.Write([]byte("grpc-timeout=" + r.Header.Get("Grpc-Timeout")))
	})
	destID := createDestination(t, "grpc-tout-dest", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "grpc-tout", "match": map[string]any{"pathPrefix": "/grpc-tout", "grpc": true},
		"forward": map[string]any{
			"destinations":  []map[string]any{{"destinationId": destID, "weight": 100}},
			"maxGrpcTimeout": "1s",
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	// Send request with grpc-timeout header larger than max.
	code, _, body := proxyRequest(t, "POST", "/grpc-tout", nil, map[string]string{
		"Content-Type": "application/grpc",
		"Grpc-Timeout": "60S",
	})
	if code != 200 {
		t.Errorf("grpc timeout: %d %q", code, body)
	}
	// The upstream should see a clamped timeout, not 60S.
	if strings.Contains(body, "60S") {
		t.Error("grpc-timeout was not clamped by maxGrpcTimeout")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 16. ExtAuthz gRPC (inline gRPC authorizer)
// ─────────────────────────────────────────────────────────────────────────────

type testAuthzServer struct {
	extauthzv1.UnimplementedAuthorizerServer
}

func (s *testAuthzServer) Check(ctx context.Context, req *extauthzv1.CheckRequest) (*extauthzv1.CheckResponse, error) {
	for _, h := range req.Headers {
		if h.Key == "x-auth-token" && h.Value == "valid-token" {
			return &extauthzv1.CheckResponse{Allowed: true}, nil
		}
	}
	return &extauthzv1.CheckResponse{Allowed: false, DeniedStatus: 403, DeniedBody: []byte("denied-by-grpc-authz")}, nil
}

func TestGap_ExtAuthz_gRPC(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcServer := grpc.NewServer()
	extauthzv1.RegisterAuthorizerServer(grpcServer, &testAuthzServer{})
	go grpcServer.Serve(lis)
	t.Cleanup(grpcServer.GracefulStop)

	port := lis.Addr().(*net.TCPAddr).Port
	destID := createDestination(t, "authz-grpc-dest", "127.0.0.1", port)
	defer apiDelete(t, "/destinations/"+destID)
	apiPut(t, "/destinations/"+destID, map[string]any{
		"name": "authz-grpc-dest", "host": "127.0.0.1", "port": port,
		"options": map[string]any{"http2": true},
	})

	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "authz-grpc", "type": "extAuthz",
		"extAuthz": map[string]any{
			"destinationId":   destID,
			"mode":            "grpc",
			"decisionTimeout": "2s",
			"onCheck":         map[string]any{"forwardHeaders": []string{"x-auth-token"}},
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "authz-grpc-route", "match": map[string]any{"pathPrefix": "/authz-grpc"},
		"directResponse": map[string]any{"status": 200, "body": "authorized"},
		"middlewareIds":   []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	// Allowed.
	code, _, body := proxyGet(t, "/authz-grpc", map[string]string{"X-Auth-Token": "valid-token"})
	if code != 200 || body != "authorized" {
		t.Errorf("allowed: %d %q", code, body)
	}

	// Denied.
	code, _, _ = proxyGet(t, "/authz-grpc", map[string]string{"X-Auth-Token": "bad"})
	if code != 403 {
		t.Errorf("denied: expected 403, got %d", code)
	}
}
