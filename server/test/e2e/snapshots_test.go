package e2e

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestE2E_SnapshotLifecycle(t *testing.T) {
	code, snap := apiPost(t, "/snapshots", map[string]string{"name": "e2e-snap"})
	if code != 201 {
		t.Fatalf("create: %d", code)
	}
	snapID := id(snap)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, body := apiGet(t, "/snapshots")
	if code != 200 {
		t.Fatalf("list: %d", code)
	}
	var summaries []map[string]any
	json.Unmarshal(body, &summaries)
	found := false
	for _, s := range summaries {
		if s["id"] == snapID {
			found = true
			if s["active"] != false {
				t.Error("should not be active yet")
			}
		}
	}
	if !found {
		t.Error("not in list")
	}

	code, activated := apiPost(t, "/snapshots/"+snapID+"/activate", nil)
	if code != 200 || activated["active"] != true {
		t.Fatalf("activate: %d", code)
	}

	apiDelete(t, "/snapshots/"+snapID)
}

func TestE2E_SyncSnapshot(t *testing.T) {
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	client := &http.Client{Timeout: 3 * time.Second}
	req, _ := http.NewRequest("GET", apiBase+"/sync/snapshot", nil)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("sse: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("content-type: %q", resp.Header.Get("Content-Type"))
	}

	scanner := bufio.NewScanner(resp.Body)
	foundSnapshot := false
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "event: snapshot") {
			foundSnapshot = true
			break
		}
	}
	if !foundSnapshot {
		t.Error("no snapshot event received")
	}
}
