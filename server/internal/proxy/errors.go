// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package proxy

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"syscall"

	"github.com/achetronic/vrata/internal/model"
)

// ProxyError holds the typed error context when a forward action fails.
// It is passed to the onError evaluation logic and, if a fallback forward
// is triggered, its fields are injected as X-Vrata-Error-* headers.
type ProxyError struct {
	Type        model.ProxyErrorType
	Status      int
	Destination string
	Endpoint    string
	Message     string
}

// proxyErrorBody is the JSON structure returned to the client when no
// onError rule matches and Vrata must respond with a default error.
type proxyErrorBody struct {
	Error string `json:"error"`
}

// writeProxyError writes a JSON error response with the given status and message.
// All proxy-generated error responses go through this function to ensure
// consistent Content-Type and body format.
func writeProxyError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(proxyErrorBody{Error: msg})
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

// matchesOnError returns true if the given error type matches any entry in
// the rule's On list, including wildcard expansion.
func matchesOnError(rule model.OnErrorRule, errType model.ProxyErrorType) bool {
	for _, on := range rule.On {
		if on == model.ProxyErrAll {
			return true
		}
		if on == model.ProxyErrInfrastructure {
			switch errType {
			case model.ProxyErrConnectionRefused, model.ProxyErrConnectionReset,
				model.ProxyErrDNSFailure, model.ProxyErrTimeout,
				model.ProxyErrTLSHandshakeFailure, model.ProxyErrCircuitOpen,
				model.ProxyErrNoDestination, model.ProxyErrNoEndpoint:
				return true
			}
		}
		if on == errType {
			return true
		}
	}
	return false
}

// findOnErrorRule evaluates the route's onError rules in order and returns
// the first rule that matches the error type. Returns nil if no rule matches.
func findOnErrorRule(rules []model.OnErrorRule, errType model.ProxyErrorType) *model.OnErrorRule {
	for i := range rules {
		if matchesOnError(rules[i], errType) {
			return &rules[i]
		}
	}
	return nil
}

// injectErrorHeaders adds X-Vrata-Error-* headers to the request so the
// fallback destination knows why the request was rerouted.
func injectErrorHeaders(r *http.Request, pe *ProxyError) {
	r.Header.Set("X-Vrata-Error", string(pe.Type))
	r.Header.Set("X-Vrata-Error-Status", fmt.Sprintf("%d", pe.Status))
	if pe.Destination != "" {
		r.Header.Set("X-Vrata-Error-Destination", pe.Destination)
	}
	if pe.Endpoint != "" {
		r.Header.Set("X-Vrata-Error-Endpoint", pe.Endpoint)
	}
	r.Header.Set("X-Vrata-Original-Path", r.URL.Path)
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
