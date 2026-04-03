// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package middlewares

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/achetronic/vrata/internal/model"
)

// RateLimitMiddleware creates a middleware that rate-limits requests using
// an embedded token bucket per client IP. Returns the middleware and a stop
// function to halt the background eviction goroutine.
func RateLimitMiddleware(cfg *model.RateLimitConfig) Middleware {
	m, _ := RateLimitMiddlewareWithStop(cfg)
	return m
}

// RateLimitMiddlewareWithStop creates a rate limit middleware and returns a
// stop function that halts the background eviction goroutine.
func RateLimitMiddlewareWithStop(cfg *model.RateLimitConfig) (Middleware, func()) {
	if cfg == nil {
		return passthrough, nil
	}

	limiter := newTokenBucketLimiter(cfg.RequestsPerSecond, cfg.Burst)
	trustedNets := parseTrustedProxies(cfg.TrustedProxies)

	mw := Middleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := clientIP(r, trustedNets)
			if !limiter.Allow(key) {
				writeJSONError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	stop := func() {
		close(limiter.stop)
	}

	return mw, stop
}

// parseTrustedProxies parses CIDR strings into net.IPNet for fast lookup.
func parseTrustedProxies(cidrs []string) []*net.IPNet {
	var nets []*net.IPNet
	for _, cidr := range cidrs {
		_, n, err := net.ParseCIDR(cidr)
		if err == nil {
			nets = append(nets, n)
		}
	}
	return nets
}

// clientIP extracts the client IP, only trusting X-Forwarded-For when the
// direct connection comes from a trusted proxy.
func clientIP(r *http.Request, trusted []*net.IPNet) string {
	directIP, _, _ := net.SplitHostPort(r.RemoteAddr)

	if len(trusted) > 0 && r.Header.Get("X-Forwarded-For") != "" {
		ip := net.ParseIP(directIP)
		isTrusted := false
		for _, n := range trusted {
			if n.Contains(ip) {
				isTrusted = true
				break
			}
		}
		if isTrusted {
			xff := r.Header.Get("X-Forwarded-For")
			if idx := strings.Index(xff, ","); idx != -1 {
				return strings.TrimSpace(xff[:idx])
			}
			return strings.TrimSpace(xff)
		}
	}

	return directIP
}

type tokenBucketLimiter struct {
	rps     float64
	burst   int
	mu      sync.Mutex
	buckets map[string]*bucket
	stop    chan struct{}
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
		stop:    make(chan struct{}),
	}
	go l.evictLoop()
	return l
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

func (l *tokenBucketLimiter) evictLoop() {
	ticker := time.NewTicker(bucketEvictionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.evictStale()
		case <-l.stop:
			return
		}
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
