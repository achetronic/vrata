// Rutoso is a programmable HTTP reverse proxy with a REST API for configuration.
// It manages routes, destinations, listeners, and middlewares — all applied in
// real time without restarts.
//
// Usage:
//
//	rutoso --config /path/to/config.yaml [--store-path /path/to/rutoso.db]
//
//	@title			Rutoso API
//	@version		1.0
//	@description	Programmable HTTP reverse proxy. Manage routes, destinations,
//	@description	listeners, and middlewares via REST API. Changes apply instantly.
//	@contact.name	Rutoso project
//	@contact.url	https://github.com/achetronic/rutoso
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
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/achetronic/rutoso/internal/api"
	"github.com/achetronic/rutoso/internal/config"
	"github.com/achetronic/rutoso/internal/gateway"
	k8swatcher "github.com/achetronic/rutoso/internal/k8s"
	"github.com/achetronic/rutoso/internal/proxy"
	boltstore "github.com/achetronic/rutoso/internal/store/bolt"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "rutoso: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "config.yaml", "path to the YAML configuration file")
	storePath := flag.String("store-path", "rutoso.db", "path to the bbolt database file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	logger := buildLogger(cfg)
	logger.Info("rutoso starting",
		slog.String("http", cfg.Server.Address),
		slog.String("store", *storePath),
	)

	// Persistent bbolt store.
	st, err := boltstore.New(*storePath)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			logger.Error("closing store", slog.String("error", err.Error()))
		}
	}()

	// Proxy router and listener manager.
	proxyRouter := proxy.NewRouter()
	listenerMgr := proxy.NewListenerManager(proxyRouter, logger)
	healthChecker := proxy.NewHealthChecker(logger)

	// Gateway: bridges store events → proxy config updates.
	gw := gateway.New(gateway.Dependencies{
		Store:           st,
		Router:          proxyRouter,
		ListenerManager: listenerMgr,
		HealthChecker:   healthChecker,
		Logger:          logger,
	})

	// HTTP REST API (management plane).
	router := api.NewRouter(st, logger)
	httpSrv := &http.Server{
		Addr:    cfg.Server.Address,
		Handler: router,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Run components concurrently.
	errCh := make(chan error, 2)

	healthChecker.Start(ctx)

	// Kubernetes EndpointSlice watcher (optional — non-fatal if no kubeconfig).
	k8sClient, err := buildK8sClient()
	if err != nil {
		logger.Warn("k8s watcher disabled: could not build k8s client",
			slog.String("error", err.Error()),
		)
	}
	var k8sWatch *k8swatcher.Watcher
	if k8sClient != nil {
		k8sWatch = k8swatcher.New(k8swatcher.Dependencies{
			Store:  st,
			Client: k8sClient,
			Logger: logger,
		})
		k8sWatch.SetOnChange(gw.Rebuild)
	}

	go func() {
		if err := gw.Run(ctx); err != nil {
			errCh <- fmt.Errorf("gateway: %w", err)
		}
	}()

	go func() {
		logger.Info("http server listening", slog.String("address", cfg.Server.Address))
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("http server: %w", err)
		}
	}()

	if k8sWatch != nil {
		go func() {
			if err := k8sWatch.Run(ctx); err != nil {
				errCh <- fmt.Errorf("k8s watcher: %w", err)
			}
		}()
	}

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
		healthChecker.Stop()
		listenerMgr.Shutdown()
		_ = httpSrv.Shutdown(context.Background())
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

// buildK8sClient creates a Kubernetes client. Tries in-cluster config first,
// then falls back to ~/.kube/config.
func buildK8sClient() (kubernetes.Interface, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			loadingRules, &clientcmd.ConfigOverrides{},
		).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("building k8s client config: %w", err)
		}
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating k8s client: %w", err)
	}
	return client, nil
}
