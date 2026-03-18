package middlewares

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/felixge/httpsnoop"
	"github.com/google/uuid"

	"github.com/achetronic/rutoso/internal/model"
)

// AccessLogMiddleware creates a middleware that logs request and response
// as two separate log entries linked by a generated request ID.
func AccessLogMiddleware(cfg *model.AccessLogConfig) Middleware {
	if cfg == nil || cfg.Path == "" {
		return passthrough
	}
	if cfg.OnRequest == nil && cfg.OnResponse == nil {
		return passthrough
	}

	writer := openLogWriter(cfg.Path)
	useJSON := cfg.JSON

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := uuid.NewString()
			start := time.Now()
			originalPath := r.URL.Path

			if cfg.OnRequest != nil {
				entry := interpolateFields(cfg.OnRequest.Fields, r, originalPath, 0, 0, requestID, start, 0)
				writeLine(writer, entry, useJSON)
			}

			m := httpsnoop.CaptureMetrics(next, w, r)

			if cfg.OnResponse != nil {
				entry := interpolateFields(cfg.OnResponse.Fields, r, originalPath, m.Code, m.Written, requestID, start, m.Duration)
				writeLine(writer, entry, useJSON)
			}
		})
	}
}

func openLogWriter(path string) io.Writer {
	switch path {
	case "/dev/stdout", "stdout":
		return os.Stdout
	case "/dev/stderr", "stderr":
		return os.Stderr
	default:
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return os.Stdout
		}
		return f
	}
}

func interpolateFields(
	fields map[string]string,
	r *http.Request,
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

		val = strings.ReplaceAll(val, "${duration.ms}", fmt.Sprintf("%d", duration.Milliseconds()))
		val = strings.ReplaceAll(val, "${duration.us}", fmt.Sprintf("%d", duration.Microseconds()))
		val = strings.ReplaceAll(val, "${duration.s}", fmt.Sprintf("%.3f", duration.Seconds()))

		result[key] = val
	}

	return result
}

func writeLine(w io.Writer, fields map[string]string, useJSON bool) {
	if useJSON {
		data, _ := json.Marshal(fields)
		fmt.Fprintf(w, "%s\n", data)
	} else {
		parts := make([]string, 0, len(fields))
		for k, v := range fields {
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		}
		fmt.Fprintf(w, "%s\n", strings.Join(parts, " "))
	}
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
