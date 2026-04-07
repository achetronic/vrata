// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package proxy

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/achetronic/vrata/internal/model"
	"github.com/achetronic/vrata/internal/proxy/celeval"
)

// clientIPResolver is a function that extracts the real client IP from a request.
type clientIPResolver func(r *http.Request) string

// BuildClientIPResolver compiles a ClientIPConfig into a fast resolver function.
// Returns nil when cfg is nil (no resolution configured).
func BuildClientIPResolver(cfg *model.ClientIPConfig) clientIPResolver {
	if cfg == nil {
		return nil
	}

	switch cfg.Source {
	case model.ClientIPSourceHeader:
		header := cfg.Header
		return func(r *http.Request) string {
			if val := r.Header.Get(header); val != "" {
				return strings.TrimSpace(val)
			}
			return directIP(r)
		}

	case model.ClientIPSourceXFF:
		if len(cfg.TrustedCidrs) > 0 {
			nets := parseCIDRs(cfg.TrustedCidrs)
			return func(r *http.Request) string {
				return resolveXFFByCIDR(r, nets)
			}
		}
		if cfg.NumTrustedHops > 0 {
			hops := cfg.NumTrustedHops
			return func(r *http.Request) string {
				return resolveXFFByHops(r, hops)
			}
		}
		return resolveXFFLeftmost

	case model.ClientIPSourceDirect:
		return directIP

	default:
		return nil
	}
}

// injectClientIP wraps an http.Handler to resolve the client IP and store it
// in the request context before the inner handler runs. When resolver is nil,
// the request passes through unchanged.
func injectClientIP(resolver clientIPResolver, next http.Handler) http.Handler {
	if resolver == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := resolver(r)
		ctx := context.WithValue(r.Context(), celeval.ClientIPCtxKey{}, ip)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// directIP returns the peer IP address from the TCP connection.
func directIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// resolveXFFByCIDR walks X-Forwarded-For from right to left, skipping
// entries whose IP falls within any trusted CIDR. The first entry NOT
// in a trusted range is the client IP. Falls back to RemoteAddr if all
// entries are trusted or the header is absent.
func resolveXFFByCIDR(r *http.Request, trusted []*net.IPNet) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return directIP(r)
	}

	parts := strings.Split(xff, ",")

	for i := len(parts) - 1; i >= 0; i-- {
		ip := strings.TrimSpace(parts[i])
		if ip == "" {
			continue
		}
		parsed := net.ParseIP(ip)
		if parsed == nil {
			continue
		}
		isTrusted := false
		for _, n := range trusted {
			if n.Contains(parsed) {
				isTrusted = true
				break
			}
		}
		if !isTrusted {
			return ip
		}
	}

	return directIP(r)
}

// resolveXFFByHops skips the rightmost N entries in X-Forwarded-For and
// returns the entry just before them. With numTrustedHops=1 and
// XFF="client, proxy1", it returns "client". Falls back to RemoteAddr
// if the chain is shorter than numTrustedHops.
func resolveXFFByHops(r *http.Request, numTrustedHops int) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return directIP(r)
	}

	parts := strings.Split(xff, ",")
	idx := len(parts) - numTrustedHops - 1
	if idx < 0 {
		return directIP(r)
	}

	ip := strings.TrimSpace(parts[idx])
	if ip == "" {
		return directIP(r)
	}
	return ip
}

// resolveXFFLeftmost returns the leftmost (first) entry in X-Forwarded-For.
// This is the legacy unsafe behaviour — the client can spoof it freely.
func resolveXFFLeftmost(r *http.Request) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return directIP(r)
	}
	if idx := strings.Index(xff, ","); idx != -1 {
		return strings.TrimSpace(xff[:idx])
	}
	return strings.TrimSpace(xff)
}

// parseCIDRs parses CIDR strings into net.IPNet for fast lookup.
func parseCIDRs(cidrs []string) []*net.IPNet {
	var nets []*net.IPNet
	for _, cidr := range cidrs {
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			slog.Warn("clientIp: invalid trustedCidr, skipping",
				slog.String("cidr", cidr),
				slog.String("error", err.Error()),
			)
			continue
		}
		nets = append(nets, n)
	}
	return nets
}
