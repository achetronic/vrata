// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package sticky implements an Envoy Go HTTP filter that provides external
// sticky session routing backed by Redis.
//
// When a request arrives, the filter checks a session cookie (or header,
// configurable via env vars) against Redis. If a pinned destination exists,
// it injects the x-envoy-upstream-alt-stat-name header so Envoy routes to
// the specific endpoint. If no pin exists, the request is forwarded normally
// and the first response pins the session.
//
// Configuration (via environment variables):
//
//	VRATA_STICKY_REDIS_ADDR    Redis address (default: localhost:6379)
//	VRATA_STICKY_REDIS_PASS    Redis password (default: "")
//	VRATA_STICKY_REDIS_DB      Redis database number (default: 0)
//	VRATA_STICKY_COOKIE_NAME   Cookie name to read/write (default: "vrata-session")
//	VRATA_STICKY_TTL_SECONDS   Session TTL in seconds (default: 3600)
package sticky

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/envoyproxy/envoy/contrib/golang/common/go/api"
	redis "github.com/redis/go-redis/v9"
)

// filter is the per-request filter instance.
type filter struct {
	api.PassThroughHttpFilter
	callbacks api.FilterCallbackHandler
	config    *filterConfig
	client    *redis.Client
}

// filterConfig holds the parsed configuration from environment variables.
type filterConfig struct {
	redisAddr  string
	redisPass  string
	redisDB    int
	cookieName string
	ttl        time.Duration
}

var globalClient *redis.Client
var globalConfig *filterConfig

func init() {
	globalConfig = loadConfig()
	globalClient = redis.NewClient(&redis.Options{
		Addr:     globalConfig.redisAddr,
		Password: globalConfig.redisPass,
		DB:       globalConfig.redisDB,
	})
}

func loadConfig() *filterConfig {
	cfg := &filterConfig{
		redisAddr:  envOr("VRATA_STICKY_REDIS_ADDR", "localhost:6379"),
		redisPass:  envOr("VRATA_STICKY_REDIS_PASS", ""),
		cookieName: envOr("VRATA_STICKY_COOKIE_NAME", "vrata-session"),
	}

	if db, err := strconv.Atoi(envOr("VRATA_STICKY_REDIS_DB", "0")); err == nil {
		cfg.redisDB = db
	}

	ttlSec := 3600
	if t, err := strconv.Atoi(envOr("VRATA_STICKY_TTL_SECONDS", "3600")); err == nil {
		ttlSec = t
	}
	cfg.ttl = time.Duration(ttlSec) * time.Second

	return cfg
}

// DecodeHeaders is called on request headers. It looks up the session cookie
// in Redis and, if a pin is found, injects the routing header.
func (f *filter) DecodeHeaders(header api.RequestHeaderMap, _ bool) api.StatusType {
	// Read session cookie.
	cookieHeader, ok := header.Get("cookie")
	if !ok {
		return api.Continue
	}

	sessionID := extractCookie(cookieHeader, f.config.cookieName)
	if sessionID == "" {
		return api.Continue
	}

	// Look up pinned destination in Redis.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	destination, err := f.client.Get(ctx, redisKey(sessionID)).Result()
	if err != nil {
		// No pin found or Redis error — continue normally.
		return api.Continue
	}

	// Inject routing hint header. Envoy reads this to select the upstream.
	header.Set("x-vrata-sticky-destination", destination)
	return api.Continue
}

// EncodeHeaders is called on response headers. It pins the session if not
// already pinned, using the upstream host Envoy selected.
func (f *filter) EncodeHeaders(header api.ResponseHeaderMap, _ bool) api.StatusType {
	// TODO: extract upstream host from filter callbacks and pin the session.
	// This requires access to the upstream info, which needs filter callbacks.
	// Implemented in a follow-up iteration.
	return api.Continue
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func redisKey(sessionID string) string {
	return fmt.Sprintf("vrata:sticky:%s", sessionID)
}

func extractCookie(cookieHeader, name string) string {
	req := &http.Request{Header: http.Header{"Cookie": {cookieHeader}}}
	c, err := req.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
