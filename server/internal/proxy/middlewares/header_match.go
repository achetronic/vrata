// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package middlewares

import (
	"net/http"
	"strings"
)

// headerMatchesAny checks if a header name matches any pattern in the list.
// Patterns support:
//   - exact match: "authorization"
//   - wildcard suffix: "x-auth-request-*" matches "x-auth-request-user", etc.
func headerMatchesAny(name string, patterns []string) bool {
	lower := strings.ToLower(name)
	for _, p := range patterns {
		pl := strings.ToLower(p)
		if pl == lower {
			return true
		}
		if strings.HasSuffix(pl, "*") {
			prefix := pl[:len(pl)-1]
			if strings.HasPrefix(lower, prefix) {
				return true
			}
		}
	}
	return false
}

// copyMatchingHeaders copies headers from src that match any pattern to dst.
func copyMatchingHeaders(dst http.Header, src http.Header, patterns []string) {
	for name, values := range src {
		if headerMatchesAny(name, patterns) {
			for _, v := range values {
				dst.Add(name, v)
			}
		}
	}
}
