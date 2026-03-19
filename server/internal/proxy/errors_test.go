package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"

	"github.com/achetronic/vrata/internal/model"
)

func TestClassifyError_ConnectionRefused(t *testing.T) {
	err := &net.OpError{Op: "dial", Err: &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}}
	got := classifyError(err)
	if got != model.ProxyErrConnectionRefused {
		t.Errorf("got %q, want connection_refused", got)
	}
}

func TestClassifyError_Timeout(t *testing.T) {
	got := classifyError(context.DeadlineExceeded)
	if got != model.ProxyErrTimeout {
		t.Errorf("got %q, want timeout", got)
	}
}

func TestClassifyError_Canceled(t *testing.T) {
	got := classifyError(context.Canceled)
	if got != model.ProxyErrTimeout {
		t.Errorf("got %q, want timeout", got)
	}
}

func TestClassifyError_DNSFailure(t *testing.T) {
	err := &net.OpError{Op: "dial", Err: &net.DNSError{Err: "no such host", Name: "bad.example.com"}}
	got := classifyError(err)
	if got != model.ProxyErrDNSFailure {
		t.Errorf("got %q, want dns_failure", got)
	}
}

func TestClassifyError_ConnectionReset(t *testing.T) {
	err := &net.OpError{Op: "read", Err: syscall.ECONNRESET}
	got := classifyError(err)
	if got != model.ProxyErrConnectionReset {
		t.Errorf("got %q, want connection_reset", got)
	}
}

func TestClassifyError_TLS(t *testing.T) {
	err := &tls.CertificateVerificationError{Err: errors.New("x509: certificate expired")}
	got := classifyError(err)
	if got != model.ProxyErrTLSHandshakeFailure {
		t.Errorf("got %q, want tls_handshake_failure", got)
	}
}

func TestClassifyError_Nil(t *testing.T) {
	got := classifyError(nil)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestClassifyError_FallbackMessage(t *testing.T) {
	err := errors.New("dial tcp: connection refused by host")
	got := classifyError(err)
	if got != model.ProxyErrConnectionRefused {
		t.Errorf("got %q, want connection_refused", got)
	}
}

func TestMatchesOnError_ExactMatch(t *testing.T) {
	rule := model.OnErrorRule{On: []model.ProxyErrorType{model.ProxyErrTimeout, model.ProxyErrCircuitOpen}}
	if !matchesOnError(rule, model.ProxyErrTimeout) {
		t.Error("should match timeout")
	}
	if !matchesOnError(rule, model.ProxyErrCircuitOpen) {
		t.Error("should match circuit_open")
	}
	if matchesOnError(rule, model.ProxyErrDNSFailure) {
		t.Error("should not match dns_failure")
	}
}

func TestMatchesOnError_Infrastructure(t *testing.T) {
	rule := model.OnErrorRule{On: []model.ProxyErrorType{model.ProxyErrInfrastructure}}
	infraTypes := []model.ProxyErrorType{
		model.ProxyErrConnectionRefused, model.ProxyErrConnectionReset,
		model.ProxyErrDNSFailure, model.ProxyErrTimeout,
		model.ProxyErrTLSHandshakeFailure, model.ProxyErrCircuitOpen,
		model.ProxyErrNoDestination, model.ProxyErrNoEndpoint,
	}
	for _, typ := range infraTypes {
		if !matchesOnError(rule, typ) {
			t.Errorf("infrastructure should match %s", typ)
		}
	}
}

func TestMatchesOnError_All(t *testing.T) {
	rule := model.OnErrorRule{On: []model.ProxyErrorType{model.ProxyErrAll}}
	for _, typ := range []model.ProxyErrorType{
		model.ProxyErrConnectionRefused, model.ProxyErrTimeout, model.ProxyErrNoEndpoint,
	} {
		if !matchesOnError(rule, typ) {
			t.Errorf("all should match %s", typ)
		}
	}
}

func TestFindOnErrorRule_Order(t *testing.T) {
	rules := []model.OnErrorRule{
		{On: []model.ProxyErrorType{model.ProxyErrTimeout}, DirectResponse: &model.RouteDirectResponse{Status: 504, Body: "timeout"}},
		{On: []model.ProxyErrorType{model.ProxyErrInfrastructure}, DirectResponse: &model.RouteDirectResponse{Status: 502, Body: "infra"}},
	}
	rule := findOnErrorRule(rules, model.ProxyErrTimeout)
	if rule == nil || rule.DirectResponse.Body != "timeout" {
		t.Error("should match first rule for timeout")
	}
	rule = findOnErrorRule(rules, model.ProxyErrConnectionRefused)
	if rule == nil || rule.DirectResponse.Body != "infra" {
		t.Error("should match second rule for connection_refused")
	}
}

func TestFindOnErrorRule_Empty(t *testing.T) {
	rule := findOnErrorRule(nil, model.ProxyErrTimeout)
	if rule != nil {
		t.Error("should return nil for empty rules")
	}
}

func TestWriteProxyError_JSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeProxyError(w, http.StatusBadGateway, "upstream unreachable")

	if w.Code != 502 {
		t.Errorf("expected 502, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"error"`) || !strings.Contains(body, "upstream unreachable") {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestInjectErrorHeaders(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/v1/orders", nil)
	pe := &ProxyError{
		Type:        model.ProxyErrConnectionRefused,
		Status:      502,
		Destination: "orders-svc",
		Endpoint:    "10.0.1.14:8080",
	}
	injectErrorHeaders(r, pe)

	if r.Header.Get("X-Vrata-Error") != "connection_refused" {
		t.Error("missing X-Vrata-Error")
	}
	if r.Header.Get("X-Vrata-Error-Status") != "502" {
		t.Error("missing X-Vrata-Error-Status")
	}
	if r.Header.Get("X-Vrata-Error-Destination") != "orders-svc" {
		t.Error("missing X-Vrata-Error-Destination")
	}
	if r.Header.Get("X-Vrata-Error-Endpoint") != "10.0.1.14:8080" {
		t.Error("missing X-Vrata-Error-Endpoint")
	}
	if r.Header.Get("X-Vrata-Original-Path") != "/api/v1/orders" {
		t.Error("missing X-Vrata-Original-Path")
	}
}

func TestStatusForErrorType(t *testing.T) {
	tests := []struct {
		typ  model.ProxyErrorType
		want int
	}{
		{model.ProxyErrCircuitOpen, 503},
		{model.ProxyErrTimeout, 504},
		{model.ProxyErrConnectionRefused, 502},
		{model.ProxyErrNoDestination, 502},
		{model.ProxyErrDNSFailure, 502},
	}
	for _, tt := range tests {
		got := statusForErrorType(tt.typ)
		if got != tt.want {
			t.Errorf("statusForErrorType(%s) = %d, want %d", tt.typ, got, tt.want)
		}
	}
}

func TestHandleProxyError_DefaultJSON(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	pe := &ProxyError{Type: model.ProxyErrConnectionRefused, Status: 502, Message: "connection refused"}

	handleProxyError(w, r, pe, nil, nil, nil)

	if w.Code != 502 {
		t.Errorf("expected 502, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "connection refused") {
		t.Error("expected error message in body")
	}
}

func TestHandleProxyError_DirectResponse(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	pe := &ProxyError{Type: model.ProxyErrCircuitOpen, Status: 503, Message: "circuit breaker open"}

	rules := []model.OnErrorRule{
		{On: []model.ProxyErrorType{model.ProxyErrCircuitOpen}, DirectResponse: &model.RouteDirectResponse{Status: 503, Body: `{"degraded":true}`}},
	}

	handleProxyError(w, r, pe, rules, nil, nil)

	if w.Code != 503 {
		t.Errorf("expected 503, got %d", w.Code)
	}
	if w.Body.String() != `{"degraded":true}` {
		t.Errorf("unexpected body: %s", w.Body.String())
	}
}

func TestHandleProxyError_Redirect(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	pe := &ProxyError{Type: model.ProxyErrConnectionRefused, Status: 502, Message: "connection refused"}

	rules := []model.OnErrorRule{
		{On: []model.ProxyErrorType{model.ProxyErrConnectionRefused}, Redirect: &model.RouteRedirect{URL: "https://status.example.com", Code: 302}},
	}

	handleProxyError(w, r, pe, rules, nil, nil)

	if w.Code != 302 {
		t.Errorf("expected 302, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "https://status.example.com" {
		t.Errorf("expected redirect to status page, got %q", loc)
	}
}

func TestHandleProxyError_ForwardInjectsHeaders(t *testing.T) {
	fallbackCalled := false
	var capturedHeaders http.Header
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackCalled = true
		capturedHeaders = r.Header.Clone()
		w.WriteHeader(200)
		w.Write([]byte("fallback-ok"))
	}))
	defer fallback.Close()

	host, port := splitHostPort(t, fallback.Listener.Addr().String())
	dest := model.Destination{ID: "fb", Name: "fallback", Host: host, Port: uint32(port)}
	pool, err := NewDestinationPool(dest, nil)
	if err != nil {
		t.Fatal(err)
	}
	pools := map[string]*DestinationPool{"fb": pool}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/orders", nil)
	pe := &ProxyError{Type: model.ProxyErrConnectionRefused, Status: 502, Destination: "orders-svc", Endpoint: "10.0.1.14:8080", Message: "connection refused"}

	rules := []model.OnErrorRule{
		{
			On: []model.ProxyErrorType{model.ProxyErrConnectionRefused},
			Forward: &model.ForwardAction{
				Destinations: []model.DestinationRef{{DestinationID: "fb", Weight: 100}},
			},
		},
	}

	handleProxyError(w, r, pe, rules, pools, nil)

	if !fallbackCalled {
		t.Fatal("fallback was not called")
	}
	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if capturedHeaders.Get("X-Vrata-Error") != "connection_refused" {
		t.Error("missing X-Vrata-Error header on fallback request")
	}
	if capturedHeaders.Get("X-Vrata-Error-Destination") != "orders-svc" {
		t.Error("missing X-Vrata-Error-Destination header")
	}
	if capturedHeaders.Get("X-Vrata-Original-Path") != "/api/v1/orders" {
		t.Error("missing X-Vrata-Original-Path header")
	}
}

func TestHandleProxyError_NoMatchFallsToDefault(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	pe := &ProxyError{Type: model.ProxyErrDNSFailure, Status: 502, Message: "dns failure"}

	rules := []model.OnErrorRule{
		{On: []model.ProxyErrorType{model.ProxyErrTimeout}, DirectResponse: &model.RouteDirectResponse{Status: 504, Body: "timeout only"}},
	}

	handleProxyError(w, r, pe, rules, nil, nil)

	if w.Code != 502 {
		t.Errorf("expected default 502, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "dns failure") {
		t.Error("expected default error message")
	}
}

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	return host, port
}
