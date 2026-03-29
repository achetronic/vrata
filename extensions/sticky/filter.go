// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package main is the entrypoint for the sticky Envoy Go filter plugin.
// It is compiled as a shared object (.so) and loaded by Envoy at startup.
//
// Build:
//
//	go build -buildmode=plugin -o sticky.so .
//
// How it works:
//
//  1. DecodeHeaders (request phase): reads the session cookie, looks up the
//     pinned upstream host in Redis, and calls SetUpstreamOverrideHost so
//     Envoy routes directly to that host, bypassing its load balancer.
//
//  2. EncodeHeaders (response phase): on the first response for a session
//     that had no pin, reads the upstream host Envoy selected via
//     StreamInfo().UpstreamRemoteAddress() and writes it to Redis with the
//     configured TTL. Subsequent requests from the same session will be
//     pinned to that host.
//
// Configuration (via environment variables in the Envoy container):
//
//	VRATA_STICKY_REDIS_ADDR    Redis address (default: localhost:6379)
//	VRATA_STICKY_REDIS_PASS    Redis password (default: "")
//	VRATA_STICKY_REDIS_DB      Redis database number (default: 0)
//	VRATA_STICKY_COOKIE_NAME   Cookie name to read/write (default: "vrata-session")
//	VRATA_STICKY_TTL_SECONDS   Session TTL in seconds (default: 3600)
//	VRATA_STICKY_STRICT        When true, 503 if pinned host unavailable (default: false)
package main

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
	api.PassThroughStreamFilter
	callbacks  api.FilterCallbackHandler
	config     *filterConfig
	client     *redis.Client
	sessionID  string // set in DecodeHeaders, read in EncodeHeaders
	wasPinned  bool   // true if the session had a pre-existing pin in Redis
}

// filterConfig holds the parsed configuration from environment variables.
type filterConfig struct {
	redisAddr  string
	redisPass  string
	redisDB    int
	cookieName string
	ttl        time.Duration
	strict     bool
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
		strict:     envOr("VRATA_STICKY_STRICT", "false") == "true",
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

// DecodeHeaders runs on the request path.
// If the session cookie maps to a pinned upstream host, it overrides the
// upstream selection via SetUpstreamOverrideHost so Envoy routes there directly.
func (f *filter) DecodeHeaders(header api.RequestHeaderMap, _ bool) api.StatusType {
	// Read session cookie.
	cookieHeader, ok := header.Get("cookie")
	if !ok {
		return api.Continue
	}

	f.sessionID = extractCookie(cookieHeader, f.config.cookieName)
	if f.sessionID == "" {
		return api.Continue
	}

	// Look up pinned upstream in Redis.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	upstreamAddr, err := f.client.Get(ctx, redisKey(f.sessionID)).Result()
	if err != nil {
		// No pin or Redis error — let Envoy choose normally.
		f.wasPinned = false
		return api.Continue
	}

	f.wasPinned = true

	// Override the upstream host. Envoy will route directly to this address,
	// bypassing its load balancing algorithm.
	if err := f.callbacks.DecoderFilterCallbacks().SetUpstreamOverrideHost(upstreamAddr, f.config.strict); err != nil {
		// Host not available or invalid address — fall through to normal LB.
		api.LogWarnf("sticky: SetUpstreamOverrideHost failed for %s: %v", upstreamAddr, err)
		f.wasPinned = false
	}

	return api.Continue
}

// EncodeHeaders runs on the response path.
// If this session had no pin, reads the upstream Envoy selected and writes
// it to Redis so future requests from this session go to the same host.
func (f *filter) EncodeHeaders(_ api.ResponseHeaderMap, _ bool) api.StatusType {
	// Nothing to pin if there was no session cookie or it was already pinned.
	if f.sessionID == "" || f.wasPinned {
		return api.Continue
	}

	// Read the upstream Envoy selected for this request.
	upstreamAddr, ok := f.callbacks.StreamInfo().UpstreamRemoteAddress()
	if !ok || upstreamAddr == "" {
		return api.Continue
	}

	// Write the pin to Redis asynchronously — we don't want to block the response.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		if err := f.client.Set(ctx, redisKey(f.sessionID), upstreamAddr, f.config.ttl).Err(); err != nil {
			api.LogWarnf("sticky: failed to write pin for session %s → %s: %v", f.sessionID, upstreamAddr, err)
		}
	}()

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

// ─────────────────────────────────────────────────────────────────────────────
// Filter registration
// ─────────────────────────────────────────────────────────────────────────────

func main() {}

func newFilter(callbacks api.FilterCallbackHandler) api.StreamFilter {
	return &filter{
		callbacks: callbacks,
		config:    globalConfig,
		client:    globalClient,
	}
}

func init() {
	api.RegisterHttpFilterFactoryAndConfigParser(
		"vrata.sticky",
		func(config interface{}, callbacks api.FilterCallbackHandler) api.StreamFilter {
			return newFilter(callbacks)
		},
		&api.EmptyConfig{},
	)
}
