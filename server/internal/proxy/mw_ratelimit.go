package proxy

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/achetronic/rutoso/internal/model"
)

// RateLimitMiddleware creates a middleware that rate-limits requests using
// an embedded token bucket per client IP.
func RateLimitMiddleware(cfg *model.RateLimitConfig) Middleware {
	if cfg == nil {
		return func(next http.Handler) http.Handler { return next }
	}

	limiter := newTokenBucketLimiter(cfg.RequestsPerSecond, cfg.Burst)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := clientIP(r)
			if !limiter.Allow(key) {
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := splitFirst(xff, ",")
		return trimSpace(parts)
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

func splitFirst(s, sep string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == sep[0] {
			return s[:i]
		}
	}
	return s
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && s[start] == ' ' {
		start++
	}
	for end > start && s[end-1] == ' ' {
		end--
	}
	return s[start:end]
}

// tokenBucketLimiter is a per-key token bucket rate limiter.
type tokenBucketLimiter struct {
	rps     float64
	burst   int
	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens   float64
	lastTime time.Time
}

func newTokenBucketLimiter(rps float64, burst int) *tokenBucketLimiter {
	if rps <= 0 {
		rps = 10
	}
	if burst <= 0 {
		burst = int(rps)
	}
	return &tokenBucketLimiter{
		rps:     rps,
		burst:   burst,
		buckets: make(map[string]*bucket),
	}
}

func (l *tokenBucketLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: float64(l.burst), lastTime: now}
		l.buckets[key] = b
	}

	elapsed := now.Sub(b.lastTime).Seconds()
	b.tokens += elapsed * l.rps
	if b.tokens > float64(l.burst) {
		b.tokens = float64(l.burst)
	}
	b.lastTime = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
