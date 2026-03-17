// Package e2e runs end-to-end tests against a live Rutoso control plane
// and proxy. Tests spin up their own helper servers (JWKS, auth, processor,
// WebSocket, controllable upstreams) and create all required Rutoso
// entities via the API. Every test cleans up after itself.
//
// Requirements:
//   - Control plane on localhost:8080
//   - Proxy on localhost:3000
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	apiBase  = "http://localhost:8080/api/v1"
	proxyURL = "http://localhost:3000"
)

// ─── API helpers ────────────────────────────────────────────────────────────

func apiPost(t *testing.T, path string, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(apiBase+path, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(data, &result)
	return resp.StatusCode, result
}

func apiGet(t *testing.T, path string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(apiBase + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

func apiDelete(t *testing.T, path string) int {
	t.Helper()
	req, _ := http.NewRequest("DELETE", apiBase+path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func apiPut(t *testing.T, path string, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("PUT", apiBase+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(data, &result)
	return resp.StatusCode, result
}

func proxyGet(t *testing.T, path string, headers map[string]string) (int, http.Header, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", proxyURL+path, nil)
	for k, v := range headers {
		if strings.EqualFold(k, "Host") {
			req.Host = v
		} else {
			req.Header.Set(k, v)
		}
	}
	client := &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("proxy GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(data)
}

func proxyRequest(t *testing.T, method, path string, body []byte, headers map[string]string) (int, http.Header, string) {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, proxyURL+path, bodyReader)
	for k, v := range headers {
		if strings.EqualFold(k, "Host") {
			req.Host = v
		} else {
			req.Header.Set(k, v)
		}
	}
	client := &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("proxy %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(data)
}

func id(m map[string]any) string { return m["id"].(string) }

func activateSnapshot(t *testing.T) string {
	t.Helper()
	code, result := apiPost(t, "/snapshots", map[string]string{"name": fmt.Sprintf("e2e-%d", time.Now().UnixNano())})
	if code != 201 {
		t.Fatalf("create snapshot: %d %v", code, result)
	}
	snapID := id(result)
	code, _ = apiPost(t, "/snapshots/"+snapID+"/activate", nil)
	if code != 200 {
		t.Fatalf("activate snapshot: %d", code)
	}
	time.Sleep(500 * time.Millisecond)
	return snapID
}

func createDestination(t *testing.T, name, host string, port int) string {
	t.Helper()
	_, d := apiPost(t, "/destinations", map[string]any{"name": name, "host": host, "port": port})
	if d["id"] == nil {
		t.Fatalf("create destination %s failed", name)
	}
	return id(d)
}

// ─── Controllable upstream ──────────────────────────────────────────────────

type testUpstream struct {
	*httptest.Server
	requestCount atomic.Int64
	lastBody     atomic.Value
}

func startUpstream(t *testing.T, handler http.HandlerFunc) *testUpstream {
	t.Helper()
	u := &testUpstream{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.requestCount.Add(1)
		body, _ := io.ReadAll(r.Body)
		u.lastBody.Store(string(body))
		handler(w, r)
	}))
	t.Cleanup(u.Close)
	return u
}

func (u *testUpstream) host() string {
	return u.Listener.Addr().(*net.TCPAddr).IP.String()
}

func (u *testUpstream) port() int {
	return u.Listener.Addr().(*net.TCPAddr).Port
}
