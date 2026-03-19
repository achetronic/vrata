// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/achetronic/vrata/internal/model"
)

// ListenerManager manages HTTP listeners that serve proxied traffic.
// Each model.Listener gets its own http.Server. On config reload, listeners
// are added, updated, or removed without dropping active connections.
type ListenerManager struct {
	mu       sync.Mutex
	servers  map[string]*managedServer // keyed by listener ID
	router   *Router
	logger   *slog.Logger
}

type managedServer struct {
	listener model.Listener
	server   *http.Server
	metrics  *MetricsCollector
	cancel   context.CancelFunc
	done     chan struct{}
}

// NewListenerManager creates a ListenerManager.
func NewListenerManager(router *Router, logger *slog.Logger) *ListenerManager {
	return &ListenerManager{
		servers: make(map[string]*managedServer),
		router:  router,
		logger:  logger,
	}
}

// Reconcile updates listeners to match the desired state. New listeners are
// started, removed listeners are gracefully shut down, and changed listeners
// are restarted.
func (lm *ListenerManager) Reconcile(listeners []model.Listener) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	desired := make(map[string]model.Listener, len(listeners))
	for _, l := range listeners {
		desired[l.ID] = l
	}

	// Remove listeners that no longer exist.
	for id, ms := range lm.servers {
		if _, ok := desired[id]; !ok {
			lm.logger.Info("proxy: stopping listener",
				slog.String("id", id),
				slog.String("name", ms.listener.Name),
			)
			if ms.metrics != nil {
				ms.metrics.Stop()
			}
			ms.cancel()
			<-ms.done
			delete(lm.servers, id)
		}
	}

	// Add or update listeners.
	for id, l := range desired {
		existing, ok := lm.servers[id]
		if ok && sameListener(existing.listener, l) {
			continue
		}

		// Stop old if updating. Wait for it to fully release the port.
		if ok {
			lm.logger.Info("proxy: restarting listener",
				slog.String("id", id),
				slog.String("name", l.Name),
			)
			if existing.metrics != nil {
				existing.metrics.Stop()
			}
			existing.cancel()
			<-existing.done
			delete(lm.servers, id)
		}

		// Start new.
		lm.startListener(l)
	}
}

// startListener creates and starts an http.Server for the given listener.
func (lm *ListenerManager) startListener(l model.Listener) {
	addr := l.Address
	if addr == "" {
		addr = "0.0.0.0"
	}
	bindAddr := fmt.Sprintf("%s:%d", addr, l.Port)

	ctx, cancel := context.WithCancel(context.Background())

	srv := &http.Server{
		Addr:              bindAddr,
		Handler:           lm.router,
		ReadHeaderTimeout: parseDurationOrDefault(l.Timeouts, func(t *model.ListenerTimeouts) string { return t.ClientHeader }, 10*time.Second),
		ReadTimeout:       parseDurationOrDefault(l.Timeouts, func(t *model.ListenerTimeouts) string { return t.ClientRequest }, 60*time.Second),
		WriteTimeout:      parseDurationOrDefault(l.Timeouts, func(t *model.ListenerTimeouts) string { return t.ClientResponse }, 60*time.Second),
		IdleTimeout:       parseDurationOrDefault(l.Timeouts, func(t *model.ListenerTimeouts) string { return t.IdleBetweenRequests }, 120*time.Second),
	}

	// TLS.
	if l.TLS != nil && l.TLS.CertPath != "" && l.TLS.KeyPath != "" {
		cert, err := tls.LoadX509KeyPair(l.TLS.CertPath, l.TLS.KeyPath)
		if err != nil {
			lm.logger.Error("proxy: failed to load TLS cert",
				slog.String("id", l.ID),
				slog.String("error", err.Error()),
			)
			cancel()
			return
		}
		srv.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
		}
		if v, ok := tlsVersionMap[l.TLS.MinVersion]; ok {
			srv.TLSConfig.MinVersion = v
		}
		if v, ok := tlsVersionMap[l.TLS.MaxVersion]; ok {
			srv.TLSConfig.MaxVersion = v
		}
	}

	// Server name via response header middleware.
	if l.ServerName != "" || l.MaxRequestHeadersKB > 0 {
		original := srv.Handler
		srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if l.ServerName != "" {
				w.Header().Set("Server", l.ServerName)
			}
			if l.MaxRequestHeadersKB > 0 {
				// Check total header size (approximate).
				totalSize := 0
				for k, values := range r.Header {
					for _, v := range values {
						totalSize += len(k) + len(v)
					}
				}
				if totalSize > int(l.MaxRequestHeadersKB)*1024 {
					writeProxyError(w, http.StatusRequestHeaderFieldsTooLarge, "request headers too large")
					return
				}
			}
			original.ServeHTTP(w, r)
		})
	}

	// Metrics collector.
	var mc *MetricsCollector
	if l.Metrics != nil {
		mc = NewMetricsCollector(l.Metrics)
		mc.Start()

		metricsPath := l.Metrics.ResolvedPath()
		routerHandler := srv.Handler
		mux := http.NewServeMux()
		mux.Handle(metricsPath, mc.Handler())
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			routerHandler.ServeHTTP(w, r)
		})
		srv.Handler = mux

		lm.logger.Info("proxy: metrics enabled",
			slog.String("id", l.ID),
			slog.String("path", metricsPath),
		)
	}

	done := make(chan struct{})

	lm.servers[l.ID] = &managedServer{
		listener: l,
		server:   srv,
		metrics:  mc,
		cancel:   cancel,
		done:     done,
	}

	go func() {
		defer close(done)

		ln, err := net.Listen("tcp", bindAddr)
		if err != nil {
			lm.logger.Error("proxy: failed to listen",
				slog.String("address", bindAddr),
				slog.String("error", err.Error()),
			)
			return
		}

		lm.logger.Info("proxy: listener started",
			slog.String("id", l.ID),
			slog.String("name", l.Name),
			slog.String("address", bindAddr),
		)

		go func() {
			<-ctx.Done()
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer shutdownCancel()
			srv.Shutdown(shutdownCtx)
		}()

		if srv.TLSConfig != nil {
			srv.ServeTLS(ln, "", "")
		} else {
			srv.Serve(ln)
		}
	}()
}

// MetricsCollectors returns all active metrics collectors across managed
// listeners. Used by the gateway to update pool references after a rebuild.
func (lm *ListenerManager) MetricsCollectors() []*MetricsCollector {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	var out []*MetricsCollector
	for _, ms := range lm.servers {
		if ms.metrics != nil {
			out = append(out, ms.metrics)
		}
	}
	return out
}

// Shutdown gracefully stops all listeners.
func (lm *ListenerManager) Shutdown() {

	lm.mu.Lock()
	defer lm.mu.Unlock()

	for id, ms := range lm.servers {
		if ms.metrics != nil {
			ms.metrics.Stop()
		}
		ms.cancel()
		<-ms.done
		delete(lm.servers, id)
	}
}

// sameListener checks if two listener configs are identical (no restart needed).
func sameListener(a, b model.Listener) bool {
	if a.Address != b.Address || a.Port != b.Port ||
		a.ServerName != b.ServerName || a.HTTP2 != b.HTTP2 ||
		a.MaxRequestHeadersKB != b.MaxRequestHeadersKB {
		return false
	}
	if !sameTLS(a.TLS, b.TLS) {
		return false
	}
	return sameMetrics(a.Metrics, b.Metrics)
}

// sameTLS compares two TLS configs for equality.
func sameTLS(a, b *model.ListenerTLS) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.CertPath == b.CertPath &&
		a.KeyPath == b.KeyPath &&
		a.MinVersion == b.MinVersion &&
		a.MaxVersion == b.MaxVersion
}

// sameMetrics compares two metrics configs for equality.
func sameMetrics(a, b *model.ListenerMetrics) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.ResolvedPath() == b.ResolvedPath()
}

// parseDurationOrDefault extracts a duration string from a timeouts struct
// using the provided accessor. Returns fallback if the struct is nil, the
// field is empty, or the value cannot be parsed.
func parseDurationOrDefault[T any](cfg *T, accessor func(*T) string, fallback time.Duration) time.Duration {
	if cfg == nil {
		return fallback
	}
	s := accessor(cfg)
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}
