package middlewares

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/achetronic/rutoso/internal/model"
)

// RateLimitMiddleware creates a middleware that rate-limits requests using
// an embedded token bucket per client IP.
func RateLimitMiddleware(cfg *model.RateLimitConfig) Middleware {
	if cfg == nil {
		return passthrough
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
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

// tokenBucketLimiter is a per-key token bucket rate limiter with automatic
// eviction of stale entries.
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

const bucketEvictionInterval = 60 * time.Second

func newTokenBucketLimiter(rps float64, burst int) *tokenBucketLimiter {
	if rps <= 0 {
		rps = 10
	}
	if burst <= 0 {
		burst = int(rps)
	}
	l := &tokenBucketLimiter{
		rps:     rps,
		burst:   burst,
		buckets: make(map[string]*bucket),
	}
	go l.evictLoop()
	return l
}

// Allow checks whether a request from the given key is allowed.
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

// evictLoop periodically removes buckets that have been idle long enough
// to have fully refilled, preventing unbounded memory growth.
func (l *tokenBucketLimiter) evictLoop() {
	ticker := time.NewTicker(bucketEvictionInterval)
	defer ticker.Stop()
	for range ticker.C {
		l.evictStale()
	}
}

func (l *tokenBucketLimiter) evictStale() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	refillTime := time.Duration(float64(time.Second) * float64(l.burst) / l.rps)
	threshold := refillTime * 2

	for key, b := range l.buckets {
		if now.Sub(b.lastTime) > threshold {
			delete(l.buckets, key)
		}
	}
}
