package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func TestE2E_Proxy_Retry(t *testing.T) {
	var count atomic.Int64
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		n := count.Add(1)
		if n <= 2 {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte("ok-after-retry"))
	})
	destID := createDestination(t, "e2e-retry", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-retry", "match": map[string]any{"pathPrefix": "/e2e-retry"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
			"retry":    map[string]any{"attempts": 3, "on": []string{"server-error"}},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyGet(t, "/e2e-retry", nil)
	if code != 200 || body != "ok-after-retry" {
		t.Errorf("retry: %d %q (count=%d)", code, body, count.Load())
	}
	if count.Load() < 3 {
		t.Errorf("expected >= 3 attempts, got %d", count.Load())
	}
}

func TestE2E_Proxy_RequestTimeout(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.Write([]byte("too-late"))
	})
	destID := createDestination(t, "e2e-timeout", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-timeout", "match": map[string]any{"pathPrefix": "/e2e-timeout"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
			"timeouts": map[string]any{"request": "500ms"},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, _ := proxyGet(t, "/e2e-timeout", nil)
	if code != 503 {
		t.Errorf("timeout: expected 503, got %d", code)
	}
}

func TestE2E_Proxy_Mirror(t *testing.T) {
	up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("primary"))
	})
	mirror := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})

	upDestID := createDestination(t, "e2e-mirror-up", up.host(), up.port())
	defer apiDelete(t, "/destinations/"+upDestID)
	mirrorDestID := createDestination(t, "e2e-mirror-dest", mirror.host(), mirror.port())
	defer apiDelete(t, "/destinations/"+mirrorDestID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-mirror", "match": map[string]any{"pathPrefix": "/e2e-mirror"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": upDestID, "weight": 100}},
			"mirror":   map[string]any{"destinationId": mirrorDestID, "percentage": 100},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	code, _, body := proxyGet(t, "/e2e-mirror", nil)
	if code != 200 || body != "primary" {
		t.Errorf("primary: %d %q", code, body)
	}

	time.Sleep(500 * time.Millisecond)
	if mirror.requestCount.Load() == 0 {
		t.Error("mirror received no requests")
	}
}

func TestE2E_Proxy_WebSocket(t *testing.T) {
	wsSrv := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		websocket.Handler(func(ws *websocket.Conn) {
			var msg string
			websocket.Message.Receive(ws, &msg)
			websocket.Message.Send(ws, "echo:"+msg)
		}).ServeHTTP(w, r)
	})

	destID := createDestination(t, "e2e-ws", wsSrv.host(), wsSrv.port())
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-ws", "match": map[string]any{"pathPrefix": "/e2e-ws"},
		"forward": map[string]any{"destinations": []map[string]any{{"destinationId": destID, "weight": 100}}},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	ws, err := websocket.Dial(fmt.Sprintf("ws://localhost:3000/e2e-ws"), "", "http://localhost")
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer ws.Close()

	websocket.Message.Send(ws, "hello")
	var reply string
	websocket.Message.Receive(ws, &reply)
	if reply != "echo:hello" {
		t.Errorf("expected echo:hello, got %q", reply)
	}
}

func TestE2E_Proxy_AccessLogToFile(t *testing.T) {
	logDir := t.TempDir()
	logFile := filepath.Join(logDir, "access.log")

	_, mw := apiPost(t, "/middlewares", map[string]any{
		"name": "e2e-accesslog", "type": "accessLog",
		"accessLog": map[string]any{
			"path": logFile, "json": true,
			"onRequest":  map[string]any{"fields": map[string]string{"event": "req", "method": "${request.method}", "path": "${request.path}"}},
			"onResponse": map[string]any{"fields": map[string]string{"event": "resp", "status": "${response.status}"}},
		},
	})
	defer apiDelete(t, "/middlewares/"+id(mw))

	_, route := apiPost(t, "/routes", map[string]any{
		"name": "e2e-alog", "match": map[string]any{"pathPrefix": "/e2e-alog"},
		"directResponse": map[string]any{"status": 200, "body": "ok"}, "middlewareIds": []string{id(mw)},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	proxyGet(t, "/e2e-alog", nil)
	time.Sleep(500 * time.Millisecond)

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected 2 log lines, got %d: %s", len(lines), string(data))
	}

	var reqEntry map[string]string
	json.Unmarshal([]byte(lines[0]), &reqEntry)
	if reqEntry["event"] != "req" || reqEntry["method"] != "GET" {
		t.Errorf("request log: %v", reqEntry)
	}

	var respEntry map[string]string
	json.Unmarshal([]byte(lines[1]), &respEntry)
	if respEntry["event"] != "resp" || respEntry["status"] != "200" {
		t.Errorf("response log: %v", respEntry)
	}
}
