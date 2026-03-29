// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package main is the entrypoint for the xfcc Envoy Go filter plugin.
// Compiled as a shared object (.so) and loaded by Envoy at startup.
//
// Build:
//
//	go build -buildmode=plugin -o xfcc.so .
//
// No configuration required. Reads verified TLS client cert metadata
// from Envoy dynamic metadata and injects the X-Forwarded-Client-Cert
// header, stripping any incoming XFCC to prevent spoofing.
package main

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

// main is required for plugin build mode but does nothing.
func main() {}

func newFilter(callbacks api.FilterCallbackHandler) api.HttpFilter {
	return &filter{callbacks: callbacks}
}

func init() {
	api.RegisterHttpFilterFactoryAndConfigParser(
		"vrata.xfcc",
		func(callbacks api.FilterCallbackHandler) api.HttpFilter {
			return newFilter(callbacks)
		},
		&api.EmptyConfig{},
	)
}
