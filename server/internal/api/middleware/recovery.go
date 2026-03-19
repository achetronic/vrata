// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/achetronic/vrata/internal/api/respond"
)

// Recovery returns a middleware that catches any panic in a handler, logs the
// stack trace at ERROR level, and writes a 500 JSON response to the client.
func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recovered",
						slog.Any("panic", rec),
						slog.String("stack", string(debug.Stack())),
					)
					respond.Error(w, http.StatusInternalServerError, "an unexpected error occurred", logger)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
