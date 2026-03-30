// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package proxy

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http/httptest"
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

func TestClassifyError_FallbackConnectionReset(t *testing.T) {
	err := errors.New("read tcp: connection reset by peer")
	got := classifyError(err)
	if got != model.ProxyErrConnectionReset {
		t.Errorf("got %q, want connection_reset", got)
	}
}

func TestClassifyError_FallbackDNS(t *testing.T) {
	err := errors.New("dial tcp: lookup bad.host: no such host")
	got := classifyError(err)
	if got != model.ProxyErrDNSFailure {
		t.Errorf("got %q, want dns_failure", got)
	}
}

func TestClassifyError_FallbackIOTimeout(t *testing.T) {
	err := errors.New("read tcp 10.0.0.1:8080: i/o timeout")
	got := classifyError(err)
	if got != model.ProxyErrTimeout {
		t.Errorf("got %q, want timeout", got)
	}
}

func TestClassifyError_FallbackTLS(t *testing.T) {
	err := errors.New("tls: handshake failure")
	got := classifyError(err)
	if got != model.ProxyErrTLSHandshakeFailure {
		t.Errorf("got %q, want tls_handshake_failure", got)
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

func TestWriteProxyError_Minimal(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	ctx := withProxyErrorDetail(r.Context(), model.ProxyErrorDetailMinimal)
	r = r.WithContext(ctx)

	pe := &ProxyError{Type: model.ProxyErrConnectionRefused, Status: 502, Message: "connection refused", Destination: "svc-1"}
	writeProxyError(w, r, pe)

	if w.Code != 502 {
		t.Errorf("expected 502, got %d", w.Code)
	}
	var body proxyErrorMinimalBody
	json.NewDecoder(w.Body).Decode(&body)
	if body.Error != "connection_refused" {
		t.Errorf("expected error=connection_refused, got %q", body.Error)
	}
	if body.Status != 502 {
		t.Errorf("expected status=502, got %d", body.Status)
	}
}

func TestWriteProxyError_Standard(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)

	pe := &ProxyError{Type: model.ProxyErrTimeout, Status: 504, Message: "request timeout", Destination: "svc-1"}
	writeProxyError(w, r, pe)

	if w.Code != 504 {
		t.Errorf("expected 504, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}
	var body proxyErrorStandardBody
	json.NewDecoder(w.Body).Decode(&body)
	if body.Error != "timeout" {
		t.Errorf("expected error=timeout, got %q", body.Error)
	}
	if body.Message != "request timeout" {
		t.Errorf("expected message, got %q", body.Message)
	}
}

func TestWriteProxyError_Full(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	ctx := withProxyErrorDetail(r.Context(), model.ProxyErrorDetailFull)
	r = r.WithContext(ctx)

	pe := &ProxyError{Type: model.ProxyErrCircuitOpen, Status: 503, Message: "circuit breaker open", Destination: "orders-svc", Endpoint: "10.0.1.14:8080"}
	writeProxyError(w, r, pe)

	if w.Code != 503 {
		t.Errorf("expected 503, got %d", w.Code)
	}
	var body proxyErrorFullBody
	json.NewDecoder(w.Body).Decode(&body)
	if body.Error != "circuit_open" {
		t.Errorf("expected error=circuit_open, got %q", body.Error)
	}
	if body.Destination != "orders-svc" {
		t.Errorf("expected destination=orders-svc, got %q", body.Destination)
	}
	if body.Endpoint != "10.0.1.14:8080" {
		t.Errorf("expected endpoint=10.0.1.14:8080, got %q", body.Endpoint)
	}
	if body.Timestamp == "" {
		t.Error("expected timestamp in full detail")
	}
}

func TestWriteProxyError_DefaultIsStandard(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)

	pe := &ProxyError{Type: model.ProxyErrDNSFailure, Status: 502, Message: "dns failure"}
	writeProxyError(w, r, pe)

	var body map[string]any
	json.NewDecoder(w.Body).Decode(&body)
	if _, ok := body["message"]; !ok {
		t.Error("default detail should include message (standard)")
	}
	if _, ok := body["destination"]; ok {
		t.Error("default detail should not include destination")
	}
}
