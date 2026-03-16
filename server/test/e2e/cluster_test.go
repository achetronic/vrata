//go:build kind

// Package e2e contains end-to-end tests for the Raft cluster mode.
// These tests verify that all control plane nodes are indistinguishable
// for proxies — same data, same snapshots, same SSE stream content.
//
// Run with: make e2e-cluster
package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// clusterNodeIP returns the kind node IP from KIND_NODE_IP env var.
func clusterNodeIP() string {
	if ip := os.Getenv("KIND_NODE_IP"); ip != "" {
		return ip
	}
	return "172.18.0.3"
}

func clusterBaseURL() string {
	port := os.Getenv("CLUSTER_NODEPORT")
	if port == "" {
		port = "31081"
	}
	return fmt.Sprintf("http://%s:%s/api/v1", clusterNodeIP(), port)
}

// clusterNamespace returns the Kubernetes namespace where the chart is installed.
// Reads from CLUSTER_NAMESPACE env var, defaults to "vrata-e2e".
func clusterNamespace() string {
	if ns := os.Getenv("CLUSTER_NAMESPACE"); ns != "" {
		return ns
	}
	return "vrata-e2e"
}

// clusterPods returns the pod names for the 3-node control plane StatefulSet.
// The pod name prefix is derived from HELM_RELEASE and CHART_NAME env vars.
// Defaults match the Makefile: release=vrata-e2e, chart=vrata → vrata-e2e-vrata-cp-{0,1,2}
func clusterPods() []string {
	release := os.Getenv("HELM_RELEASE")
	if release == "" {
		release = "vrata-e2e"
	}
	prefix := fmt.Sprintf("%s-vrata-cp", release)
	return []string{
		prefix + "-0",
		prefix + "-1",
		prefix + "-2",
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func clusterPost(t *testing.T, base, path string, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(base+path, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s%s: %v", base, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(data, &result)
	return resp.StatusCode, result
}

func clusterGet(t *testing.T, base, path string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatalf("GET %s%s: %v", base, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

func clusterDelete(t *testing.T, base, path string) int {
	t.Helper()
	req, _ := http.NewRequest("DELETE", base+path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s%s: %v", base, path, err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func clusterID(m map[string]any) string {
	v, _ := m["id"].(string)
	return v
}

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

// portForward starts a kubectl port-forward to the given pod and returns
// the local base URL and a cleanup function.
func portForward(t *testing.T, pod string) (string, func()) {
	t.Helper()
	localPort := freeTestPort(t)
	kubeCtx := os.Getenv("KUBE_CONTEXT")
	if kubeCtx == "" {
		kubeCtx = "kind-vrata-dev"
	}
	cmd := exec.Command("kubectl", "--context", kubeCtx,
		"-n", clusterNamespace(), "port-forward", pod,
		fmt.Sprintf("%d:8080", localPort))
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		t.Fatalf("port-forward %s: %v", pod, err)
	}

	base := fmt.Sprintf("http://127.0.0.1:%d/api/v1", localPort)

	// Wait for the port-forward to be ready.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", localPort), 500*time.Millisecond)
		if err == nil {
			conn.Close()
			return base, func() { cmd.Process.Kill(); cmd.Wait() }
		}
		time.Sleep(200 * time.Millisecond)
	}
	cmd.Process.Kill()
	cmd.Wait()
	t.Fatalf("port-forward to %s not ready after 10s", pod)
	return "", nil
}

func freeTestPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func waitForReplication(t *testing.T) {
	t.Helper()
	time.Sleep(500 * time.Millisecond)
}

// ─── Basic Tests ────────────────────────────────────────────────────────────

// TestCluster_BasicWrite writes via the NodePort (any pod) and reads back.
func TestCluster_BasicWrite(t *testing.T) {
	waitClusterHealthy(t)
	base := clusterBaseURL()

	code, dest := clusterPost(t, base, "/destinations", map[string]any{
		"name": "cluster-e2e-dest", "host": "127.0.0.1", "port": 9999,
	})
	if code != 201 {
		t.Fatalf("create: %d", code)
	}
	defer clusterDelete(t, base, "/destinations/"+clusterID(dest))
	waitForReplication(t)

	code, _ = clusterGet(t, base, "/destinations/"+clusterID(dest))
	if code != 200 {
		t.Errorf("get: %d", code)
	}
}

// TestCluster_SnapshotActivation creates and activates a snapshot.
func TestCluster_SnapshotActivation(t *testing.T) {
	waitClusterHealthy(t)
	base := clusterBaseURL()

	code, snap := clusterPost(t, base, "/snapshots", map[string]string{"name": "cluster-snap"})
	if code != 201 {
		t.Fatalf("create: %d", code)
	}
	snapID := clusterID(snap)
	defer clusterDelete(t, base, "/snapshots/"+snapID)

	code, activated := clusterPost(t, base, "/snapshots/"+snapID+"/activate", nil)
	if code != 200 {
		t.Fatalf("activate: %d", code)
	}
	if activated["active"] != true {
		t.Error("expected active=true")
	}
	waitForReplication(t)

	code, body := clusterGet(t, base, "/snapshots")
	if code != 200 {
		t.Fatalf("list: %d", code)
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
		t.Errorf("snapshot %s not active in list", snapID)
	}
}

// ─── Indistinguishable Nodes Tests ──────────────────────────────────────────

// TestCluster_AllNodesHaveSameData writes data via NodePort and verifies
// it is readable from EACH individual node via port-forward.
func TestCluster_AllNodesHaveSameData(t *testing.T) {
	waitClusterHealthy(t)
	base := clusterBaseURL()

	code, route := clusterPost(t, base, "/routes", map[string]any{
		"name":           "cluster-same-data",
		"match":          map[string]any{"pathPrefix": "/same"},
		"directResponse": map[string]any{"status": 200, "body": "same"},
	})
	if code != 201 {
		t.Fatalf("create route: %d", code)
	}
	routeID := clusterID(route)
	defer clusterDelete(t, base, "/routes/"+routeID)

	waitForReplication(t)

	for _, pod := range clusterPods() {
		podBase, cleanup := portForward(t, pod)
		defer cleanup()

		code, data := clusterGet(t, podBase, "/routes/"+routeID)
		if code != 200 {
			t.Errorf("%s: route not found (code=%d)", pod, code)
			continue
		}
		var got map[string]any
		json.Unmarshal(data, &got)
		if got["name"] != "cluster-same-data" {
			t.Errorf("%s: expected name 'cluster-same-data', got %v", pod, got["name"])
		}
	}
}

// TestCluster_SnapshotIdenticalAcrossNodes creates a full config with
// multiple entities, takes a snapshot, activates it, and verifies that
// every node serves the exact same snapshot payload.
func TestCluster_SnapshotIdenticalAcrossNodes(t *testing.T) {
	waitClusterHealthy(t)
	base := clusterBaseURL()

	_, dest := clusterPost(t, base, "/destinations", map[string]any{
		"name": "cluster-id-dest", "host": "10.0.0.1", "port": 80,
	})
	defer clusterDelete(t, base, "/destinations/"+clusterID(dest))

	_, route := clusterPost(t, base, "/routes", map[string]any{
		"name":           "cluster-id-route",
		"match":          map[string]any{"pathPrefix": "/id-test"},
		"directResponse": map[string]any{"status": 200, "body": "id"},
	})
	defer clusterDelete(t, base, "/routes/"+clusterID(route))

	_, listener := clusterPost(t, base, "/listeners", map[string]any{
		"name": "cluster-id-listener", "port": 19876,
	})
	defer clusterDelete(t, base, "/listeners/"+clusterID(listener))

	waitForReplication(t)

	_, snap := clusterPost(t, base, "/snapshots", map[string]string{"name": "cluster-id-snap"})
	snapID := clusterID(snap)
	defer clusterDelete(t, base, "/snapshots/"+snapID)
	clusterPost(t, base, "/snapshots/"+snapID+"/activate", nil)

	waitForReplication(t)

	var snapshots []string
	for _, pod := range clusterPods() {
		podBase, cleanup := portForward(t, pod)
		defer cleanup()

		code, data := clusterGet(t, podBase, "/snapshots/"+snapID)
		if code != 200 {
			t.Errorf("%s: snapshot not found (code=%d)", pod, code)
			continue
		}
		snapshots = append(snapshots, string(data))
	}

	if len(snapshots) < 3 {
		t.Fatal("could not read snapshot from all 3 nodes")
	}
	for i := 1; i < len(snapshots); i++ {
		if snapshots[i] != snapshots[0] {
			t.Errorf("snapshot on node %d differs from node 0", i)
		}
	}
}

// TestCluster_WriteOnFollowerReplicates writes directly to a follower
// via port-forward and verifies the data appears on all nodes.
func TestCluster_WriteOnFollowerReplicates(t *testing.T) {
	waitClusterHealthy(t)
	base := clusterBaseURL()

	// Port-forward to each node.
	bases := make(map[string]string)
	for _, pod := range clusterPods() {
		podBase, cleanup := portForward(t, pod)
		defer cleanup()
		bases[pod] = podBase
	}

	// Write to cp-2 (likely a follower).
	code, mw := clusterPost(t, bases[clusterPods()[2]], "/middlewares", map[string]any{
		"name": "cluster-follower-write", "type": "headers",
		"headers": map[string]any{
			"requestHeadersToAdd": []map[string]any{{"key": "X-Cluster", "value": "true"}},
		},
	})
	if code != 201 {
		t.Fatalf("write on cp-2: %d", code)
	}
	mwID := clusterID(mw)
	defer clusterDelete(t, base, "/middlewares/"+mwID)

	waitForReplication(t)

	// Verify on all nodes.
	for pod, podBase := range bases {
		code, _ := clusterGet(t, podBase, "/middlewares/"+mwID)
		if code != 200 {
			t.Errorf("%s: middleware not found after follower write (code=%d)", pod, code)
		}
	}
}

// TestCluster_ConcurrentWrites sends writes concurrently to different nodes
// and verifies all data converges.
func TestCluster_ConcurrentWrites(t *testing.T) {
	waitClusterHealthy(t)
	base := clusterBaseURL()

	bases := make(map[string]string)
	for _, pod := range clusterPods() {
		podBase, cleanup := portForward(t, pod)
		defer cleanup()
		bases[pod] = podBase
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var routeIDs []string

	pods := clusterPods()
	for i := 0; i < 9; i++ {
		pod := pods[i%3]
		idx := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, route := clusterPost(t, bases[pod], "/routes", map[string]any{
				"name":           fmt.Sprintf("concurrent-%d", idx),
				"match":          map[string]any{"pathPrefix": fmt.Sprintf("/concurrent-%d", idx)},
				"directResponse": map[string]any{"status": 200, "body": fmt.Sprintf("c-%d", idx)},
			})
			if code != 201 {
				t.Errorf("concurrent write %d on %s: %d", idx, pod, code)
				return
			}
			mu.Lock()
			routeIDs = append(routeIDs, clusterID(route))
			mu.Unlock()
		}()
	}
	wg.Wait()

	defer func() {
		for _, rid := range routeIDs {
			clusterDelete(t, base, "/routes/"+rid)
		}
	}()

	waitForReplication(t)

	// Verify all 9 routes exist on every node.
	for pod, podBase := range bases {
		for _, rid := range routeIDs {
			code, _ := clusterGet(t, podBase, "/routes/"+rid)
			if code != 200 {
				t.Errorf("%s: route %s missing after concurrent writes (code=%d)", pod, rid, code)
			}
		}
	}
}

// TestCluster_SSEStreamServesActiveSnapshot connects to each node's SSE
// stream and verifies they all serve the same active snapshot.
func TestCluster_SSEStreamServesActiveSnapshot(t *testing.T) {
	waitClusterHealthy(t)
	base := clusterBaseURL()

	_, snap := clusterPost(t, base, "/snapshots", map[string]string{"name": "cluster-sse-identical"})
	snapID := clusterID(snap)
	defer clusterDelete(t, base, "/snapshots/"+snapID)
	clusterPost(t, base, "/snapshots/"+snapID+"/activate", nil)

	waitForReplication(t)

	for _, pod := range clusterPods() {
		podBase, cleanup := portForward(t, pod)
		defer cleanup()

		client := &http.Client{Timeout: 5 * time.Second}
		req, _ := http.NewRequest("GET", podBase+"/sync/stream", nil)
		req.Header.Set("Accept", "text/event-stream")
		resp, err := client.Do(req)
		if err != nil {
			t.Errorf("%s: SSE connect: %v", pod, err)
			continue
		}

		scanner := bufio.NewScanner(resp.Body)
		found := false
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				var vs map[string]any
				json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &vs)
				if vs["id"] == snapID {
					found = true
				}
				break
			}
		}
		resp.Body.Close()

		if !found {
			t.Errorf("%s: SSE did not serve snapshot %s", pod, snapID)
		}
	}
}

// TestCluster_ConfigDump verifies /debug/config on each node.
func TestCluster_ConfigDump(t *testing.T) {
	waitClusterHealthy(t)

	for _, pod := range clusterPods() {
		podBase, cleanup := portForward(t, pod)
		defer cleanup()

		code, body := clusterGet(t, podBase, "/debug/config")
		if code != 200 {
			t.Errorf("%s: config dump: %d", pod, code)
			continue
		}
		var dump map[string]json.RawMessage
		json.Unmarshal(body, &dump)
		for _, key := range []string{"listeners", "routes", "groups", "destinations", "middlewares"} {
			if _, ok := dump[key]; !ok {
				t.Errorf("%s: missing %q in config dump", pod, key)
			}
		}
	}
}
