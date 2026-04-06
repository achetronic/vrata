// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package middlewares

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/achetronic/vrata/internal/model"
	extauthzv1 "github.com/achetronic/vrata/proto/extauthz/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ExtAuthzMiddleware creates a middleware that sends a check request to an
// external authorization service before forwarding to the upstream.
// Supports both HTTP and gRPC modes.
func ExtAuthzMiddleware(cfg *model.ExtAuthzConfig, services map[string]Service) Middleware {
	mw, _ := ExtAuthzMiddlewareWithStop(cfg, services)
	return mw
}

// ExtAuthzMiddlewareWithStop creates an ExtAuthz middleware and returns a stop
// function that closes the underlying gRPC connection (if any). The stop
// function must be registered as a cleanup callback on the routing table.
func ExtAuthzMiddlewareWithStop(cfg *model.ExtAuthzConfig, services map[string]Service) (Middleware, func()) {
	noop := func() {}
	if cfg == nil || cfg.DestinationID == "" {
		return passthrough, noop
	}

	svc, ok := services[cfg.DestinationID]
	if !ok {
		slog.Error("extauthz: destination not found", slog.String("destinationId", cfg.DestinationID))
		return passthrough, noop
	}

	timeout := 5 * time.Second
	if cfg.DecisionTimeout != "" {
		if d, err := time.ParseDuration(cfg.DecisionTimeout); err == nil {
			timeout = d
		}
	}

	if cfg.Mode == "grpc" {
		mw, stop := extAuthzGRPCWithStop(cfg, svc, timeout)
		return mw, stop
	}
	return extAuthzHTTP(cfg, svc, timeout), noop
}

// ─────────────────────────────────────────────────────────────────────────────
// HTTP mode
// ─────────────────────────────────────────────────────────────────────────────

func extAuthzHTTP(cfg *model.ExtAuthzConfig, svc Service, timeout time.Duration) Middleware {
	baseURL := svc.BaseURL
	if cfg.Path != "" {
		baseURL += cfg.Path
	}

	maxBody := cfg.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = 1048576
	}

	forwardHeaders := map[string]bool{"host": true, "content-length": true}
	if cfg.OnCheck != nil {
		for _, h := range cfg.OnCheck.ForwardHeaders {
			forwardHeaders[strings.ToLower(h)] = true
		}
	}

	var allowPatterns []string
	if cfg.OnAllow != nil {
		allowPatterns = cfg.OnAllow.CopyToUpstream
	}

	denyPatterns := []string{"location", "set-cookie", "www-authenticate", "content-type"}
	if cfg.OnDeny != nil {
		denyPatterns = append(denyPatterns, cfg.OnDeny.CopyToClient...)
	}

	client := &http.Client{
		Transport: svc.Transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var checkBody io.Reader
			if cfg.IncludeBody && r.Body != nil {
				bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
				if err != nil {
					handleAuthzError(w, r, next, cfg.FailureModeAllow, "failed to read request body")
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				checkBody = bytes.NewReader(bodyBytes)
			}

			checkReq, err := http.NewRequestWithContext(r.Context(), r.Method, baseURL, checkBody)
			if err != nil {
				handleAuthzError(w, r, next, cfg.FailureModeAllow, "failed to create check request")
				return
			}

			for key, values := range r.Header {
				if forwardHeaders[strings.ToLower(key)] {
					for _, v := range values {
						checkReq.Header.Add(key, v)
					}
				}
			}

			if cfg.OnCheck != nil {
				for _, h := range cfg.OnCheck.InjectHeaders {
					checkReq.Header.Set(h.Key, Interpolate(h.Value, r))
				}
			}

			resp, err := client.Do(checkReq)
			if err != nil {
				handleAuthzError(w, r, next, cfg.FailureModeAllow, "authz service unreachable")
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				if len(allowPatterns) > 0 {
					copyMatchingHeaders(r.Header, resp.Header, allowPatterns)
				}
				next.ServeHTTP(w, r)
				return
			}

			copyMatchingHeaders(w.Header(), resp.Header, denyPatterns)
			w.WriteHeader(resp.StatusCode)
			if _, err := io.Copy(w, resp.Body); err != nil {
				slog.Warn("extauthz: failed to copy deny body", slog.String("error", err.Error()))
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// gRPC mode
// ─────────────────────────────────────────────────────────────────────────────

func extAuthzGRPCWithStop(cfg *model.ExtAuthzConfig, svc Service, timeout time.Duration) (Middleware, func()) {
	maxBody := cfg.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = 1048576
	}

	forwardHeaders := map[string]bool{"host": true, "content-length": true}
	if cfg.OnCheck != nil {
		for _, h := range cfg.OnCheck.ForwardHeaders {
			forwardHeaders[strings.ToLower(h)] = true
		}
	}

	var allowPatterns []string
	if cfg.OnAllow != nil {
		allowPatterns = cfg.OnAllow.CopyToUpstream
	}

	denyPatterns := []string{"location", "set-cookie", "www-authenticate", "content-type"}
	if cfg.OnDeny != nil {
		denyPatterns = append(denyPatterns, cfg.OnDeny.CopyToClient...)
	}

	target := strings.TrimPrefix(strings.TrimPrefix(svc.BaseURL, "http://"), "https://")

	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		slog.Error("extauthz: failed to create gRPC connection", slog.String("error", err.Error()))
		return passthrough, func() {}
	}
	stop := func() {
		if err := conn.Close(); err != nil {
			slog.Warn("extauthz: failed to close gRPC connection", slog.String("error", err.Error()))
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			var headers []*extauthzv1.HeaderPair
			for key, values := range r.Header {
				if forwardHeaders[strings.ToLower(key)] {
					for _, v := range values {
						headers = append(headers, &extauthzv1.HeaderPair{Key: strings.ToLower(key), Value: v})
					}
				}
			}

			if cfg.OnCheck != nil {
				for _, h := range cfg.OnCheck.InjectHeaders {
					headers = append(headers, &extauthzv1.HeaderPair{Key: h.Key, Value: Interpolate(h.Value, r)})
				}
			}

			var body []byte
			if cfg.IncludeBody && r.Body != nil {
				bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
				if err != nil {
					handleAuthzError(w, r, next, cfg.FailureModeAllow, "failed to read request body")
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				body = bodyBytes
			}

			checkReq := &extauthzv1.CheckRequest{
				Method:  r.Method,
				Path:    r.URL.RequestURI(),
				Headers: headers,
				Body:    body,
			}

			client := extauthzv1.NewAuthorizerClient(conn)
			resp, err := client.Check(ctx, checkReq)
			if err != nil {
				handleAuthzError(w, r, next, cfg.FailureModeAllow, "grpc check failed: "+err.Error())
				return
			}

			if resp.Allowed {
				if len(allowPatterns) > 0 {
					tempHeader := make(http.Header)
					for _, h := range resp.Headers {
						tempHeader.Add(h.Key, h.Value)
					}
					copyMatchingHeaders(r.Header, tempHeader, allowPatterns)
				}
				next.ServeHTTP(w, r)
				return
			}

			tempHeader := make(http.Header)
			for _, h := range resp.Headers {
				tempHeader.Add(h.Key, h.Value)
			}
			copyMatchingHeaders(w.Header(), tempHeader, denyPatterns)
			status := int(resp.DeniedStatus)
			if status == 0 {
				status = http.StatusForbidden
			}
			w.WriteHeader(status)
			if resp.DeniedBody != nil {
				if _, err := w.Write(resp.DeniedBody); err != nil {
					slog.Warn("extauthz: failed to write deny body", slog.String("error", err.Error()))
				}
			}
		})
	}, stop
}

func handleAuthzError(w http.ResponseWriter, r *http.Request, next http.Handler, allow bool, msg string) {
	if allow {
		next.ServeHTTP(w, r)
		return
	}
	writeJSONError(w, http.StatusForbidden, "ext_authz: "+msg)
}
