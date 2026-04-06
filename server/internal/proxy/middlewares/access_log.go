// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package middlewares

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/felixge/httpsnoop"
	"github.com/google/uuid"

	"github.com/achetronic/vrata/internal/model"
)

// AccessLogMiddleware creates a middleware that logs request and response
// as two separate log entries linked by a generated request ID.
func AccessLogMiddleware(cfg *model.AccessLogConfig) Middleware {
	m, _ := AccessLogMiddlewareWithStop(cfg)
	return m
}

// AccessLogMiddlewareWithStop creates an access log middleware and returns a
// stop function that closes the log file handle (if file-based).
func AccessLogMiddlewareWithStop(cfg *model.AccessLogConfig) (Middleware, func()) {
	if cfg == nil || cfg.Path == "" {
		return passthrough, func() {}
	}
	if cfg.OnRequest == nil && cfg.OnResponse == nil {
		return passthrough, func() {}
	}

	lw := openLogWriter(cfg.Path)
	useJSON := cfg.JSON

	mw := Middleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := uuid.NewString()
			start := time.Now()
			originalPath := r.URL.Path

			if cfg.OnRequest != nil {
				entry := interpolateFields(cfg.OnRequest.Fields, r, nil, originalPath, 0, 0, requestID, start, 0)
				lw.writeLine(entry, useJSON)
			}

			m := httpsnoop.CaptureMetrics(next, w, r)

			if cfg.OnResponse != nil {
				entry := interpolateFields(cfg.OnResponse.Fields, r, w.Header(), originalPath, m.Code, m.Written, requestID, start, m.Duration)
				lw.writeLine(entry, useJSON)
			}
		})
	})

	return mw, lw.close
}

// logWriter wraps an io.Writer with a mutex to prevent interleaved lines
// from concurrent requests, and tracks the closer for cleanup.
type logWriter struct {
	mu     sync.Mutex
	w      io.Writer
	closer io.Closer
}

func openLogWriter(path string) *logWriter {
	switch path {
	case "/dev/stdout", "stdout":
		return &logWriter{w: os.Stdout}
	case "/dev/stderr", "stderr":
		return &logWriter{w: os.Stderr}
	default:
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			slog.Warn("accesslog: failed to open file, falling back to stdout",
				slog.String("path", path),
				slog.String("error", err.Error()),
			)
			return &logWriter{w: os.Stdout}
		}
		return &logWriter{w: f, closer: f}
	}
}

// writeLine writes a single log entry atomically.
func (lw *logWriter) writeLine(fields map[string]string, useJSON bool) {
	lw.mu.Lock()
	defer lw.mu.Unlock()

	if useJSON {
		data, err := json.Marshal(fields)
		if err != nil {
			slog.Warn("accesslog: failed to marshal JSON", slog.String("error", err.Error()))
			return
		}
		if _, err := fmt.Fprintf(lw.w, "%s\n", data); err != nil {
			slog.Warn("accesslog: failed to write log entry", slog.String("error", err.Error()))
		}
	} else {
		keys := make([]string, 0, len(fields))
		for k := range fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(fields))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s=%s", k, fields[k]))
		}
		if _, err := fmt.Fprintf(lw.w, "%s\n", strings.Join(parts, " ")); err != nil {
			slog.Warn("accesslog: failed to write log entry", slog.String("error", err.Error()))
		}
	}
}

// close releases the file handle if this writer owns one.
func (lw *logWriter) close() {
	if lw.closer != nil {
		if err := lw.closer.Close(); err != nil {
			slog.Warn("accesslog: failed to close file", slog.String("error", err.Error()))
		}
	}
}

func interpolateFields(
	fields map[string]string,
	r *http.Request,
	respHeaders http.Header,
	originalPath string,
	statusCode int,
	bytesWritten int64,
	requestID string,
	start time.Time,
	duration time.Duration,
) map[string]string {
	result := make(map[string]string, len(fields))

	for key, tmpl := range fields {
		val := tmpl

		val = strings.ReplaceAll(val, "${id}", requestID)
		val = strings.ReplaceAll(val, "${request.method}", r.Method)
		val = strings.ReplaceAll(val, "${request.path}", originalPath)
		val = strings.ReplaceAll(val, "${request.host}", reqHost(r))
		val = strings.ReplaceAll(val, "${request.authority}", r.Host)
		val = strings.ReplaceAll(val, "${request.clientIp}", clientIPFromRequest(r))

		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		val = strings.ReplaceAll(val, "${request.scheme}", scheme)

		pos := 0
		for {
			idx := strings.Index(val[pos:], "${request.header.")
			if idx == -1 {
				break
			}
			s := pos + idx
			end := strings.Index(val[s:], "}")
			if end == -1 {
				break
			}
			end += s
			headerName := val[s+len("${request.header.") : end]
			headerValue := r.Header.Get(headerName)
			val = val[:s] + headerValue + val[end+1:]
			pos = s + len(headerValue)
		}

		val = strings.ReplaceAll(val, "${response.status}", fmt.Sprintf("%d", statusCode))
		val = strings.ReplaceAll(val, "${response.bytes}", fmt.Sprintf("%d", bytesWritten))

		// Response header interpolation: ${response.header.NAME}
		if respHeaders != nil {
			pos = 0
			for {
				idx := strings.Index(val[pos:], "${response.header.")
				if idx == -1 {
					break
				}
				s := pos + idx
				end := strings.Index(val[s:], "}")
				if end == -1 {
					break
				}
				end += s
				headerName := val[s+len("${response.header."):end]
				headerValue := respHeaders.Get(headerName)
				val = val[:s] + headerValue + val[end+1:]
				pos = s + len(headerValue)
			}
		}

		val = strings.ReplaceAll(val, "${duration.ms}", fmt.Sprintf("%d", duration.Milliseconds()))
		val = strings.ReplaceAll(val, "${duration.us}", fmt.Sprintf("%d", duration.Microseconds()))
		val = strings.ReplaceAll(val, "${duration.s}", fmt.Sprintf("%.3f", duration.Seconds()))

		result[key] = val
	}

	return result
}

func reqHost(r *http.Request) string {
	host := r.Host
	if idx := strings.Index(host, ":"); idx != -1 {
		return host[:idx]
	}
	return host
}

func clientIPFromRequest(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}
