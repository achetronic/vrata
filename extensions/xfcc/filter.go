// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package xfcc implements an Envoy Go HTTP filter that injects the
// X-Forwarded-Client-Cert (XFCC) header from the verified TLS client
// certificate, stripping any incoming XFCC to prevent spoofing.
//
// When a client presents a certificate that Envoy has verified (mTLS),
// this filter:
//  1. Strips any incoming x-forwarded-client-cert header (spoof protection).
//  2. Builds a new XFCC header from the verified cert metadata.
//  3. Injects it into the request before forwarding to the upstream.
//
// The upstream (or inlineauthz filter) can then read the XFCC header to
// make authorization decisions based on the client's identity without
// having direct access to the TLS layer.
//
// No configuration required. The filter reads cert metadata from Envoy's
// dynamic metadata (populated by the TLS inspector).
package xfcc

import (
	"fmt"
	"strings"

	"github.com/envoyproxy/envoy/contrib/golang/common/go/api"
)

// filter is the per-request filter instance.
type filter struct {
	api.PassThroughHttpFilter
	callbacks api.FilterCallbackHandler
}

// DecodeHeaders strips incoming XFCC and injects a new one from the verified
// client certificate metadata.
func (f *filter) DecodeHeaders(header api.RequestHeaderMap, _ bool) api.StatusType {
	// Step 1: Strip incoming XFCC to prevent spoofing.
	header.Del("x-forwarded-client-cert")

	// Step 2: Build XFCC from Envoy dynamic metadata.
	// Envoy populates envoy.filters.network.client_ssl_cipher with cert info
	// when mTLS is configured on the listener.
	//
	// For the initial implementation we read from the connection info that
	// Envoy exposes. Full cert metadata (SANs, subject) requires the TLS
	// inspector filter to be configured on the listener, which the xDS
	// translator will handle.
	//
	// This is a best-effort injection: if no cert info is available
	// (plain HTTP or mTLS not configured), we simply skip injection.
	certInfo := f.callbacks.StreamInfo().DynamicMetadata().Get("envoy.filters.network.client_ssl_cipher")
	if certInfo == nil {
		return api.Continue
	}

	xfcc := buildXFCC(certInfo)
	if xfcc != "" {
		header.Set("x-forwarded-client-cert", xfcc)
	}

	return api.Continue
}

// buildXFCC constructs an XFCC header value from Envoy cert metadata.
// Format: "By=<server-uri>;Hash=<cert-hash>;Subject=<subject>;URI=<san-uri>"
func buildXFCC(certInfo any) string {
	m, ok := certInfo.(map[string]any)
	if !ok {
		return ""
	}

	parts := []string{}

	if uri, ok := m["uri"].(string); ok && uri != "" {
		parts = append(parts, fmt.Sprintf("URI=%s", uri))
	}
	if subject, ok := m["subject"].(string); ok && subject != "" {
		parts = append(parts, fmt.Sprintf("Subject=%q", subject))
	}
	if hash, ok := m["sha256"].(string); ok && hash != "" {
		parts = append(parts, fmt.Sprintf("Hash=%s", hash))
	}

	return strings.Join(parts, ";")
}
