// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/achetronic/vrata/internal/api/respond"
	"github.com/achetronic/vrata/internal/config"
)

// Auth returns a middleware that validates the Authorization header against
// the configured API keys. When keys is nil or empty, the middleware is a
// no-op (dev mode). Clients must send "Authorization: Bearer <key>".
func Auth(keys []config.APIKeyEntry, logger *slog.Logger) func(http.Handler) http.Handler {
	lookup := make(map[string]string, len(keys))
	for _, k := range keys {
		lookup[k.Key] = k.Name
	}

	return func(next http.Handler) http.Handler {
		if len(lookup) == 0 {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth == "" {
				respond.Error(w, http.StatusUnauthorized, "missing authorization header", logger)
				return
			}

			token, ok := strings.CutPrefix(auth, "Bearer ")
			if !ok {
				respond.Error(w, http.StatusUnauthorized, "authorization header must use Bearer scheme", logger)
				return
			}

			entry, valid := lookup[token]
			if !valid {
				respond.Error(w, http.StatusUnauthorized, "invalid API key", logger)
				return
			}

			logger.Debug("authenticated",
				slog.String("keyName", entry),
			)

			next.ServeHTTP(w, r)
		})
	}
}
