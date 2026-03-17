// Package middleware provides HTTP middleware for the Rutoso REST API.
package middleware

import (
	"log/slog"
	"net/http"

	"github.com/felixge/httpsnoop"
)

// Logger returns a middleware that logs each HTTP request using slog with
// method, path, status code, and elapsed time. It uses httpsnoop to capture
// metrics without breaking optional ResponseWriter interfaces (Flusher,
// Hijacker, etc.).
func Logger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			m := httpsnoop.CaptureMetrics(next, w, r)

			logger.Info("request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", m.Code),
				slog.Duration("duration", m.Duration),
			)
		})
	}
}
