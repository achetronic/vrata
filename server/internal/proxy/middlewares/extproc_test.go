// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package middlewares

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/achetronic/vrata/internal/model"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// fakeProcessor is a configurable HTTP handler that mimics an external
// processor in HTTP mode. It receives a JSON ProcessingRequest and returns
// a JSON ProcessingResponse based on the configured behavior.
type fakeProcessor struct {
	t       *testing.T
	handler func(req httpRequest) httpResponse
}

func (fp *fakeProcessor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		fp.t.Fatalf("fakeProcessor: reading body: %v", err)
	}

	var req httpRequest
	if err := json.Unmarshal(body, &req); err != nil {
		fp.t.Fatalf("fakeProcessor: decoding request: %v", err)
	}

	resp := fp.handler(req)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func newExtProcCfg(processorURL string) *model.ExtProcConfig {
	return &model.ExtProcConfig{
		DestinationID: "proc-1",
		Mode:          "http",
		PhaseTimeout:   "2s",
	}
}

func newServices(processorURL string) map[string]Service {
	return map[string]Service{
		"proc-1": {BaseURL: processorURL},
	}
}

func upstream() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upstream", "true")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("upstream-body"))
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Request headers phase
// ─────────────────────────────────────────────────────────────────────────────

func TestExtProcRequestHeadersContinue(t *testing.T) {
	proc := httptest.NewServer(&fakeProcessor{t: t, handler: func(req httpRequest) httpResponse {
		if req.Phase == "requestHeaders" {
			return httpResponse{
				Action:     "requestHeaders",
				Status:     "continue",
				SetHeaders: []headerPair{{Key: "x-injected", Value: "hello"}},
			}
		}
		return httpResponse{Action: req.Phase, Status: "continue"}
	}})
	defer proc.Close()

	cfg := newExtProcCfg(proc.URL)
	mw := ExtProcMiddleware(cfg, newServices(proc.URL))

	var capturedHeader string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("X-Injected")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if capturedHeader != "hello" {
		t.Errorf("expected injected header 'hello', got %q", capturedHeader)
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestExtProcRequestHeadersRemove(t *testing.T) {
	proc := httptest.NewServer(&fakeProcessor{t: t, handler: func(req httpRequest) httpResponse {
		return httpResponse{
			Action:        "requestHeaders",
			Status:        "continue",
			RemoveHeaders: []string{"x-secret"},
		}
	}})
	defer proc.Close()

	cfg := newExtProcCfg(proc.URL)
	mw := ExtProcMiddleware(cfg, newServices(proc.URL))

	var hasSecret bool
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hasSecret = r.Header.Get("X-Secret") != ""
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Secret", "sensitive")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if hasSecret {
		t.Error("expected X-Secret to be removed")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Reject
// ─────────────────────────────────────────────────────────────────────────────

func TestExtProcReject(t *testing.T) {
	proc := httptest.NewServer(&fakeProcessor{t: t, handler: func(req httpRequest) httpResponse {
		return httpResponse{
			Action:       "reject",
			RejectStatus: 403,
			RejectBody:   []byte("forbidden"),
		}
	}})
	defer proc.Close()

	cfg := newExtProcCfg(proc.URL)
	mw := ExtProcMiddleware(cfg, newServices(proc.URL))
	handler := mw(upstream())

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Errorf("expected 403, got %d", w.Code)
	}
	if w.Body.String() != "forbidden" {
		t.Errorf("expected body 'forbidden', got %q", w.Body.String())
	}
}

func TestExtProcDisableReject(t *testing.T) {
	proc := httptest.NewServer(&fakeProcessor{t: t, handler: func(req httpRequest) httpResponse {
		return httpResponse{
			Action:       "reject",
			RejectStatus: 403,
			RejectBody:   []byte("forbidden"),
		}
	}})
	defer proc.Close()

	cfg := newExtProcCfg(proc.URL)
	cfg.DisableReject = true
	mw := ExtProcMiddleware(cfg, newServices(proc.URL))
	handler := mw(upstream())

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (reject disabled), got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AllowOnError
// ─────────────────────────────────────────────────────────────────────────────

func TestExtProcAllowOnError(t *testing.T) {
	cfg := &model.ExtProcConfig{
		DestinationID: "proc-1",
		Mode:          "http",
		PhaseTimeout:   "50ms",
		AllowOnError:  true,
	}
	services := map[string]Service{
		"proc-1": {BaseURL: "http://127.0.0.1:1"},
	}

	mw := ExtProcMiddleware(cfg, services)
	handler := mw(upstream())

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (allow on error), got %d", w.Code)
	}
}

func TestExtProcFailClosed(t *testing.T) {
	cfg := &model.ExtProcConfig{
		DestinationID: "proc-1",
		Mode:          "http",
		PhaseTimeout:   "50ms",
		AllowOnError:  false,
		StatusOnError: 503,
	}
	services := map[string]Service{
		"proc-1": {BaseURL: "http://127.0.0.1:1"},
	}

	mw := ExtProcMiddleware(cfg, services)
	handler := mw(upstream())

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 503 {
		t.Errorf("expected 503 (fail closed), got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ForwardRules
// ─────────────────────────────────────────────────────────────────────────────

func TestExtProcForwardRulesDeny(t *testing.T) {
	var receivedHeaders []headerPair
	proc := httptest.NewServer(&fakeProcessor{t: t, handler: func(req httpRequest) httpResponse {
		receivedHeaders = req.Headers
		return httpResponse{Action: "requestHeaders", Status: "continue"}
	}})
	defer proc.Close()

	cfg := newExtProcCfg(proc.URL)
	cfg.ForwardRules = &model.ForwardRules{
		DenyHeaders: []string{"authorization"},
	}
	mw := ExtProcMiddleware(cfg, newServices(proc.URL))
	handler := mw(upstream())

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("X-Public", "visible")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	for _, h := range receivedHeaders {
		if h.Key == "authorization" {
			t.Error("authorization header should not be forwarded")
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// MutationRules
// ─────────────────────────────────────────────────────────────────────────────

func TestExtProcMutationRulesDeny(t *testing.T) {
	proc := httptest.NewServer(&fakeProcessor{t: t, handler: func(req httpRequest) httpResponse {
		return httpResponse{
			Action: "requestHeaders",
			Status: "continue",
			SetHeaders: []headerPair{
				{Key: "x-allowed", Value: "yes"},
				{Key: "host", Value: "evil.com"},
			},
		}
	}})
	defer proc.Close()

	cfg := newExtProcCfg(proc.URL)
	cfg.AllowedMutations = &model.MutationRules{
		DenyHeaders: []string{"host"},
	}
	mw := ExtProcMiddleware(cfg, newServices(proc.URL))

	var capturedHost, capturedAllowed string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHost = r.Host
		capturedAllowed = r.Header.Get("X-Allowed")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Host = "original.com"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if capturedAllowed != "yes" {
		t.Errorf("expected x-allowed=yes, got %q", capturedAllowed)
	}
	if capturedHost != "original.com" {
		t.Errorf("expected host unchanged (original.com), got %q", capturedHost)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Request body phase
// ─────────────────────────────────────────────────────────────────────────────

func TestExtProcRequestBodyReplace(t *testing.T) {
	proc := httptest.NewServer(&fakeProcessor{t: t, handler: func(req httpRequest) httpResponse {
		if req.Phase == "requestHeaders" {
			return httpResponse{Action: "requestHeaders", Status: "continue"}
		}
		return httpResponse{
			Action:      "requestBody",
			Status:      "continue",
			ReplaceBody: []byte("replaced-body"),
		}
	}})
	defer proc.Close()

	cfg := newExtProcCfg(proc.URL)
	cfg.Phases = &model.ExtProcPhases{
		RequestBody: model.BodyModeBuffered,
	}
	mw := ExtProcMiddleware(cfg, newServices(proc.URL))

	var capturedBody string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		capturedBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/test", bytes.NewReader([]byte("original-body")))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if capturedBody != "replaced-body" {
		t.Errorf("expected 'replaced-body', got %q", capturedBody)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Response headers phase
// ─────────────────────────────────────────────────────────────────────────────

func TestExtProcResponseHeadersMutate(t *testing.T) {
	proc := httptest.NewServer(&fakeProcessor{t: t, handler: func(req httpRequest) httpResponse {
		if req.Phase == "requestHeaders" {
			return httpResponse{Action: "requestHeaders", Status: "continue"}
		}
		return httpResponse{
			Action:     "responseHeaders",
			Status:     "continue",
			SetHeaders: []headerPair{{Key: "x-processed", Value: "true"}},
		}
	}})
	defer proc.Close()

	cfg := newExtProcCfg(proc.URL)
	mw := ExtProcMiddleware(cfg, newServices(proc.URL))
	handler := mw(upstream())

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("X-Processed") != "true" {
		t.Errorf("expected x-processed=true, got %q", w.Header().Get("X-Processed"))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Response body phase
// ─────────────────────────────────────────────────────────────────────────────

func TestExtProcResponseBodyReplace(t *testing.T) {
	proc := httptest.NewServer(&fakeProcessor{t: t, handler: func(req httpRequest) httpResponse {
		switch req.Phase {
		case "requestHeaders":
			return httpResponse{Action: "requestHeaders", Status: "continue"}
		case "responseHeaders":
			return httpResponse{Action: "responseHeaders", Status: "continue"}
		case "responseBody":
			return httpResponse{
				Action:      "responseBody",
				Status:      "continue",
				ReplaceBody: []byte("modified-response"),
			}
		}
		return httpResponse{Action: req.Phase, Status: "continue"}
	}})
	defer proc.Close()

	cfg := newExtProcCfg(proc.URL)
	cfg.Phases = &model.ExtProcPhases{
		ResponseBody: model.BodyModeBuffered,
	}
	mw := ExtProcMiddleware(cfg, newServices(proc.URL))
	handler := mw(upstream())

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Body.String() != "modified-response" {
		t.Errorf("expected 'modified-response', got %q", w.Body.String())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Phase skipping
// ─────────────────────────────────────────────────────────────────────────────

func TestExtProcSkipPhases(t *testing.T) {
	var phases []string
	proc := httptest.NewServer(&fakeProcessor{t: t, handler: func(req httpRequest) httpResponse {
		phases = append(phases, req.Phase)
		return httpResponse{Action: req.Phase, Status: "continue"}
	}})
	defer proc.Close()

	cfg := newExtProcCfg(proc.URL)
	cfg.Phases = &model.ExtProcPhases{
		RequestHeaders:  model.PhaseModeSkip,
		ResponseHeaders: model.PhaseModeSkip,
	}
	mw := ExtProcMiddleware(cfg, newServices(proc.URL))
	handler := mw(upstream())

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if len(phases) != 0 {
		t.Errorf("expected no phases sent, got %v", phases)
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ObserveOnly
// ─────────────────────────────────────────────────────────────────────────────

func TestExtProcObserveOnly(t *testing.T) {
	called := make(chan string, 10)
	proc := httptest.NewServer(&fakeProcessor{t: t, handler: func(req httpRequest) httpResponse {
		called <- req.Phase
		return httpResponse{
			Action:       "reject",
			RejectStatus: 403,
		}
	}})
	defer proc.Close()

	cfg := newExtProcCfg(proc.URL)
	cfg.ObserveMode = &model.ObserveModeConfig{Enabled: true}
	mw := ExtProcMiddleware(cfg, newServices(proc.URL))
	handler := mw(upstream())

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("observe-only: expected 200, got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Passthrough (nil/empty config)
// ─────────────────────────────────────────────────────────────────────────────

func TestExtProcNilConfig(t *testing.T) {
	mw := ExtProcMiddleware(nil, nil)
	handler := mw(upstream())

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected passthrough 200, got %d", w.Code)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Duplicate headers preserved
// ─────────────────────────────────────────────────────────────────────────────

func TestExtProcDuplicateHeadersPreserved(t *testing.T) {
	var receivedHeaders []headerPair
	var captured bool
	proc := httptest.NewServer(&fakeProcessor{t: t, handler: func(req httpRequest) httpResponse {
		if req.Phase == "requestHeaders" && !captured {
			receivedHeaders = req.Headers
			captured = true
		}
		return httpResponse{Action: req.Phase, Status: "continue"}
	}})
	defer proc.Close()

	cfg := newExtProcCfg(proc.URL)
	mw := ExtProcMiddleware(cfg, newServices(proc.URL))
	handler := mw(upstream())

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Add("X-Multi", "value1")
	req.Header.Add("X-Multi", "value2")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	count := 0
	for _, h := range receivedHeaders {
		if h.Key == "x-multi" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 x-multi headers, got %d", count)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CONTINUE_AND_REPLACE from headers phase
// ─────────────────────────────────────────────────────────────────────────────

func TestExtProcContinueAndReplace(t *testing.T) {
	proc := httptest.NewServer(&fakeProcessor{t: t, handler: func(req httpRequest) httpResponse {
		return httpResponse{
			Action:      "requestHeaders",
			Status:      "continueAndReplace",
			ReplaceBody: []byte("injected-body"),
		}
	}})
	defer proc.Close()

	cfg := newExtProcCfg(proc.URL)
	mw := ExtProcMiddleware(cfg, newServices(proc.URL))

	var capturedBody string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		capturedBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if capturedBody != "injected-body" {
		t.Errorf("expected 'injected-body', got %q", capturedBody)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// BUFFERED_PARTIAL body mode
// ─────────────────────────────────────────────────────────────────────────────

func TestExtProcBufferedPartialRequestBody(t *testing.T) {
	var receivedBodyLen int
	proc := httptest.NewServer(&fakeProcessor{t: t, handler: func(req httpRequest) httpResponse {
		if req.Phase == "requestBody" {
			receivedBodyLen = len(req.Body)
		}
		return httpResponse{Action: req.Phase, Status: "continue"}
	}})
	defer proc.Close()

	cfg := newExtProcCfg(proc.URL)
	cfg.Phases = &model.ExtProcPhases{
		RequestBody:  model.BodyModeBufferedPartial,
		MaxBodyBytes: 10,
	}
	mw := ExtProcMiddleware(cfg, newServices(proc.URL))
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	largeBody := bytes.Repeat([]byte("x"), 100)
	req := httptest.NewRequest("POST", "/test", bytes.NewReader(largeBody))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if receivedBodyLen != 10 {
		t.Errorf("expected processor to receive 10 bytes (partial), got %d", receivedBodyLen)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// STREAMED body mode
// ─────────────────────────────────────────────────────────────────────────────

func TestExtProcStreamedRequestBody(t *testing.T) {
	var chunkCount int
	var totalBytes int
	proc := httptest.NewServer(&fakeProcessor{t: t, handler: func(req httpRequest) httpResponse {
		if req.Phase == "requestBody" {
			chunkCount++
			totalBytes += len(req.Body)
		}
		return httpResponse{Action: req.Phase, Status: "continue"}
	}})
	defer proc.Close()

	cfg := newExtProcCfg(proc.URL)
	cfg.Phases = &model.ExtProcPhases{
		RequestBody: model.BodyModeStreamed,
	}
	mw := ExtProcMiddleware(cfg, newServices(proc.URL))
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	largeBody := bytes.Repeat([]byte("a"), 100000)
	req := httptest.NewRequest("POST", "/test", bytes.NewReader(largeBody))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if chunkCount < 2 {
		t.Errorf("expected multiple chunks for streamed mode, got %d", chunkCount)
	}
	if totalBytes != 100000 {
		t.Errorf("expected total 100000 bytes, got %d", totalBytes)
	}
}

func TestExtProcStreamedResponseBody(t *testing.T) {
	var chunkCount int
	proc := httptest.NewServer(&fakeProcessor{t: t, handler: func(req httpRequest) httpResponse {
		if req.Phase == "responseBody" {
			chunkCount++
		}
		return httpResponse{Action: req.Phase, Status: "continue"}
	}})
	defer proc.Close()

	cfg := newExtProcCfg(proc.URL)
	cfg.Phases = &model.ExtProcPhases{
		ResponseBody: model.BodyModeStreamed,
	}
	mw := ExtProcMiddleware(cfg, newServices(proc.URL))

	largeBody := bytes.Repeat([]byte("b"), 100000)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(largeBody)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if chunkCount < 2 {
		t.Errorf("expected multiple response body chunks, got %d", chunkCount)
	}
}
