// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

// proxyListenerID holds the ID of the shared listener on port 3000 created
// by TestMain. Tests that use proxyGet / proxyRequest rely on it.
var proxyListenerID string

func TestMain(m *testing.M) {
	// Check if the control plane is reachable.
	resp, err := http.Get(apiBase + "/routes")
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: control plane not reachable at %s: %v\n", apiBase, err)
		fmt.Fprintln(os.Stderr, "e2e: start vrata in controlplane mode before running e2e tests")
		os.Exit(1)
	}
	resp.Body.Close()

	// Create a shared listener on port 3000 for proxy routing tests.
	proxyListenerID, err = ensureProxyListener()
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: failed to create proxy listener on :3000: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	// Cleanup: delete the shared listener.
	if proxyListenerID != "" {
		req, _ := http.NewRequest("DELETE", apiBase+"/listeners/"+proxyListenerID, nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}

	os.Exit(code)
}

// ensureProxyListener creates a listener on port 3000 if one doesn't exist.
func ensureProxyListener() (string, error) {
	body, _ := json.Marshal(map[string]any{
		"name":    "e2e-proxy-listener",
		"address": "0.0.0.0",
		"port":    3000,
	})
	resp, err := http.Post(apiBase+"/listeners", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("POST /listeners: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 201 {
		return "", fmt.Errorf("POST /listeners returned %d: %s", resp.StatusCode, data)
	}

	var result map[string]any
	json.Unmarshal(data, &result)
	listenerID, ok := result["id"].(string)
	if !ok {
		return "", fmt.Errorf("listener response missing id: %s", data)
	}

	// Give the gateway time to start the listener.
	time.Sleep(500 * time.Millisecond)
	return listenerID, nil
}
