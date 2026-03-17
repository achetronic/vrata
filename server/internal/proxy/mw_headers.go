package proxy

import (
	"net/http"

	"github.com/achetronic/rutoso/internal/model"
)

// HeadersMiddleware creates a middleware that adds/removes request and
// response headers.
func HeadersMiddleware(cfg *model.HeadersConfig) Middleware {
	if cfg == nil {
		return func(next http.Handler) http.Handler { return next }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Request headers: add.
			for _, h := range cfg.RequestHeadersToAdd {
				if h.Append {
					r.Header.Add(h.Key, h.Value)
				} else {
					r.Header.Set(h.Key, h.Value)
				}
			}

			// Request headers: remove.
			for _, name := range cfg.RequestHeadersToRemove {
				r.Header.Del(name)
			}

			// Wrap response writer to intercept response headers.
			rw := &headerResponseWriter{
				ResponseWriter: w,
				cfg:            cfg,
				wroteHeader:    false,
			}

			next.ServeHTTP(rw, r)
		})
	}
}

// headerResponseWriter intercepts WriteHeader to add/remove response headers.
type headerResponseWriter struct {
	http.ResponseWriter
	cfg         *model.HeadersConfig
	wroteHeader bool
}

func (rw *headerResponseWriter) WriteHeader(code int) {
	if !rw.wroteHeader {
		rw.wroteHeader = true

		// Response headers: add.
		for _, h := range rw.cfg.ResponseHeadersToAdd {
			if h.Append {
				rw.ResponseWriter.Header().Add(h.Key, h.Value)
			} else {
				rw.ResponseWriter.Header().Set(h.Key, h.Value)
			}
		}

		// Response headers: remove.
		for _, name := range rw.cfg.ResponseHeadersToRemove {
			rw.ResponseWriter.Header().Del(name)
		}
	}

	rw.ResponseWriter.WriteHeader(code)
}

func (rw *headerResponseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}
