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

	"github.com/achetronic/rutoso/internal/model"
	"github.com/achetronic/rutoso/internal/proxy/middlewares"
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
	cancel   context.CancelFunc
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
			ms.cancel()
			delete(lm.servers, id)
		}
	}

	// Add or update listeners.
	for id, l := range desired {
		existing, ok := lm.servers[id]
		if ok && sameListener(existing.listener, l) {
			continue
		}

		// Stop old if updating.
		if ok {
			lm.logger.Info("proxy: restarting listener",
				slog.String("id", id),
				slog.String("name", l.Name),
			)
			existing.cancel()
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
		ReadHeaderTimeout: 10 * time.Second,
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
	if l.ServerName != "" || l.AccessLog != nil || l.MaxRequestHeadersKB > 0 {
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
					http.Error(w, "request headers too large", http.StatusRequestHeaderFieldsTooLarge)
					return
				}
			}
			original.ServeHTTP(w, r)
		})
	}

	// Access log middleware wraps the handler.
	if l.AccessLog != nil {
		srv.Handler = middlewares.AccessLogMiddleware(l.AccessLog)(srv.Handler)
	}

	lm.servers[l.ID] = &managedServer{
		listener: l,
		server:   srv,
		cancel:   cancel,
	}

	go func() {
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

// Shutdown gracefully stops all listeners.
func (lm *ListenerManager) Shutdown() {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	for id, ms := range lm.servers {
		ms.cancel()
		delete(lm.servers, id)
	}
}

// sameListener checks if two listener configs are identical (no restart needed).
func sameListener(a, b model.Listener) bool {
	return a.Address == b.Address &&
		a.Port == b.Port &&
		a.ServerName == b.ServerName &&
		a.HTTP2 == b.HTTP2
}
