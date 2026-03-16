// Rutoso is a REST API control plane for Envoy proxies.
// It manages route groups and routes, pushing configuration to Envoy via xDS
// (go-control-plane) without requiring Envoy restarts.
//
// Usage:
//
//	rutoso --config /path/to/config.yaml [--store-path /path/to/rutoso.db]
//
//	@title			Rutoso API
//	@version		1.0
//	@description	REST API control plane for Envoy proxies. Manage route groups and routes;
//	@description	changes are pushed to all connected Envoy instances via xDS in real time.
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
	boltstore "github.com/achetronic/rutoso/internal/store/bolt"
	"github.com/achetronic/rutoso/internal/xds"
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
		slog.String("xds", cfg.XDS.Address),
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

	// xDS control-plane server.
	xdsSrv := xds.New(logger)

	// Gateway: bridges store events → xDS snapshot updates.
	gw := gateway.New(gateway.Dependencies{
		Store:       st,
		Cache:       xdsSrv.Cache(),
		XDSServer:   xdsSrv,
		Logger:      logger,
		NextVersion: xdsSrv.NextVersion,
	})

	// Kubernetes client (in-cluster, fallback to ~/.kube/config).
	k8sClient, err := buildK8sClient()
	if err != nil {
		logger.Warn("k8s watcher disabled: could not build k8s client",
			slog.String("error", err.Error()),
		)
	}

	// HTTP REST API.
	router := api.NewRouter(st, xdsSrv, logger)
	httpSrv := &http.Server{
		Addr:    cfg.Server.Address,
		Handler: router,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Run all components concurrently.
	errCh := make(chan error, 4)

	go func() {
		if err := gw.Run(ctx); err != nil {
			errCh <- fmt.Errorf("gateway: %w", err)
		}
	}()

	if k8sClient != nil {
		k8sWatch := k8swatcher.New(k8swatcher.Dependencies{
			Store:     st,
			Cache:     xdsSrv.Cache(),
			XDSServer: xdsSrv,
			Client:  k8sClient,
			Logger:  logger,
			Rebuild: gw.Rebuild,
		})
		go func() {
			if err := k8sWatch.Run(ctx); err != nil {
				errCh <- fmt.Errorf("k8s watcher: %w", err)
			}
		}()
	}

	go func() {
		if err := xdsSrv.Serve(ctx, cfg.XDS.Address); err != nil {
			errCh <- fmt.Errorf("xds server: %w", err)
		}
	}()

	go func() {
		logger.Info("http server listening", slog.String("address", cfg.Server.Address))
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("http server: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
		_ = httpSrv.Shutdown(context.Background())
		return nil
	case err := <-errCh:
		return err
	}
}

// buildK8sClient creates a Kubernetes client. It tries in-cluster config first,
// then falls back to the default kubeconfig (~/.kube/config).
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
