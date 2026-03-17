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

	"github.com/achetronic/rutoso/internal/model"
	"github.com/google/uuid"
)

// AccessLogMiddleware creates a middleware that logs request and response
// as two separate log entries linked by a generated request ID.
func AccessLogMiddleware(cfg *model.AccessLogConfig) Middleware {
	if cfg == nil || cfg.Path == "" {
		return func(next http.Handler) http.Handler { return next }
	}
	if cfg.OnRequest == nil && cfg.OnResponse == nil {
		return func(next http.Handler) http.Handler { return next }
	}

	writer := openLogWriter(cfg.Path)
	useJSON := cfg.JSON

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := uuid.NewString()
			start := time.Now()

			// Log request.
			if cfg.OnRequest != nil {
				entry := interpolateFields(cfg.OnRequest.Fields, r, nil, requestID, start, 0, 0)
				writeLine(writer, entry, useJSON)
			}

			// Wrap response writer to capture status and bytes.
			lrw := &logResponseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			next.ServeHTTP(lrw, r)

			// Log response.
			if cfg.OnResponse != nil {
				duration := time.Since(start)
				entry := interpolateFields(cfg.OnResponse.Fields, r, lrw, requestID, start, duration, lrw.bytesWritten)
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
	lrw *logResponseWriter,
	requestID string,
	start time.Time,
	duration time.Duration,
	bytesWritten int64,
) map[string]string {
	result := make(map[string]string, len(fields))

	for key, tmpl := range fields {
		val := tmpl

		// Static replacements.
		val = strings.ReplaceAll(val, "${id}", requestID)

		// Request fields.
		val = strings.ReplaceAll(val, "${request.method}", r.Method)
		val = strings.ReplaceAll(val, "${request.path}", r.URL.Path)
		val = strings.ReplaceAll(val, "${request.host}", reqHost(r))
		val = strings.ReplaceAll(val, "${request.authority}", r.Host)
		val = strings.ReplaceAll(val, "${request.clientIp}", clientIPFromRequest(r))

		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		val = strings.ReplaceAll(val, "${request.scheme}", scheme)

		// Request headers.
		for {
			start := strings.Index(val, "${request.header.")
			if start == -1 {
				break
			}
			end := strings.Index(val[start:], "}")
			if end == -1 {
				break
			}
			end += start
			placeholder := val[start : end+1]
			headerName := val[start+len("${request.header.") : end]
			val = strings.Replace(val, placeholder, r.Header.Get(headerName), 1)
		}

		// Response fields (only available in onResponse).
		if lrw != nil {
			val = strings.ReplaceAll(val, "${response.status}", fmt.Sprintf("%d", lrw.statusCode))
			val = strings.ReplaceAll(val, "${response.bytes}", fmt.Sprintf("%d", bytesWritten))

			// Response headers.
			for {
				s := strings.Index(val, "${response.header.")
				if s == -1 {
					break
				}
				e := strings.Index(val[s:], "}")
				if e == -1 {
					break
				}
				e += s
				placeholder := val[s : e+1]
				headerName := val[s+len("${response.header.") : e]
				val = strings.Replace(val, placeholder, lrw.Header().Get(headerName), 1)
			}
		}

		// Duration fields.
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

// logResponseWriter captures status code and bytes written.
type logResponseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
	wroteHeader  bool
}

func (lrw *logResponseWriter) WriteHeader(code int) {
	if !lrw.wroteHeader {
		lrw.wroteHeader = true
		lrw.statusCode = code
	}
	lrw.ResponseWriter.WriteHeader(code)
}

func (lrw *logResponseWriter) Write(b []byte) (int, error) {
	if !lrw.wroteHeader {
		lrw.WriteHeader(http.StatusOK)
	}
	n, err := lrw.ResponseWriter.Write(b)
	lrw.bytesWritten += int64(n)
	return n, err
}
