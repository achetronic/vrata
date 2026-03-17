package middlewares

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/achetronic/rutoso/internal/model"
)

// CORSMiddleware creates a CORS middleware from a CORSConfig.
func CORSMiddleware(cfg *model.CORSConfig) Middleware {
	if cfg == nil {
		return func(next http.Handler) http.Handler { return next }
	}

	// Pre-compile regex origins.
	type originMatcher struct {
		exact string
		regex *regexp.Regexp
	}
	var matchers []originMatcher
	for _, o := range cfg.AllowOrigins {
		if o.Regex {
			if re, err := regexp.Compile(o.Value); err == nil {
				matchers = append(matchers, originMatcher{regex: re})
			}
		} else {
			matchers = append(matchers, originMatcher{exact: o.Value})
		}
	}

	methods := strings.Join(cfg.AllowMethods, ", ")
	headers := strings.Join(cfg.AllowHeaders, ", ")
	expose := strings.Join(cfg.ExposeHeaders, ", ")
	maxAge := ""
	if cfg.MaxAge > 0 {
		maxAge = strconv.Itoa(int(cfg.MaxAge))
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Check if origin is allowed.
			allowed := false
			for _, m := range matchers {
				if m.exact == "*" || m.exact == origin {
					allowed = true
					break
				}
				if m.regex != nil && m.regex.MatchString(origin) {
					allowed = true
					break
				}
			}

			if !allowed {
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("Access-Control-Allow-Origin", origin)
			if cfg.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			if expose != "" {
				w.Header().Set("Access-Control-Expose-Headers", expose)
			}

			// Preflight.
			if r.Method == http.MethodOptions {
				if methods != "" {
					w.Header().Set("Access-Control-Allow-Methods", methods)
				}
				if headers != "" {
					w.Header().Set("Access-Control-Allow-Headers", headers)
				}
				if maxAge != "" {
					w.Header().Set("Access-Control-Max-Age", maxAge)
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
