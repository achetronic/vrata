// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package proxy

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/achetronic/vrata/internal/model"
)

// ProxyError holds the typed error context when a forward action fails.
type ProxyError struct {
	Type        model.ProxyErrorType
	Status      int
	Destination string
	Endpoint    string
	Message     string
}

type proxyErrorDetailKey struct{}

// withProxyErrorDetail stores the detail level on the request context.
func withProxyErrorDetail(ctx context.Context, detail model.ProxyErrorDetail) context.Context {
	return context.WithValue(ctx, proxyErrorDetailKey{}, detail)
}

// proxyErrorDetailFromCtx reads the detail level from the request context.
func proxyErrorDetailFromCtx(ctx context.Context) model.ProxyErrorDetail {
	if v, ok := ctx.Value(proxyErrorDetailKey{}).(model.ProxyErrorDetail); ok {
		return v
	}
	return model.ProxyErrorDetailStandard
}

// proxyErrorMinimalBody is the JSON structure for "minimal" detail level.
type proxyErrorMinimalBody struct {
	Error  string `json:"error"`
	Status int    `json:"status"`
}

// proxyErrorStandardBody is the JSON structure for "standard" detail level.
type proxyErrorStandardBody struct {
	Error   string `json:"error"`
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// proxyErrorFullBody is the JSON structure for "full" detail level.
type proxyErrorFullBody struct {
	Error       string `json:"error"`
	Status      int    `json:"status"`
	Message     string `json:"message"`
	Destination string `json:"destination,omitempty"`
	Endpoint    string `json:"endpoint,omitempty"`
	Timestamp   string `json:"timestamp"`
}

// writeProxyError writes a structured JSON error response. The detail level
// is read from the request context (set by the listener middleware). When
// no ProxyError is available (e.g. no matching route), callers pass a
// simple status + message and the function builds a minimal ProxyError
// internally.
func writeProxyError(w http.ResponseWriter, r *http.Request, pe *ProxyError) {
	detail := proxyErrorDetailFromCtx(r.Context())

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(pe.Status)

	switch detail {
	case model.ProxyErrorDetailMinimal:
		json.NewEncoder(w).Encode(proxyErrorMinimalBody{
			Error:  string(pe.Type),
			Status: pe.Status,
		})
	case model.ProxyErrorDetailFull:
		json.NewEncoder(w).Encode(proxyErrorFullBody{
			Error:       string(pe.Type),
			Status:      pe.Status,
			Message:     pe.Message,
			Destination: pe.Destination,
			Endpoint:    pe.Endpoint,
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
		})
	default:
		json.NewEncoder(w).Encode(proxyErrorStandardBody{
			Error:   string(pe.Type),
			Status:  pe.Status,
			Message: pe.Message,
		})
	}
}

// classifyError inspects a transport-level error and returns the corresponding
// ProxyErrorType. It unwraps nested errors to find the root cause.
func classifyError(err error) model.ProxyErrorType {
	if err == nil {
		return ""
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return model.ProxyErrTimeout
	}
	if errors.Is(err, context.Canceled) {
		return model.ProxyErrTimeout
	}

	var netErr *net.OpError
	if errors.As(err, &netErr) {
		if netErr.Op == "dial" {
			if errors.Is(netErr.Err, syscall.ECONNREFUSED) {
				return model.ProxyErrConnectionRefused
			}
			var dnsErr *net.DNSError
			if errors.As(netErr.Err, &dnsErr) {
				return model.ProxyErrDNSFailure
			}
			return model.ProxyErrConnectionRefused
		}
		if errors.Is(netErr.Err, syscall.ECONNRESET) {
			return model.ProxyErrConnectionReset
		}
	}

	var tlsErr *tls.CertificateVerificationError
	if errors.As(err, &tlsErr) {
		return model.ProxyErrTLSHandshakeFailure
	}
	var tlsRecordErr *tls.RecordHeaderError
	if errors.As(err, &tlsRecordErr) {
		return model.ProxyErrTLSHandshakeFailure
	}

	msg := err.Error()
	if strings.Contains(msg, "tls:") || strings.Contains(msg, "TLS") || strings.Contains(msg, "certificate") {
		return model.ProxyErrTLSHandshakeFailure
	}
	if strings.Contains(msg, "connection refused") {
		return model.ProxyErrConnectionRefused
	}
	if strings.Contains(msg, "connection reset") {
		return model.ProxyErrConnectionReset
	}
	if strings.Contains(msg, "no such host") {
		return model.ProxyErrDNSFailure
	}
	if strings.Contains(msg, "i/o timeout") || strings.Contains(msg, "deadline exceeded") {
		return model.ProxyErrTimeout
	}

	return model.ProxyErrConnectionRefused
}

// statusForErrorType returns the default HTTP status code for a proxy error.
func statusForErrorType(t model.ProxyErrorType) int {
	switch t {
	case model.ProxyErrCircuitOpen:
		return http.StatusServiceUnavailable
	case model.ProxyErrTimeout:
		return http.StatusGatewayTimeout
	default:
		return http.StatusBadGateway
	}
}
