//go:build kind

// Package e2e contains end-to-end tests for the Raft cluster mode.
// These tests require a running kind cluster named "rutoso-dev" with the
// control plane StatefulSet deployed via test/k8s/cluster.yaml.
// Run with: make e2e-cluster
package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// clusterNodeIP returns the kind node IP from KIND_NODE_IP env var,
// falling back to 172.18.0.3 (kind default on Linux).
func clusterNodeIP() string {
	if ip := os.Getenv("KIND_NODE_IP"); ip != "" {
		return ip
	}
	return "172.18.0.3"
}

func clusterBaseURL() string {
	return fmt.Sprintf("http://%s:31080/api/v1", clusterNodeIP())
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func clusterPost(t *testing.T, path string, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(clusterBaseURL()+path, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(data, &result)
	return resp.StatusCode, result
}

func clusterGet(t *testing.T, path string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(clusterBaseURL() + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

func clusterDelete(t *testing.T, path string) int {
	t.Helper()
	req, _ := http.NewRequest("DELETE", clusterBaseURL()+path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func clusterID(m map[string]any) string {
	v, _ := m["id"].(string)
	return v
}

// waitClusterHealthy polls the cluster API until it responds or times out.
func waitClusterHealthy(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(clusterBaseURL() + "/snapshots")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("cluster not healthy after 30s")
}

// ─── Tests ──────────────────────────────────────────────────────────────────

// TestCluster_BasicWrite writes a destination via the NodePort Service
// (which may hit any pod) and reads it back to verify replication.
func TestCluster_BasicWrite(t *testing.T) {
	waitClusterHealthy(t)

	code, dest := clusterPost(t, "/destinations", map[string]any{
		"name": "cluster-e2e-dest", "host": "127.0.0.1", "port": 9999,
	})
	if code != 201 {
		t.Fatalf("create destination: %d %v", code, dest)
	}
	defer clusterDelete(t, "/destinations/"+clusterID(dest))

	time.Sleep(300 * time.Millisecond)

	code, _ = clusterGet(t, "/destinations/"+clusterID(dest))
	if code != 200 {
		t.Errorf("get destination: %d", code)
	}
}

// TestCluster_SnapshotActivation creates and activates a snapshot,
// verifying the cluster persists and serves it.
func TestCluster_SnapshotActivation(t *testing.T) {
	waitClusterHealthy(t)

	code, snap := clusterPost(t, "/snapshots", map[string]string{"name": "cluster-e2e-snap"})
	if code != 201 {
		t.Fatalf("create snapshot: %d %v", code, snap)
	}
	snapID := clusterID(snap)
	defer clusterDelete(t, "/snapshots/"+snapID)

	code, activated := clusterPost(t, "/snapshots/"+snapID+"/activate", nil)
	if code != 200 {
		t.Fatalf("activate snapshot: %d", code)
	}
	if activated["active"] != true {
		t.Error("expected active=true")
	}

	time.Sleep(300 * time.Millisecond)

	code, body := clusterGet(t, "/snapshots")
	if code != 200 {
		t.Fatalf("list snapshots: %d", code)
	}
	var summaries []map[string]any
	json.Unmarshal(body, &summaries)
	found := false
	for _, s := range summaries {
		if s["id"] == snapID && s["active"] == true {
			found = true
		}
	}
	if !found {
		t.Errorf("snapshot %s not found as active in list", snapID)
	}
}

// TestCluster_WriteAndReadReplicated writes several routes and reads them
// back. Each request may go to a different pod, proving replication works.
func TestCluster_WriteAndReadReplicated(t *testing.T) {
	waitClusterHealthy(t)

	var routeIDs []string
	for i := 0; i < 5; i++ {
		code, route := clusterPost(t, "/routes", map[string]any{
			"name":           fmt.Sprintf("cluster-rep-route-%d", i),
			"match":          map[string]any{"pathPrefix": fmt.Sprintf("/rep-%d", i)},
			"directResponse": map[string]any{"status": 200, "body": fmt.Sprintf("rep-%d", i)},
		})
		if code != 201 {
			t.Fatalf("create route %d: %d", i, code)
		}
		routeIDs = append(routeIDs, clusterID(route))
	}
	defer func() {
		for _, rid := range routeIDs {
			clusterDelete(t, "/routes/"+rid)
		}
	}()

	time.Sleep(500 * time.Millisecond)

	for _, rid := range routeIDs {
		code, _ := clusterGet(t, "/routes/"+rid)
		if code != 200 {
			t.Errorf("route %s not found after replication (code=%d)", rid, code)
		}
	}
}

// TestCluster_SSEStream verifies the SSE sync stream works in cluster mode.
func TestCluster_SSEStream(t *testing.T) {
	waitClusterHealthy(t)

	code, snap := clusterPost(t, "/snapshots", map[string]string{"name": "cluster-sse-snap"})
	if code != 201 {
		t.Fatalf("create snapshot: %d", code)
	}
	snapID := clusterID(snap)
	defer clusterDelete(t, "/snapshots/"+snapID)

	clusterPost(t, "/snapshots/"+snapID+"/activate", nil)

	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest("GET", clusterBaseURL()+"/sync/stream", nil)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("SSE connect: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %q", resp.Header.Get("Content-Type"))
	}

	scanner := bufio.NewScanner(resp.Body)
	found := false
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "event: snapshot") {
			found = true
			break
		}
	}
	if !found {
		t.Error("no snapshot event received from cluster SSE")
	}
}

// TestCluster_ConfigDump verifies /debug/config works in cluster mode.
func TestCluster_ConfigDump(t *testing.T) {
	waitClusterHealthy(t)

	code, body := clusterGet(t, "/debug/config")
	if code != 200 {
		t.Fatalf("config dump: %d", code)
	}
	var dump map[string]json.RawMessage
	json.Unmarshal(body, &dump)
	for _, key := range []string{"listeners", "routes", "groups", "destinations", "middlewares"} {
		if _, ok := dump[key]; !ok {
			t.Errorf("missing %q in config dump", key)
		}
	}
}
