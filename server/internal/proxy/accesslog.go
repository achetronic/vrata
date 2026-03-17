package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/achetronic/rutoso/internal/model"
)

// AccessLogMiddleware creates a middleware that logs each request.
func AccessLogMiddleware(cfg *model.AccessLogConfig) Middleware {
	if cfg == nil || cfg.Path == "" {
		return func(next http.Handler) http.Handler { return next }
	}

	var writer io.Writer
	switch cfg.Path {
	case "/dev/stdout", "stdout":
		writer = os.Stdout
	case "/dev/stderr", "stderr":
		writer = os.Stderr
	default:
		f, err := os.OpenFile(cfg.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			// If we can't open the log file, log to stdout as fallback.
			writer = os.Stdout
		} else {
			writer = f
		}
	}

	useJSON := cfg.JSON

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap response writer to capture status code.
			lrw := &loggingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(lrw, r)

			duration := time.Since(start)

			if useJSON {
				entry := map[string]interface{}{
					"timestamp":  start.UTC().Format(time.RFC3339Nano),
					"method":     r.Method,
					"path":       r.URL.Path,
					"status":     lrw.statusCode,
					"duration_ms": duration.Milliseconds(),
					"client_ip":  clientIP(r),
					"host":       r.Host,
					"user_agent": r.UserAgent(),
					"bytes":      lrw.bytesWritten,
				}
				data, _ := json.Marshal(entry)
				fmt.Fprintf(writer, "%s\n", data)
			} else {
				format := cfg.Format
				if format == "" {
					format = "%s %s %s %d %dms %d\n"
				}
				fmt.Fprintf(writer, format,
					r.Method,
					r.URL.Path,
					clientIP(r),
					lrw.statusCode,
					duration.Milliseconds(),
					lrw.bytesWritten,
				)
			}
		})
	}
}

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

func (lrw *loggingResponseWriter) Write(b []byte) (int, error) {
	n, err := lrw.ResponseWriter.Write(b)
	lrw.bytesWritten += int64(n)
	return n, err
}
