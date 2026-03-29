// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Vrata is a programmable HTTP reverse proxy control plane with a REST API.
// It manages routes, destinations, listeners, and middlewares — all applied
// in real time without restarts. Configuration is distributed to a fleet of
// Envoy proxies via xDS (ADS).
//
// Usage:
//
//	vrata --config /path/to/config.yaml
//
//	@title			Vrata API
//	@version		1.0
//	@description	Programmable HTTP reverse proxy control plane. Manage routes,
//	@description	destinations, listeners, and middlewares via REST API.
//	@description	Changes are pushed to Envoy instances via xDS instantly.
//	@contact.name	Vrata project
//	@contact.url	https://github.com/achetronic/vrata
//	@license.name	Apache 2.0
//	@license.url	https://www.apache.org/licenses/LICENSE-2.0
//	@host			localhost:8080
//	@BasePath		/
//	@schemes		http https
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/achetronic/vrata/internal/api"
	"github.com/achetronic/vrata/internal/api/handlers"
	"github.com/achetronic/vrata/internal/config"
	"github.com/achetronic/vrata/internal/gateway"
	"github.com/achetronic/vrata/internal/k8s"
	raftnode "github.com/achetronic/vrata/internal/raft"
	"github.com/achetronic/vrata/internal/store"
	boltstore "github.com/achetronic/vrata/internal/store/bolt"
	"github.com/achetronic/vrata/internal/store/raftstore"
	"github.com/achetronic/vrata/internal/xds"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "vrata: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "config.yaml", "path to the YAML configuration file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	logger := buildLogger(cfg)
	return runControlPlane(cfg, logger)
}

// runControlPlane starts the control plane: REST API, persistent store, and
// the xDS gRPC server for Envoy nodes.
func runControlPlane(cfg *config.Config, logger *slog.Logger) error {
	boltPath := cfg.ControlPlane.BoltDBPath()
	logger.Info("vrata starting",
		slog.String("http", cfg.ControlPlane.Address),
		slog.String("xds", cfg.ControlPlane.XDSAddress),
		slog.String("store", boltPath),
	)

	st, err := boltstore.New(boltPath)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			logger.Error("closing store", slog.String("error", err.Error()))
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// In raft mode, wrap the bolt store with the Raft store.
	var activeStore store.Store = st
	var raftApplier handlers.RaftApplier
	if cfg.ControlPlane.Raft != nil {
		raftDir := cfg.ControlPlane.RaftDataDir()
		node, err := raftnode.NewNode(ctx, cfg.ControlPlane.Raft, raftDir, st, logger, cfg.ControlPlane.Address)
		if err != nil {
			return fmt.Errorf("starting raft node: %w", err)
		}
		defer node.Shutdown()

		if err := node.WaitForLeader(120 * time.Second); err != nil {
			return fmt.Errorf("waiting for raft leader: %w", err)
		}

		rs := raftstore.New(st, node)
		activeStore = rs
		raftApplier = node

		logger.Info("raft cluster mode active",
			slog.String("nodeId", cfg.ControlPlane.Raft.NodeID),
			slog.String("bindAddress", cfg.ControlPlane.Raft.BindAddress),
			slog.Bool("isLeader", node.IsLeader()),
		)
	}

	// xDS server — Envoy nodes connect here to receive configuration.
	xdsSrv := xds.New(logger)

	// REST API.
	router := api.NewRouter(activeStore, logger, raftApplier)
	httpSrv := &http.Server{
		Addr:    cfg.ControlPlane.Address,
		Handler: router,
	}
	httpSrv.BaseContext = func(_ net.Listener) context.Context { return ctx }

	// Kubernetes endpoint discovery (non-fatal if no kubeconfig available).
	var epProvider gateway.EndpointProvider
	if k8sClient, err := buildK8sClient(logger); err == nil && k8sClient != nil {
		watcher := k8s.New(k8s.Dependencies{
			Store:  activeStore,
			Client: k8sClient,
			Logger: logger,
		})
		epProvider = watcher

		go func() {
			if err := watcher.Run(ctx); err != nil {
				logger.Error("k8s watcher failed", slog.String("error", err.Error()))
			}
		}()
	}

	gw := gateway.New(gateway.Dependencies{
		Store:            activeStore,
		XDS:              xdsSrv,
		EndpointProvider: epProvider,
		Logger:           logger,
	})

	if epProvider != nil {
		if watcher, ok := epProvider.(*k8s.Watcher); ok {
			watcher.SetOnChange(gw.Rebuild)
		}
	}

	errCh := make(chan error, 3)

	// Start xDS gRPC server.
	go func() {
		if err := xdsSrv.Run(ctx, cfg.ControlPlane.XDSAddress); err != nil {
			errCh <- fmt.Errorf("xds server: %w", err)
		}
	}()

	// Start gateway (store watcher → xDS push).
	go func() {
		if err := gw.Run(ctx); err != nil {
			errCh <- fmt.Errorf("gateway: %w", err)
		}
	}()

	// Start REST API.
	go func() {
		logger.Info("http server listening", slog.String("address", cfg.ControlPlane.Address))
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("http server: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = httpSrv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

// buildLogger creates an slog.Logger based on the log configuration.
func buildLogger(cfg *config.Config) *slog.Logger {
	var level slog.Level
	switch cfg.Log.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if cfg.Log.Format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}

// buildK8sClient creates a Kubernetes client from in-cluster config or
// kubeconfig. Returns nil, nil if neither is available (non-fatal).
func buildK8sClient(logger *slog.Logger) (kubernetes.Interface, error) {
	cfg, err := rest.InClusterConfig()
	if err == nil {
		client, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("creating in-cluster k8s client: %w", err)
		}
		logger.Info("k8s client created from in-cluster config")
		return client, nil
	}

	kubeconfig := ""
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = filepath.Join(home, ".kube", "config")
	}
	if kubeconfig == "" {
		logger.Info("k8s client not available (no in-cluster config, no kubeconfig)")
		return nil, nil
	}

	cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		logger.Info("k8s client not available", slog.String("error", err.Error()))
		return nil, nil
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating k8s client from kubeconfig: %w", err)
	}
	logger.Info("k8s client created from kubeconfig", slog.String("path", kubeconfig))
	return client, nil
}
