package middlewares

import (
	"net/http"

	"github.com/felixge/httpsnoop"

	"github.com/achetronic/vrata/internal/model"
)

// HeadersMiddleware creates a middleware that adds/removes request and
// response headers.
func HeadersMiddleware(cfg *model.HeadersConfig) Middleware {
	if cfg == nil {
		return passthrough
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, h := range cfg.RequestHeadersToAdd {
				if h.Append {
					r.Header.Add(h.Key, h.Value)
				} else {
					r.Header.Set(h.Key, h.Value)
				}
			}

			for _, name := range cfg.RequestHeadersToRemove {
				r.Header.Del(name)
			}

			wrappedW := httpsnoop.Wrap(w, httpsnoop.Hooks{
				WriteHeader: func(next httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
					return func(code int) {
						for _, h := range cfg.ResponseHeadersToAdd {
							if h.Append {
								w.Header().Add(h.Key, h.Value)
							} else {
								w.Header().Set(h.Key, h.Value)
							}
						}
						for _, name := range cfg.ResponseHeadersToRemove {
							w.Header().Del(name)
						}
						next(code)
					}
				},
			})

			next.ServeHTTP(wrappedW, r)
		})
	}
}
