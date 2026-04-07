// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package model

// ClientIPSource selects the strategy used to determine the real client IP.
type ClientIPSource string

const (
	// ClientIPSourceDirect uses the TCP peer address (r.RemoteAddr) as the
	// client IP. Ignores all forwarding headers. Safest choice when no
	// reverse proxy sits in front of the listener, or when the proxy sets
	// a custom header and source "header" is used instead.
	ClientIPSourceDirect ClientIPSource = "direct"

	// ClientIPSourceXFF walks the X-Forwarded-For chain from right to left,
	// skipping entries that belong to trusted CIDRs or skipping a fixed
	// number of trusted hops. The first untrusted entry is the client IP.
	ClientIPSourceXFF ClientIPSource = "xff"

	// ClientIPSourceHeader reads the client IP from a single named request
	// header (e.g. X-Real-IP, CF-Connecting-IP). The header must be
	// injected by a trusted load balancer.
	ClientIPSourceHeader ClientIPSource = "header"
)

// ClientIPConfig configures how the proxy determines the real client IP
// from an incoming request. Attached to Routes or RouteGroups as a
// middleware (type "clientIp"). The resolved IP is stored in the request
// context and consumed by CEL expressions (request.clientIp), access
// logging (${request.clientIp}), and any other middleware that needs it.
type ClientIPConfig struct {
	// Source selects the resolution strategy.
	// Default: "direct".
	Source ClientIPSource `json:"source" yaml:"source"`

	// Header is the request header name to read when Source is "header".
	// Required when Source is "header". Ignored otherwise.
	// Examples: "X-Real-IP", "CF-Connecting-IP", "True-Client-IP".
	Header string `json:"header,omitempty" yaml:"header,omitempty"`

	// TrustedCidrs lists CIDR ranges whose entries in X-Forwarded-For are
	// considered infrastructure (load balancers, CDN nodes) and skipped
	// when walking the chain. Only used when Source is "xff".
	// Mutually exclusive with NumTrustedHops.
	// Example: ["10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"]
	TrustedCidrs []string `json:"trustedCidrs,omitempty" yaml:"trustedCidrs,omitempty"`

	// NumTrustedHops is the number of rightmost X-Forwarded-For entries to
	// skip (they belong to trusted proxies). The entry just before the
	// skipped ones is the client IP. Only used when Source is "xff".
	// Mutually exclusive with TrustedCidrs. When both are zero/empty and
	// Source is "xff", the leftmost (first) entry is used — which is the
	// legacy behaviour and NOT safe against spoofing.
	NumTrustedHops int `json:"numTrustedHops,omitempty" yaml:"numTrustedHops,omitempty"`
}
