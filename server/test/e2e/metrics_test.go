// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const metricsListenerPort = 13900

func metricsURL(path string) string {
	return fmt.Sprintf("http://localhost:%d%s", metricsListenerPort, path)
}

func scrapeMetrics(t *testing.T) string {
	t.Helper()
	resp, err := http.Get(metricsURL("/metrics"))
	if err != nil {
		t.Fatalf("scraping /metrics: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("GET /metrics returned %d: %s", resp.StatusCode, data)
	}
	return string(data)
}

func metricValue(body, name string, labels map[string]string) (string, bool) {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if !strings.HasPrefix(line, name) {
			continue
		}
		match := true
		for k, v := range labels {
			needle := fmt.Sprintf(`%s="%s"`, k, v)
			if !strings.Contains(line, needle) {
				match = false
				break
			}
		}
		if match {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[len(parts)-1], true
			}
		}
	}
	return "", false
}

func TestE2E_Metrics_RouteCounters(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("metrics-ok"))
	})
	destID := createDestination(t, "metrics-dest", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, listener := apiPost(t, "/listeners", map[string]any{
		"name":    "metrics-listener",
		"address": "0.0.0.0",
		"port":    metricsListenerPort,
		"metrics": map[string]any{
			"path": "/metrics",
			"collect": map[string]any{
				"route":       true,
				"destination": true,
				"endpoint":    true,
				"middleware":   true,
				"listener":    true,
			},
		},
	})
	defer apiDelete(t, "/listeners/"+id(listener))

	_, route := apiPost(t, "/routes", map[string]any{
		"name":    "metrics-route",
		"match":   map[string]any{"pathPrefix": "/e2e-metrics"},
		"forward": map[string]any{"destinations": []map[string]any{{"destinationId": destID, "weight": 100}}},
	})
	defer apiDelete(t, "/routes/"+id(route))

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	time.Sleep(500 * time.Millisecond)

	client := &http.Client{Timeout: 5 * time.Second}
	for i := 0; i < 10; i++ {
		resp, err := client.Get(metricsURL("/e2e-metrics"))
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("request %d: got %d", i, resp.StatusCode)
		}
	}

	time.Sleep(200 * time.Millisecond)
	body := scrapeMetrics(t)

	val, ok := metricValue(body, "vrata_route_requests_total", map[string]string{
		"route":        "metrics-route",
		"method":       "GET",
		"status_code":  "200",
		"status_class": "2xx",
	})
	if !ok {
		t.Fatalf("vrata_route_requests_total not found in metrics output:\n%s", body)
	}
	if val != "10" {
		t.Errorf("expected 10 requests, got %s", val)
	}

	_, ok = metricValue(body, "vrata_route_duration_seconds_count", map[string]string{
		"route":  "metrics-route",
		"method": "GET",
	})
	if !ok {
		t.Error("vrata_route_duration_seconds_count not found")
	}
}

func TestE2E_Metrics_DestinationCounters(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})
	destID := createDestination(t, "metrics-dest-2", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, listener := apiPost(t, "/listeners", map[string]any{
		"name": "metrics-listener-2", "address": "0.0.0.0", "port": metricsListenerPort + 1,
		"metrics": map[string]any{
			"path": "/metrics",
			"collect": map[string]any{
				"route":       true,
				"destination": true,
				"endpoint":    true,
			},
		},
	})
	defer apiDelete(t, "/listeners/"+id(listener))

	_, route := apiPost(t, "/routes", map[string]any{
		"name":    "metrics-route-dest",
		"match":   map[string]any{"pathPrefix": "/e2e-metrics-dest"},
		"forward": map[string]any{"destinations": []map[string]any{{"destinationId": destID, "weight": 100}}},
	})
	defer apiDelete(t, "/routes/"+id(route))

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	time.Sleep(500 * time.Millisecond)

	listenerURL := fmt.Sprintf("http://localhost:%d", metricsListenerPort+1)
	client := &http.Client{Timeout: 5 * time.Second}
	for i := 0; i < 5; i++ {
		resp, err := client.Get(listenerURL + "/e2e-metrics-dest")
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp.Body.Close()
	}

	time.Sleep(200 * time.Millisecond)
	resp, err := client.Get(listenerURL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	body := string(data)

	val, ok := metricValue(body, "vrata_destination_requests_total", map[string]string{
		"status_code":  "200",
		"status_class": "2xx",
	})
	if !ok {
		t.Fatalf("vrata_destination_requests_total not found:\n%s", body)
	}
	if val != "5" {
		t.Errorf("expected 5 destination requests, got %s", val)
	}

	_, ok = metricValue(body, "vrata_endpoint_requests_total", map[string]string{
		"status_code": "200",
	})
	if !ok {
		t.Error("vrata_endpoint_requests_total not found (endpoint collection enabled)")
	}
}

func TestE2E_Metrics_MiddlewareTracking(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})
	destID := createDestination(t, "metrics-dest-mw", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "metrics-hdr-mw", "type": "headers",
		"headers": map[string]any{"responseHeadersToAdd": []map[string]any{{"key": "X-Metrics-E2E", "value": "true"}}},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	_, listener := apiPost(t, "/listeners", map[string]any{
		"name": "metrics-listener-mw", "address": "0.0.0.0", "port": metricsListenerPort + 2,
		"metrics": map[string]any{"path": "/metrics", "collect": map[string]any{"middleware": true}},
	})
	defer apiDelete(t, "/listeners/"+id(listener))

	_, route := apiPost(t, "/routes", map[string]any{
		"name":          "metrics-mw-route",
		"match":         map[string]any{"pathPrefix": "/e2e-metrics-mw"},
		"forward":       map[string]any{"destinations": []map[string]any{{"destinationId": destID, "weight": 100}}},
		"middlewareIds": []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	time.Sleep(500 * time.Millisecond)

	listenerURL := fmt.Sprintf("http://localhost:%d", metricsListenerPort+2)
	client := &http.Client{Timeout: 5 * time.Second}
	for i := 0; i < 3; i++ {
		resp, err := client.Get(listenerURL + "/e2e-metrics-mw")
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp.Body.Close()
	}

	time.Sleep(200 * time.Millisecond)
	resp, err := client.Get(listenerURL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	body := string(data)

	val, ok := metricValue(body, "vrata_middleware_passed_total", map[string]string{
		"middleware": "metrics-hdr-mw",
		"type":       "headers",
	})
	if !ok {
		t.Fatalf("vrata_middleware_passed_total not found:\n%s", body)
	}
	if val != "3" {
		t.Errorf("expected 3 middleware passes, got %s", val)
	}

	_, ok = metricValue(body, "vrata_middleware_duration_seconds_count", map[string]string{
		"middleware": "metrics-hdr-mw",
		"type":       "headers",
	})
	if !ok {
		t.Error("vrata_middleware_duration_seconds_count not found")
	}
}

func TestE2E_Metrics_EndpointDisabledByDefault(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	destID := createDestination(t, "metrics-dest-noep", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, listener := apiPost(t, "/listeners", map[string]any{
		"name": "metrics-listener-noep", "address": "0.0.0.0", "port": metricsListenerPort + 3,
		"metrics": map[string]any{"path": "/metrics"},
	})
	defer apiDelete(t, "/listeners/"+id(listener))

	_, route := apiPost(t, "/routes", map[string]any{
		"name":    "metrics-noep-route",
		"match":   map[string]any{"pathPrefix": "/e2e-metrics-noep"},
		"forward": map[string]any{"destinations": []map[string]any{{"destinationId": destID, "weight": 100}}},
	})
	defer apiDelete(t, "/routes/"+id(route))

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	time.Sleep(500 * time.Millisecond)

	listenerURL := fmt.Sprintf("http://localhost:%d", metricsListenerPort+3)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(listenerURL + "/e2e-metrics-noep")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	time.Sleep(200 * time.Millisecond)
	resp, err = client.Get(listenerURL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	body := string(data)

	if strings.Contains(body, "vrata_endpoint_requests_total") {
		t.Error("endpoint metrics should not be present when collect.endpoint is not set (default false)")
	}

	if !strings.Contains(body, "vrata_route_requests_total") {
		t.Error("route metrics should be present by default")
	}
	if !strings.Contains(body, "vrata_destination_requests_total") {
		t.Error("destination metrics should be present by default")
	}
}

func TestE2E_Metrics_ScrapeEndpointPath(t *testing.T) {
	_, listener := apiPost(t, "/listeners", map[string]any{
		"name": "metrics-listener-path", "address": "0.0.0.0", "port": metricsListenerPort + 4,
		"metrics": map[string]any{"path": "/custom-metrics"},
	})
	defer apiDelete(t, "/listeners/"+id(listener))

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	time.Sleep(500 * time.Millisecond)

	listenerURL := fmt.Sprintf("http://localhost:%d", metricsListenerPort+4)
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(listenerURL + "/custom-metrics")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("custom path: expected 200, got %d", resp.StatusCode)
	}

	resp, err = client.Get(listenerURL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == 200 {
		data, _ := io.ReadAll(resp.Body)
		if strings.Contains(string(data), "vrata_") {
			t.Error("/metrics should not serve metrics when path is /custom-metrics")
		}
	}
}
