// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

//go:build kind

// Tests verify TLS and API key authentication deployed via Helm into a kind
// cluster. Three modes are tested: self-signed (Job), cert-manager, and
// existingSecret. Each test expects the chart to already be installed with
// the matching ci/kind-tls-*-values.yaml — the Makefile targets handle this.
//
// Run with:
//
//	make server-e2e-tls-selfsigned
//	make server-e2e-tls-certmanager
//	make server-e2e-tls-existingsecret
package e2e

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// ######################## Helpers ########################

func tlsNodeIP() string {
	if ip := os.Getenv("KIND_NODE_IP"); ip != "" {
		return ip
	}
	return "172.18.0.3"
}

func tlsNodePort() string {
	if p := os.Getenv("TLS_NODEPORT"); p != "" {
		return p
	}
	return "31082"
}

func tlsNamespace() string {
	if ns := os.Getenv("TLS_NAMESPACE"); ns != "" {
		return ns
	}
	return "vrata-tls"
}

func tlsKubeContext() string {
	if ctx := os.Getenv("KUBE_CONTEXT"); ctx != "" {
		return ctx
	}
	return "kind-vrata-dev"
}

// tlsServerName returns the TLS ServerName (SNI) to use when connecting
// to the CP via NodePort. Must match a DNS SAN in the server certificate.
func tlsServerName() string {
	if sn := os.Getenv("TLS_SERVER_NAME"); sn != "" {
		return sn
	}
	return "vrata-tls-vrata-cp"
}

// tlsAPIKey returns the operator API key used in the e2e values.
func tlsAPIKey() string {
	if k := os.Getenv("TLS_API_KEY"); k != "" {
		return k
	}
	return "e2e-operator-key"
}

// extractCAFromCluster reads the ca.crt from the server TLS Secret.
// The secret name follows the helm helper convention: <release>-vrata-tls-server.
// For existingSecret mode, all three components share one secret.
func extractCAFromCluster(t *testing.T) []byte {
	t.Helper()
	secretName := os.Getenv("TLS_SERVER_SECRET")
	if secretName == "" {
		secretName = "vrata-tls-vrata-tls-server"
	}
	out, err := exec.Command("kubectl", "--context", tlsKubeContext(),
		"-n", tlsNamespace(), "get", "secret", secretName,
		"-o", "jsonpath={.data.ca\\.crt}").CombinedOutput()
	if err != nil {
		t.Fatalf("extracting CA from secret %s: %v\n%s", secretName, err, out)
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("decoding CA: %v", err)
	}
	return decoded
}

// tlsHTTPClient creates an *http.Client that trusts the CA from the cluster.
// ServerName is set to the CP service hostname so TLS verification succeeds
// when connecting via NodePort IP.
func tlsHTTPClient(t *testing.T, caPEM []byte) *http.Client {
	t.Helper()
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("CA PEM contains no valid certificates")
	}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    pool,
				ServerName: tlsServerName(),
				MinVersion: tls.VersionTLS12,
			},
		},
	}
}

func tlsGet(t *testing.T, client *http.Client, url string, apiKey string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, []byte(err.Error())
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

func waitTLSHealthy(t *testing.T, client *http.Client, baseURL, apiKey string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		code, _ := tlsGet(t, client, baseURL+"/api/v1/snapshots", apiKey)
		if code == 200 {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatal("TLS control plane not healthy after 60s")
}

// ######################## Tests ########################

// TestTLS_HTTPSConnection verifies the control plane serves HTTPS with a
// valid certificate that the cluster CA can verify.
func TestTLS_HTTPSConnection(t *testing.T) {
	caPEM := extractCAFromCluster(t)
	client := tlsHTTPClient(t, caPEM)
	baseURL := fmt.Sprintf("https://%s:%s", tlsNodeIP(), tlsNodePort())

	waitTLSHealthy(t, client, baseURL, tlsAPIKey())

	code, body := tlsGet(t, client, baseURL+"/api/v1/snapshots", tlsAPIKey())
	if code != 200 {
		t.Fatalf("expected 200, got %d: %s", code, body)
	}
}

// TestTLS_RejectsWithoutAPIKey verifies the CP returns 401 when no API key
// is provided.
func TestTLS_RejectsWithoutAPIKey(t *testing.T) {
	caPEM := extractCAFromCluster(t)
	client := tlsHTTPClient(t, caPEM)
	baseURL := fmt.Sprintf("https://%s:%s", tlsNodeIP(), tlsNodePort())

	waitTLSHealthy(t, client, baseURL, tlsAPIKey())

	code, _ := tlsGet(t, client, baseURL+"/api/v1/snapshots", "")
	if code != 401 {
		t.Errorf("expected 401 without API key, got %d", code)
	}
}

// TestTLS_RejectsInvalidAPIKey verifies the CP returns 401 for a wrong key.
func TestTLS_RejectsInvalidAPIKey(t *testing.T) {
	caPEM := extractCAFromCluster(t)
	client := tlsHTTPClient(t, caPEM)
	baseURL := fmt.Sprintf("https://%s:%s", tlsNodeIP(), tlsNodePort())

	waitTLSHealthy(t, client, baseURL, tlsAPIKey())

	code, _ := tlsGet(t, client, baseURL+"/api/v1/snapshots", "wrong-key")
	if code != 401 {
		t.Errorf("expected 401 with wrong API key, got %d", code)
	}
}

// TestTLS_RejectsPlainHTTP verifies that a plain HTTP connection to the TLS
// endpoint either fails or returns a non-200 response (the server sends a
// TLS handshake alert that the HTTP client sees as garbage).
func TestTLS_RejectsPlainHTTP(t *testing.T) {
	plainURL := fmt.Sprintf("http://%s:%s/api/v1/snapshots", tlsNodeIP(), tlsNodePort())
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(plainURL)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		t.Fatal("plain HTTP should not get 200 from a TLS endpoint")
	}
}

// TestTLS_RejectsUntrustedCA verifies the client rejects the server cert
// when using the wrong CA.
func TestTLS_RejectsUntrustedCA(t *testing.T) {
	// Use an empty CA pool — will not trust the server cert.
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    x509.NewCertPool(),
				MinVersion: tls.VersionTLS12,
			},
		},
	}
	url := fmt.Sprintf("https://%s:%s/api/v1/snapshots", tlsNodeIP(), tlsNodePort())
	_, err := client.Get(url)
	if err == nil {
		t.Fatal("expected TLS error when server cert is not trusted")
	}
	if !strings.Contains(err.Error(), "certificate") {
		t.Errorf("expected certificate error, got: %v", err)
	}
}

// TestTLS_AllAPIKeysWork verifies all configured API keys are accepted.
func TestTLS_AllAPIKeysWork(t *testing.T) {
	caPEM := extractCAFromCluster(t)
	client := tlsHTTPClient(t, caPEM)
	baseURL := fmt.Sprintf("https://%s:%s", tlsNodeIP(), tlsNodePort())

	waitTLSHealthy(t, client, baseURL, tlsAPIKey())

	keys := []string{"e2e-proxy-key", "e2e-controller-key", "e2e-operator-key"}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			code, body := tlsGet(t, client, baseURL+"/api/v1/snapshots", key)
			if code != 200 {
				t.Errorf("expected 200 for key %q, got %d: %s", key, code, body)
			}
		})
	}
}

// TestTLS_ProxyPodIsRunning verifies that the proxy pod connected to the
// TLS-enabled control plane and is in Ready state.
func TestTLS_ProxyPodIsRunning(t *testing.T) {
	out, err := exec.Command("kubectl", "--context", tlsKubeContext(),
		"-n", tlsNamespace(),
		"get", "pods", "-l", "app.kubernetes.io/component=proxy",
		"-o", "jsonpath={.items[*].status.phase}").CombinedOutput()
	if err != nil {
		t.Fatalf("kubectl get pods: %v\n%s", err, out)
	}
	phases := strings.TrimSpace(string(out))
	if phases == "" {
		t.Fatal("no proxy pods found")
	}
	for _, phase := range strings.Fields(phases) {
		if phase != "Running" {
			t.Errorf("proxy pod phase: %s (expected Running)", phase)
		}
	}
}

// TestTLS_CRUDWithAuth does a full CRUD cycle (create + read + delete) on a
// destination to verify the auth middleware allows authenticated writes.
func TestTLS_CRUDWithAuth(t *testing.T) {
	caPEM := extractCAFromCluster(t)
	client := tlsHTTPClient(t, caPEM)
	baseURL := fmt.Sprintf("https://%s:%s", tlsNodeIP(), tlsNodePort())
	apiKey := tlsAPIKey()

	waitTLSHealthy(t, client, baseURL, apiKey)

	// Create
	body := `{"name":"tls-e2e-dest","host":"10.0.0.1","port":80}`
	req, _ := http.NewRequest("POST", baseURL+"/api/v1/destinations", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: expected 201, got %d: %s", resp.StatusCode, b)
	}
	var created map[string]any
	json.NewDecoder(resp.Body).Decode(&created)
	destID := created["id"].(string)

	// Read
	code, _ := tlsGet(t, client, baseURL+"/api/v1/destinations/"+destID, apiKey)
	if code != 200 {
		t.Errorf("get: expected 200, got %d", code)
	}

	// Delete
	delReq, _ := http.NewRequest("DELETE", baseURL+"/api/v1/destinations/"+destID, nil)
	delReq.Header.Set("Authorization", "Bearer "+apiKey)
	delResp, err := client.Do(delReq)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	delResp.Body.Close()
	if delResp.StatusCode != 204 {
		t.Errorf("delete: expected 204, got %d", delResp.StatusCode)
	}
}
